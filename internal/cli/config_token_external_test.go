package cli_test

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/config"
	"github.com/avienor/bediz/internal/result"
)

func TestConfigSetTokenStdinPersistsConnectionToken(t *testing.T) {
	isolateUserConfigDir(t)
	for _, name := range []string{"BEDIZ_URL", "BEDIZ_TOKEN"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	const token = "stdin-connection-token-sentinel"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/models/" || r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "stored connection token was not used", http.StatusUnauthorized)
			return
		}
		_, _ = fmt.Fprint(w, `{"models":[]}`)
	}))
	t.Cleanup(server.Close)
	var stdout, stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(" \t"+token+"\r\n"), &stdout, &stderr)
	code := app.Run(t.Context(), []string{"config", "set", "--url", server.URL, "--token-stdin", "--json"})
	if code != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON envelope: %v", err)
	}
	if envelope.SchemaVersion != 1 || !envelope.OK || envelope.Operation != result.OperationConfigSet {
		t.Fatalf("unexpected config set envelope: %#v", envelope)
	}
	if strings.Contains(stdout.String()+stderr.String(), token) {
		t.Fatal("standard-input token leaked into output")
	}

	stdout.Reset()
	stderr.Reset()
	app = cli.NewWithIO(strings.NewReader(""), &stdout, &stderr)
	code = app.Run(t.Context(), []string{"models", "list", "--json"})
	if code != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("stored connection failed: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("models list stdout is not one JSON envelope: %v", err)
	}
	if !envelope.OK || envelope.Operation != result.OperationModelsList || strings.Contains(stdout.String()+stderr.String(), token) {
		t.Fatalf("unexpected models list output: %q; stderr=%q", stdout.String(), stderr.String())
	}
}

type configTokenErrorReader struct{}

func (configTokenErrorReader) Read(data []byte) (int, error) {
	return copy(data, "partial-token-sentinel"), fmt.Errorf("private-stdin-error-sentinel")
}

func runConfigTokenCommand(t *testing.T, stdin io.Reader, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	app := cli.NewWithIO(stdin, &stdout, &stderr)
	code := app.Run(t.Context(), args)
	return code, stdout.String(), stderr.String()
}

func TestConfigSetTokenStdinRejectsConflictingFlagsBeforeReading(t *testing.T) {
	for _, args := range [][]string{
		{"--token", "argument-token-sentinel", "--token-stdin"},
		{"--token", "", "--token-stdin"},
		{"--unset-token", "--token-stdin"},
		{"--url", "http://127.0.0.1:9091", "--unset-url", "--token-stdin"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			isolateUserConfigDir(t)
			code, stdout, stderr := runConfigTokenCommand(t, strings.NewReader(""), "config", "set", "--url", "http://127.0.0.1:9090", "--token", "existing-token-sentinel", "--json")
			if code != result.ExitSuccess {
				t.Fatalf("seed config: code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			_, before, _ := runConfigTokenCommand(t, strings.NewReader(""), "config", "get", "--json")

			command := append([]string{"config", "set", "--json"}, args...)
			code, stdout, stderr = runConfigTokenCommand(t, configTokenErrorReader{}, command...)
			var envelope result.Envelope
			if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
				t.Fatalf("stdout is not one JSON envelope: %v", err)
			}
			if code != result.ExitInvalidRequest || stderr != "" || envelope.Operation != result.OperationConfigSet || envelope.Error == nil || envelope.Error.Code != result.CodeInvalidRequest || !strings.Contains(envelope.Error.Message, "cannot be combined") {
				t.Fatalf("expected flag conflict before reading stdin: code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			if strings.Contains(stdout+stderr, "sentinel") {
				t.Fatal("secret input or reader error leaked into output")
			}
			code, after, stderr := runConfigTokenCommand(t, strings.NewReader(""), "config", "get", "--json")
			if code != result.ExitSuccess || stderr != "" || after != before {
				t.Fatalf("invalid input changed configuration: code=%d before=%q after=%q stderr=%q", code, before, after, stderr)
			}
			assertStoredConfigTokenPreserved(t)
		})
	}
}

func assertStoredConfigTokenPreserved(t *testing.T) {
	t.Helper()
	path, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if stored.URL != "http://127.0.0.1:9090" || stored.Token != "existing-token-sentinel" {
		t.Fatal("invalid input changed the stored URL or token")
	}
}

func TestConfigSetTokenStdinRejectsInvalidInputWithoutChangingConfig(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		for _, test := range []struct {
			name    string
			stdin   io.Reader
			message string
		}{
			{"empty", strings.NewReader(""), "token cannot be empty"},
			{"whitespace", strings.NewReader(" \t\r\n"), "token cannot be empty"},
			{"read failure", configTokenErrorReader{}, "could not read InvokeAI token from standard input"},
		} {
			t.Run(fmt.Sprintf("%s/json=%t", test.name, jsonOutput), func(t *testing.T) {
				isolateUserConfigDir(t)
				code, stdout, stderr := runConfigTokenCommand(t, strings.NewReader(""), "config", "set", "--url", "http://127.0.0.1:9090", "--token", "existing-token-sentinel", "--json")
				if code != result.ExitSuccess {
					t.Fatalf("seed config: code=%d stdout=%q stderr=%q", code, stdout, stderr)
				}
				args := []string{"config", "set", "--token-stdin", "--url", "http://127.0.0.1:9091"}
				if jsonOutput {
					args = append(args, "--json")
				}
				code, stdout, stderr = runConfigTokenCommand(t, test.stdin, args...)
				if code != result.ExitInvalidRequest || !strings.Contains(stdout+stderr, test.message) || strings.Contains(stdout+stderr, "sentinel") {
					t.Fatalf("unexpected input error: code=%d stdout=%q stderr=%q", code, stdout, stderr)
				}
				if jsonOutput {
					var envelope result.Envelope
					if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
						t.Fatal(err)
					}
					if stderr != "" || envelope.OK || envelope.Operation != result.OperationConfigSet || envelope.Error == nil || envelope.Error.Code != result.CodeInvalidRequest {
						t.Fatalf("unexpected input error envelope: stdout=%q stderr=%q", stdout, stderr)
					}
				} else if stdout != "" || !strings.Contains(stderr, result.CodeInvalidRequest) {
					t.Fatalf("unexpected human input error: stdout=%q stderr=%q", stdout, stderr)
				}
				assertStoredConfigTokenPreserved(t)
			})
		}
	}
}

func TestConfigSetTokenStdinHumanOutputAndConfigGetRedactToken(t *testing.T) {
	isolateUserConfigDir(t)
	const token = "human-config-token-sentinel"
	code, stdout, stderr := runConfigTokenCommand(t, strings.NewReader(token+"\n"), "config", "set", "--token-stdin")
	if code != result.ExitSuccess || stderr != "" || !strings.Contains(stdout, "Stored token: configured") || strings.Contains(stdout, token) {
		t.Fatalf("unexpected human output: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, stderr = runConfigTokenCommand(t, strings.NewReader(""), "config", "get", "--json")
	var envelope struct {
		OK        bool   `json:"ok"`
		Operation string `json:"operation"`
		Data      struct {
			Stored struct {
				TokenConfigured bool `json:"token_configured"`
			} `json:"stored"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	if code != result.ExitSuccess || stderr != "" || !envelope.OK || envelope.Operation != result.OperationConfigGet || !envelope.Data.Stored.TokenConfigured || strings.Contains(stdout, token) {
		t.Fatalf("unexpected config get output: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestConfigSetTokenArgumentErrorsDoNotEchoAccidentalToken(t *testing.T) {
	const token = "accidental-config-token-sentinel"
	for _, jsonOutput := range []bool{false, true} {
		for _, args := range [][]string{
			{"config", "set", "--token-stdin=" + token},
			{"config", "set", "--token-stdin", token},
			{"config", "set", "--token", token, token},
		} {
			if jsonOutput {
				args = append(args, "--json")
			}
			code, stdout, stderr := runConfigTokenCommand(t, strings.NewReader(""), args...)
			if code != result.ExitInvalidRequest || strings.Contains(stdout+stderr, token) {
				t.Fatalf("argument error exposed token: code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			if jsonOutput {
				var envelope result.Envelope
				if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
					t.Fatal(err)
				}
				if stderr != "" || envelope.Operation != result.OperationConfigSet || envelope.Error == nil || envelope.Error.Code != result.CodeInvalidRequest {
					t.Fatalf("unexpected argument error: stdout=%q stderr=%q", stdout, stderr)
				}
			} else if stdout != "" || !strings.Contains(stderr, result.CodeInvalidRequest) {
				t.Fatalf("unexpected human argument error: stdout=%q stderr=%q", stdout, stderr)
			}
		}
	}
}

func TestConnectionTokenHelpRecommendsSafeInput(t *testing.T) {
	for _, command := range [][]string{{"config", "set"}, {"doctor"}, {"models", "list"}} {
		t.Run(strings.Join(command, " "), func(t *testing.T) {
			args := append(command, "--help")
			code, stdout, stderr := runConfigTokenCommand(t, strings.NewReader(""), args...)
			if code != result.ExitSuccess || stderr != "" {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			for _, advice := range []string{"--token-stdin", "process arguments"} {
				if !strings.Contains(stdout, advice) {
					t.Errorf("help is missing %q: %s", advice, stdout)
				}
			}
			if command[0] != "config" && (!strings.Contains(stdout, "BEDIZ_TOKEN") || !strings.Contains(stdout, "config set --token-stdin")) {
				t.Errorf("remote help is missing environment and stored-token guidance: %s", stdout)
			}
		})
	}
}
