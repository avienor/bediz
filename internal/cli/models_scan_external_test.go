package cli_test

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

func TestModelsScanReturnsSortedAllowlistedServerPaths(t *testing.T) {
	isolateUserConfigDir(t)
	const serverPath = "/server/models with spaces"
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/models/scan_folder" || r.URL.Query().Get("scan_path") != serverPath {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		_, _ = w.Write([]byte(`[{"path":"/server/z.safetensors","is_installed":false,"secret":"do not expose"},{"path":"/server/a.safetensors","is_installed":true,"source":"private"}]`))
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)
	code := app.Run(t.Context(), []string{"models", "scan", "--path", serverPath, "--url", server.URL, "--json"})
	if code != result.ExitSuccess || stderr.Len() != 0 || requests != 1 {
		t.Fatalf("exit = %d, requests = %d, stdout = %q, stderr = %q", code, requests, stdout.String(), stderr.String())
	}
	var envelope struct {
		SchemaVersion int            `json:"schema_version"`
		OK            bool           `json:"ok"`
		Operation     string         `json:"operation"`
		Data          jsontext.Value `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	const wantData = `{"models":[{"path":"/server/a.safetensors","installed":true},{"path":"/server/z.safetensors","installed":false}]}`
	if envelope.SchemaVersion != 1 || !envelope.OK || envelope.Operation != "models.scan" || string(envelope.Data) != wantData {
		t.Fatalf("unexpected scan envelope: %s", stdout.String())
	}
}

func TestModelsScanAcceptsWindowsAbsoluteServerPath(t *testing.T) {
	isolateUserConfigDir(t)
	const serverPath = `C:\Models\checkpoints`
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/models/scan_folder" || r.URL.Query().Get("scan_path") != serverPath {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)
	code := app.Run(t.Context(), []string{"models", "scan", "--path", serverPath, "--url", server.URL, "--json"})
	if code != result.ExitSuccess || requests != 1 || stderr.Len() != 0 {
		t.Fatalf("exit = %d, requests = %d, stdout = %q, stderr = %q", code, requests, stdout.String(), stderr.String())
	}
}

func TestModelsScanRejectsMissingAndRelativeServerPathsBeforeNetwork(t *testing.T) {
	isolateUserConfigDir(t)
	for _, args := range [][]string{
		{"models", "scan"},
		{"models", "scan", "--path", "relative/models"},
		{"models", "scan", "--request", "-"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			app := cli.NewWithIO(strings.NewReader(`{"schema_version":1,"path":"relative/models"}`), &stdout, &stderr)
			code := app.Run(t.Context(), append(args, "--url", server.URL, "--json"))
			var envelope result.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if code != result.ExitInvalidRequest || requests != 0 || envelope.Operation != "models.scan" || envelope.Error == nil || envelope.Error.Code != result.CodeInvalidRequest || stderr.Len() != 0 {
				t.Fatalf("exit = %d, requests = %d, stdout = %q, stderr = %q", code, requests, stdout.String(), stderr.String())
			}
		})
	}
}

func TestModelsScanRequiresInstalledStateInBackendResponse(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"path":"/server/model.safetensors"}]`))
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)
	code := app.Run(t.Context(), []string{"models", "scan", "--path", "/server/models", "--url", server.URL, "--json"})
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if code != result.ExitInvokeAIFailure || envelope.Error == nil || envelope.Error.Code != result.CodeInvalidInvokeAIResponse || envelope.Operation != "models.scan" || stderr.Len() != 0 {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestModelsScanAcceptsRequestDocumentAndRejectsMixedPathFlag(t *testing.T) {
	isolateUserConfigDir(t)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Query().Get("scan_path") != "/server/from-document" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	for _, tc := range []struct {
		name     string
		extra    []string
		wantCode int
		wantErr  string
	}{
		{name: "document", wantCode: result.ExitSuccess},
		{name: "mixed flag", extra: []string{"--path", "/server/other"}, wantCode: result.ExitInvalidRequest, wantErr: result.CodeInvalidRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			app := cli.NewWithIO(strings.NewReader(`{"schema_version":1,"path":"/server/from-document"}`), &stdout, &stderr)
			args := append([]string{"models", "scan", "--request", "-"}, tc.extra...)
			code := app.Run(t.Context(), append(args, "--url", server.URL, "--json"))
			var envelope result.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if code != tc.wantCode || envelope.Operation != "models.scan" || stderr.Len() != 0 {
				t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
			}
			if tc.wantErr == "" {
				if !envelope.OK || requests != 1 || !bytes.Contains(stdout.Bytes(), []byte(`"models":[]`)) {
					t.Fatalf("unexpected document result: %q, requests = %d", stdout.String(), requests)
				}
			} else if envelope.Error == nil || envelope.Error.Code != tc.wantErr || requests != 1 {
				t.Fatalf("unexpected mixed-input result: %q, requests = %d", stdout.String(), requests)
			}
		})
	}
}

func TestModelsScanReportsFolderRejectionWithoutBackendBody(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"detail":"private backend field"}`))
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)
	code := app.Run(t.Context(), []string{"models", "scan", "--path", "/server/rejected", "--url", server.URL, "--json"})
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if code != result.ExitInvokeAIFailure || envelope.Operation != "models.scan" || envelope.Error == nil || envelope.Error.Code != result.CodeInvokeAIOperationFailed || envelope.Error.Details["status"] != float64(http.StatusBadRequest) || bytes.Contains(stdout.Bytes(), []byte("private backend field")) || stderr.Len() != 0 {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}
