package cli_test

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

const authOpenAPI = `{"paths":{"/api/v2/models/hf_login":{"get":{},"post":{"requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Body_do_hf_login"}}}}},"delete":{}}},"components":{"schemas":{"Body_do_hf_login":{"properties":{"token":{"type":"string"}},"required":["token"]},"HFTokenStatus":{"enum":["valid","invalid","unknown"]}}}}`

func runAuthCommand(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(stdin), &stdout, &stderr)
	code := app.Run(t.Context(), args)
	return code, stdout.String(), stderr.String()
}

func authEnvelope(t *testing.T, output string) struct {
	SchemaVersion int    `json:"schema_version"`
	OK            bool   `json:"ok"`
	Operation     string `json:"operation"`
	Data          struct {
		Status string `json:"status"`
	} `json:"data"`
	Error    *result.Error    `json:"error"`
	Warnings []result.Warning `json:"warnings"`
} {
	t.Helper()
	var envelope struct {
		SchemaVersion int    `json:"schema_version"`
		OK            bool   `json:"ok"`
		Operation     string `json:"operation"`
		Data          struct {
			Status string `json:"status"`
		} `json:"data"`
		Error    *result.Error    `json:"error"`
		Warnings []result.Warning `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.SchemaVersion != 1 || envelope.Warnings == nil {
		t.Fatalf("invalid envelope: %s", output)
	}
	return envelope
}

func TestHuggingFaceAuthStatusReportsOnlyNormalizedState(t *testing.T) {
	isolateUserConfigDir(t)
	for _, status := range []string{"valid", "invalid", "unknown"} {
		t.Run(status, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v2/models/hf_login" || r.Method != http.MethodGet {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				_, _ = w.Write([]byte(`"` + status + `"`))
			}))
			t.Cleanup(server.Close)
			code, stdout, stderr := runAuthCommand(t, "", "auth", "huggingface", "status", "--url", server.URL, "--json")
			envelope := authEnvelope(t, stdout)
			if code != 0 || stderr != "" || !envelope.OK || envelope.Operation != "auth.huggingface.status" || envelope.Data.Status != status {
				t.Fatalf("code=%d output=%s stderr=%q", code, stdout, stderr)
			}
		})
	}
}

func TestHuggingFaceAuthLoginAndLogoutUseExactMutations(t *testing.T) {
	isolateUserConfigDir(t)
	const sentinel = "hf-sentinel-517"
	var posts, deletes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(authOpenAPI))
		case "/api/v2/models/hf_login":
			switch r.Method {
			case http.MethodPost:
				posts.Add(1)
				body, _ := io.ReadAll(r.Body)
				if string(body) != `{"token":"`+sentinel+`"}` {
					t.Errorf("login body = %q", body)
				}
				_, _ = w.Write([]byte(`"valid"`))
			case http.MethodDelete:
				deletes.Add(1)
				_, _ = w.Write([]byte(`"invalid"`))
			default:
				t.Errorf("unexpected method %s", r.Method)
			}
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	for _, tc := range []struct {
		args                     []string
		stdin, operation, status string
	}{
		{[]string{"login", "--token-stdin"}, sentinel + "\n", "auth.huggingface.login", "valid"},
		{[]string{"logout"}, "", "auth.huggingface.logout", "invalid"},
	} {
		args := append([]string{"auth", "huggingface"}, tc.args...)
		args = append(args, "--url", server.URL, "--json")
		code, stdout, stderr := runAuthCommand(t, tc.stdin, args...)
		value := authEnvelope(t, stdout)
		if code != 0 || stderr != "" || !value.OK || value.Operation != tc.operation || value.Data.Status != tc.status || strings.Contains(stdout+stderr, sentinel) {
			t.Fatalf("code=%d output=%q stderr=%q", code, stdout, stderr)
		}
	}
	if posts.Load() != 1 || deletes.Load() != 1 {
		t.Fatalf("mutations: post=%d delete=%d", posts.Load(), deletes.Load())
	}
}

func TestHuggingFaceAuthFailuresAreStructuredAndDoNotEchoToken(t *testing.T) {
	isolateUserConfigDir(t)
	const sentinel = "hf-secret-sentinel-683"
	for _, tc := range []struct {
		name, command, response, code string
		status, exit                  int
	}{
		{"rejected login", "login", `"invalid"`, result.CodeAuthenticationFailed, 200, result.ExitConnection},
		{"unknown login", "login", `"unknown"`, result.CodeOutcomeUnknown, 200, result.ExitInvokeAIFailure},
		{"failed logout", "logout", sentinel, result.CodeInvokeAIOperationFailed, 500, result.ExitInvokeAIFailure},
		{"unchanged logout", "logout", `"valid"`, result.CodeInvokeAIOperationFailed, 200, result.ExitInvokeAIFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mutations atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/app/version":
					_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
				case "/openapi.json":
					_, _ = w.Write([]byte(authOpenAPI))
				case "/api/v2/models/hf_login":
					mutations.Add(1)
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.response))
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
				}
			}))
			t.Cleanup(server.Close)
			args := []string{"auth", "huggingface", tc.command}
			if tc.command == "login" {
				args = append(args, "--token-stdin")
			}
			args = append(args, "--url", server.URL, "--json")
			code, stdout, stderr := runAuthCommand(t, sentinel+"\n", args...)
			value := authEnvelope(t, stdout)
			if code != tc.exit || value.OK || value.Error == nil || value.Error.Code != tc.code || stderr != "" || mutations.Load() != 1 || strings.Contains(stdout+stderr, sentinel) {
				t.Fatalf("code=%d output=%s stderr=%q mutations=%d", code, stdout, stderr, mutations.Load())
			}
		})
	}
}

func TestHuggingFaceAuthRejectsEmptyTokenBeforeMutation(t *testing.T) {
	isolateUserConfigDir(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		t.Errorf("unexpected request: %s", r.URL.Path)
	}))
	t.Cleanup(server.Close)
	code, stdout, stderr := runAuthCommand(t, " \n", "auth", "huggingface", "login", "--token-stdin", "--url", server.URL, "--json")
	value := authEnvelope(t, stdout)
	if code != result.ExitInvalidRequest || value.Error == nil || value.Error.Code != result.CodeInvalidRequest || stderr != "" || requests.Load() != 0 {
		t.Fatalf("code=%d output=%s stderr=%q", code, stdout, stderr)
	}
}

func TestHuggingFaceAuthArgumentErrorsDoNotEchoAccidentalToken(t *testing.T) {
	isolateUserConfigDir(t)
	const sentinel = "hf-accidental-token-225"
	for _, args := range [][]string{
		{"auth", "huggingface", "login", sentinel, "--json"},
		{"auth", "huggingface", "login", "--token-stdin=" + sentinel, "--json"},
	} {
		code, stdout, stderr := runAuthCommand(t, "", args...)
		value := authEnvelope(t, stdout)
		if code != result.ExitInvalidRequest || value.Error == nil || value.Error.Code != result.CodeInvalidRequest || strings.Contains(stdout+stderr, sentinel) {
			t.Fatalf("code=%d output=%s stderr=%q", code, stdout, stderr)
		}
	}
}

func TestHuggingFaceAuthUnknownStatusIsInvalidResponse(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`"expired"`)) }))
	t.Cleanup(server.Close)
	code, stdout, _ := runAuthCommand(t, "", "auth", "huggingface", "status", "--url", server.URL, "--json")
	value := authEnvelope(t, stdout)
	if code != result.ExitInvokeAIFailure || value.Error == nil || value.Error.Code != result.CodeInvalidInvokeAIResponse {
		t.Fatalf("code=%d output=%s", code, stdout)
	}
}

func TestHuggingFaceAuthMutationsAreNotRetriedAfterConnectionLoss(t *testing.T) {
	isolateUserConfigDir(t)
	const sentinel = "hf-sentinel-904"
	for _, command := range []string{"login", "logout"} {
		t.Run(command, func(t *testing.T) {
			var mutations atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/app/version":
					_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
				case "/openapi.json":
					_, _ = w.Write([]byte(authOpenAPI))
				case "/api/v2/models/hf_login":
					mutations.Add(1)
					connection, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = connection.Close()
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
			}))
			t.Cleanup(server.Close)
			args := []string{"auth", "huggingface", command}
			if command == "login" {
				args = append(args, "--token-stdin")
			}
			args = append(args, "--url", server.URL, "--json")
			code, stdout, stderr := runAuthCommand(t, sentinel+"\n", args...)
			value := authEnvelope(t, stdout)
			if code != result.ExitInvokeAIFailure || value.Error == nil || value.Error.Code != result.CodeOutcomeUnknown || mutations.Load() != 1 || strings.Contains(stdout+stderr, sentinel) {
				t.Fatalf("code=%d output=%s stderr=%q mutations=%d", code, stdout, stderr, mutations.Load())
			}
		})
	}
}

func TestHuggingFaceAuthMutationsDoNotFollowRedirects(t *testing.T) {
	isolateUserConfigDir(t)
	const sentinel = "hf-redirect-sentinel-210"
	for _, command := range []string{"login", "logout"} {
		t.Run(command, func(t *testing.T) {
			var mutations, forwarded atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/app/version":
					_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
				case "/openapi.json":
					_, _ = w.Write([]byte(authOpenAPI))
				case "/api/v2/models/hf_login":
					mutations.Add(1)
					w.Header().Set("Location", "/forwarded")
					w.WriteHeader(http.StatusTemporaryRedirect)
					_, _ = w.Write([]byte(sentinel))
				case "/forwarded":
					forwarded.Add(1)
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
			}))
			t.Cleanup(server.Close)
			args := []string{"auth", "huggingface", command}
			if command == "login" {
				args = append(args, "--token-stdin")
			}
			args = append(args, "--url", server.URL, "--json")
			code, stdout, stderr := runAuthCommand(t, sentinel+"\n", args...)
			value := authEnvelope(t, stdout)
			if code != result.ExitInvokeAIFailure || value.Error == nil || value.Error.Code != result.CodeInvokeAIOperationFailed || mutations.Load() != 1 || forwarded.Load() != 0 || strings.Contains(stdout+stderr, sentinel) {
				t.Fatalf("code=%d output=%s stderr=%q mutations=%d forwarded=%d", code, stdout, stderr, mutations.Load(), forwarded.Load())
			}
		})
	}
}
