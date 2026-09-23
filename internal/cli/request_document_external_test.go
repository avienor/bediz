package cli_test

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

// Every Request Document matches member names exactly and rejects repeated
// members at every depth, before any request reaches InvokeAI.
func TestRequestDocumentsRejectCaseFoldedAndDuplicateMembers(t *testing.T) {
	commands := []struct {
		name  string
		args  []string
		valid string
		cases map[string]string
	}{
		{
			name:  "generate",
			args:  []string{"generate"},
			valid: `{"schema_version":1,"model":"m","positive_prompt":"p","components":{"vae":"v"}}`,
			cases: map[string]string{
				"case-folded top-level member": `{"schema_version":1,"MODEL":"m","positive_prompt":"p"}`,
				"case-folded nested member":    `{"schema_version":1,"model":"m","positive_prompt":"p","components":{"VAE":"v"}}`,
				"duplicate top-level member":   `{"schema_version":1,"model":"a","model":"m","positive_prompt":"p"}`,
				"duplicate nested member":      `{"schema_version":1,"model":"m","positive_prompt":"p","components":{"vae":"a","vae":"v"}}`,
			},
		},
		{
			name:  "recall",
			args:  []string{"recall"},
			valid: `{"schema_version":1,"seed":1}`,
			cases: map[string]string{
				"case-folded top-level member": `{"schema_version":1,"Seed":1}`,
				"duplicate top-level member":   `{"schema_version":1,"seed":2,"seed":1}`,
			},
		},
		{
			name:  "models install",
			args:  []string{"models", "install"},
			valid: `{"schema_version":1,"source":{"type":"url","reference":"https://example.com/model.safetensors"}}`,
			cases: map[string]string{
				"case-folded top-level member": `{"schema_version":1,"Source":{"type":"url","reference":"https://example.com/model.safetensors"}}`,
				"case-folded nested member":    `{"schema_version":1,"source":{"TYPE":"url","reference":"https://example.com/model.safetensors"}}`,
				"duplicate top-level member":   `{"schema_version":1,"schema_version":1,"source":{"type":"url","reference":"https://example.com/model.safetensors"}}`,
				"duplicate nested member":      `{"schema_version":1,"source":{"type":"path","type":"url","reference":"https://example.com/model.safetensors"}}`,
			},
		},
		{
			name:  "upscale",
			args:  []string{"upscale"},
			valid: `{"schema_version":1,"source":{"type":"image","reference":"source.png"},"model":"m"}`,
			cases: map[string]string{
				"case-folded top-level member": `{"schema_version":1,"source":{"type":"image","reference":"source.png"},"Model":"m"}`,
				"case-folded nested member":    `{"schema_version":1,"source":{"type":"image","Reference":"source.png"},"model":"m"}`,
				"duplicate top-level member":   `{"schema_version":1,"source":{"type":"image","reference":"source.png"},"model":"a","model":"m"}`,
				"duplicate nested member":      `{"schema_version":1,"source":{"type":"image","reference":"a.png","reference":"source.png"},"model":"m"}`,
			},
		},
	}
	for _, command := range commands {
		t.Run(command.name+"/valid document reaches InvokeAI", func(t *testing.T) {
			if requests, _, _ := runRequestDocument(t, command.args, command.valid); requests == 0 {
				t.Fatal("valid document sent no request; the rejection cases would prove nothing")
			}
		})
		for name, document := range command.cases {
			t.Run(command.name+"/"+name, func(t *testing.T) {
				requests, exitCode, stdout := runRequestDocument(t, command.args, document)
				if exitCode != result.ExitInvalidRequest || requests != 0 {
					t.Fatalf("exit code = %d, requests = %d, stdout = %q", exitCode, requests, stdout)
				}
				var envelope result.Envelope
				if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
					t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout)
				}
				if envelope.Error == nil || envelope.Error.Code != result.CodeInvalidRequest {
					t.Fatalf("unexpected envelope: %#v", envelope)
				}
			})
		}
	}
}

func runRequestDocument(t *testing.T, args []string, document string) (requests int64, exitCode int, stdout string) {
	t.Helper()
	isolateUserConfigDir(t)
	var count atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	var out, stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(document), &out, &stderr)
	exitCode = app.Run(t.Context(), append(args, "--request", "-", "--url", server.URL, "--json"))
	return count.Load(), exitCode, out.String()
}

// An optional field is omitted, never null: an explicit null member value is
// rejected at every depth instead of silently acquiring a default.
func TestRequestDocumentsRejectExplicitNullMembers(t *testing.T) {
	commands := []struct {
		name    string
		args    []string
		valid   string
		nulls   map[string]string
		several string
	}{
		{
			name:  "generate",
			args:  []string{"generate"},
			valid: `{"schema_version":1,"model":"m","positive_prompt":"p","components":{"vae":"v"}}`,
			nulls: map[string]string{
				"top-level seed":  `{"schema_version":1,"model":"m","positive_prompt":"p","seed":null,"components":{"vae":"v"}}`,
				"top-level model": `{"schema_version":1,"model":null,"positive_prompt":"p","components":{"vae":"v"}}`,
				"nested vae":      `{"schema_version":1,"model":"m","positive_prompt":"p","components":{"vae":null}}`,
			},
			several: `{"schema_version":1,"model":"m","positive_prompt":"p","steps":null,"seed":null,"components":{"vae":null}}`,
		},
		{
			name:  "recall",
			args:  []string{"recall"},
			valid: `{"schema_version":1,"positive_prompt":"p"}`,
			nulls: map[string]string{
				"top-level seed":  `{"schema_version":1,"positive_prompt":"p","seed":null}`,
				"top-level model": `{"schema_version":1,"positive_prompt":"p","model":null}`,
			},
			several: `{"schema_version":1,"positive_prompt":"p","seed":null,"model":null,"width":null}`,
		},
		{
			name:  "models install",
			args:  []string{"models", "install"},
			valid: `{"schema_version":1,"source":{"type":"url","reference":"https://example.com/model.safetensors"}}`,
			nulls: map[string]string{
				"top-level move":  `{"schema_version":1,"source":{"type":"url","reference":"https://example.com/model.safetensors"},"move":null}`,
				"nested file_id":  `{"schema_version":1,"source":{"type":"url","reference":"https://example.com/model.safetensors","file_id":null}}`,
				"nested artifact": `{"schema_version":1,"source":{"type":"url","reference":"https://example.com/model.safetensors","artifact":null}}`,
			},
			several: `{"schema_version":1,"move":null,"source":{"type":"url","reference":"https://example.com/model.safetensors","file_id":null,"artifact":null}}`,
		},
	}
	for _, command := range commands {
		t.Run(command.name+"/document without null reaches InvokeAI", func(t *testing.T) {
			if requests, _, _ := runRequestDocument(t, command.args, command.valid); requests == 0 {
				t.Fatal("document without null sent no request; the rejection cases would prove nothing")
			}
		})
		for name, document := range command.nulls {
			t.Run(command.name+"/"+name, func(t *testing.T) {
				requests, exitCode, stdout := runRequestDocument(t, command.args, document)
				if exitCode != result.ExitInvalidRequest || requests != 0 || requestDocumentError(t, stdout).Code != result.CodeInvalidRequest {
					t.Fatalf("exit code = %d, requests = %d, stdout = %q", exitCode, requests, stdout)
				}
			})
		}
		t.Run(command.name+"/several nulls report the same field on every run", func(t *testing.T) {
			messages := map[string]bool{}
			for range 20 {
				_, exitCode, stdout := runRequestDocument(t, command.args, command.several)
				failure := requestDocumentError(t, stdout)
				if exitCode != result.ExitInvalidRequest || failure.Code != result.CodeInvalidRequest {
					t.Fatalf("exit code = %d, stdout = %q", exitCode, stdout)
				}
				messages[failure.Message] = true
			}
			if len(messages) != 1 {
				t.Fatalf("null-field messages differ between runs: %v", slices.Collect(maps.Keys(messages)))
			}
		})
	}
}

func requestDocumentError(t *testing.T, stdout string) result.Error {
	t.Helper()
	var envelope result.Envelope
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil || envelope.Error == nil {
		t.Fatalf("stdout is not one error envelope: %v; stdout = %q", err, stdout)
	}
	return *envelope.Error
}

// A null list element would otherwise become an empty value and silently change
// the operation, so it is rejected like a null member.
func TestRequestDocumentsRejectNullListElements(t *testing.T) {
	args := []string{"models", "list"}
	if requests, _, _ := runRequestDocument(t, args, `{"schema_version":1,"base_models":["sdxl"]}`); requests == 0 {
		t.Fatal("document without null sent no request; the rejection cases would prove nothing")
	}
	for name, document := range map[string]string{
		"only element":  `{"schema_version":1,"base_models":[null]}`,
		"later element": `{"schema_version":1,"base_models":["sdxl",null]}`,
	} {
		t.Run(name, func(t *testing.T) {
			requests, exitCode, stdout := runRequestDocument(t, args, document)
			if exitCode != result.ExitInvalidRequest || requests != 0 || requestDocumentError(t, stdout).Code != result.CodeInvalidRequest {
				t.Fatalf("exit code = %d, requests = %d, stdout = %q", exitCode, requests, stdout)
			}
		})
	}
}
