package cli_test

import (
	"bytes"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

func TestInvalidRequestUnknownFieldNamesRequestKey(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("invalid document reached InvokeAI: %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	status := cli.NewWithIO(strings.NewReader(`{"schema_version":1,"model":"main-key","positive_prompt":"test","widht":512}`), &stdout, &stderr).
		Run(t.Context(), []string{"generate", "--request", "-", "--url", server.URL, "--json"})
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON envelope: %v; stdout=%q", err, stdout.String())
	}
	if status != 2 || stderr.Len() != 0 || envelope.OK || envelope.SchemaVersion != 1 || envelope.Operation != "generate" || envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("status=%d stderr=%q envelope=%#v", status, stderr.String(), envelope)
	}
	if envelope.Error.Details["field"] != "widht" {
		t.Fatalf("invalid_request details=%#v; want field widht", envelope.Error.Details)
	}
}

func TestInvalidRequestMissingDimensionNamesRequiredKey(t *testing.T) {
	for _, tc := range []struct {
		name, document, field string
		args                  []string
	}{
		{"width document", `{"schema_version":1,"model":"main-key","positive_prompt":"test","width":512}`, "height", []string{"--request", "-"}},
		{"height document", `{"schema_version":1,"model":"main-key","positive_prompt":"test","height":512}`, "width", []string{"--request", "-"}},
		{"width flag", "", "height", []string{"--model", "main-key", "--prompt", "test", "--width", "512"}},
		{"height flag", "", "width", []string{"--model", "main-key", "--prompt", "test", "--height", "512"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("invalid dimensions reached InvokeAI: %s %s", r.Method, r.URL.Path)
				http.NotFound(w, r)
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			args := append([]string{"generate", "--url", server.URL, "--json"}, tc.args...)
			status := cli.NewWithIO(strings.NewReader(tc.document), &stdout, &stderr).Run(t.Context(), args)
			var envelope result.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("stdout is not one JSON envelope: %v; stdout=%q", err, stdout.String())
			}
			if status != 2 || stderr.Len() != 0 || envelope.OK || envelope.Operation != "generate" || envelope.Error == nil || envelope.Error.Code != "invalid_request" {
				t.Fatalf("status=%d stderr=%q envelope=%#v", status, stderr.String(), envelope)
			}
			if envelope.Error.Details["field"] != tc.field {
				t.Fatalf("invalid_request details=%#v; want field %s", envelope.Error.Details, tc.field)
			}
		})
	}
}

func TestInvalidRequestSchedulerNamesRequestKey(t *testing.T) {
	for _, tc := range []struct {
		name, model string
		inventory   []map[string]any
		openAPI     map[string]any
	}{
		{"Anima", "main-key", animaModelInventory(), animaOpenAPIFixture("", "")},
		{"SDXL", "sdxl-main", sdxlInventory(), sdxlOpenAPIFixture(t)},
		{"FLUX.1", "flux-dev", fluxCLIInventory(), sdxlOpenAPIFixture(t)},
	} {
		for _, input := range []string{"document", "flags"} {
			t.Run(tc.name+"/"+input, func(t *testing.T) {
				isolateUserConfigDir(t)
				server := newAnimaGenerationServer(tc.openAPI, tc.inventory, func(w http.ResponseWriter, r *http.Request) {
					t.Errorf("invalid scheduler reached enqueue: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				})
				defer server.Close()
				var stdout, stderr bytes.Buffer
				document := `{"schema_version":1,"model":"` + tc.model + `","positive_prompt":"test","scheduler":"bad-scheduler"}`
				args := []string{"generate", "--request", "-", "--url", server.URL, "--json"}
				if input == "flags" {
					args = []string{"generate", "--model", tc.model, "--prompt", "test", "--scheduler", "bad-scheduler", "--url", server.URL, "--json"}
				}
				status := cli.NewWithIO(strings.NewReader(document), &stdout, &stderr).Run(t.Context(), args)
				var envelope result.Envelope
				if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
					t.Fatalf("stdout is not one JSON envelope: %v; stdout=%q", err, stdout.String())
				}
				if status != 2 || stderr.Len() != 0 || envelope.OK || envelope.Operation != "generate" || envelope.Error == nil || envelope.Error.Code != "invalid_request" {
					t.Fatalf("status=%d stderr=%q envelope=%#v", status, stderr.String(), envelope)
				}
				if envelope.Error.Details["field"] != "scheduler" {
					t.Fatalf("invalid_request details=%#v; want field scheduler", envelope.Error.Details)
				}
			})
		}
	}
}

func TestInvalidRequestDocumentFailuresNameFieldPaths(t *testing.T) {
	for _, tc := range []struct {
		name, document, operation, field string
		args                             []string
	}{
		{"top-level type", `{"schema_version":1,"model":"m","positive_prompt":"p","width":"wide"}`, "generate", "width", []string{"generate"}},
		{"nested type", `{"schema_version":1,"model":"m","positive_prompt":"p","components":{"vae":42}}`, "generate", "components.vae", []string{"generate"}},
		{"nested unknown", `{"schema_version":1,"model":"m","positive_prompt":"p","components":{"vea":"v"}}`, "generate", "components.vea", []string{"generate"}},
		{"installation source type", `{"schema_version":1,"source":{"type":"url","reference":42}}`, "models.install", "source.reference", []string{"models", "install"}},
		{"upscale component type", `{"schema_version":1,"source":{"type":"image","reference":"source.png"},"model":"m","components":{"vae":42}}`, "upscale", "components.vae", []string{"upscale"}},
		{"array element type", `{"schema_version":1,"base_models":["sdxl",42]}`, "models.list", "base_models.1", []string{"models", "list"}},
		{"profile type", `{"schema_version":1,"name":"preset","generate":{"steps":"many"}}`, "profiles.create", "generate.steps", []string{"profiles", "create"}},
		{"top-level null", `{"schema_version":1,"model":"m","positive_prompt":"p","seed":null}`, "generate", "seed", []string{"generate"}},
		{"nested null", `{"schema_version":1,"model":"m","positive_prompt":"p","components":{"vae":null}}`, "generate", "components.vae", []string{"generate"}},
		{"array element null", `{"schema_version":1,"base_models":["sdxl",null]}`, "models.list", "base_models.1", []string{"models", "list"}},
		{"top-level duplicate", `{"schema_version":1,"model":"m","positive_prompt":"p","model":"other"}`, "generate", "model", []string{"generate"}},
		{"nested duplicate", `{"schema_version":1,"model":"m","positive_prompt":"p","components":{"vae":"v","vae":"other"}}`, "generate", "components.vae", []string{"generate"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("invalid document reached InvokeAI: %s %s", r.Method, r.URL.Path)
				http.NotFound(w, r)
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			args := append(tc.args, "--request", "-", "--json")
			if tc.operation != "profiles.create" {
				args = append(args, "--url", server.URL)
			}
			status := cli.NewWithIO(strings.NewReader(tc.document), &stdout, &stderr).Run(t.Context(), args)
			var envelope result.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("stdout is not one JSON envelope: %v; stdout=%q", err, stdout.String())
			}
			if status != 2 || stderr.Len() != 0 || envelope.OK || envelope.Operation != tc.operation || envelope.Error == nil || envelope.Error.Code != "invalid_request" {
				t.Fatalf("status=%d stderr=%q envelope=%#v", status, stderr.String(), envelope)
			}
			if envelope.Error.Details["field"] != tc.field {
				t.Fatalf("invalid_request details=%#v; want field %s", envelope.Error.Details, tc.field)
			}
		})
	}
}
