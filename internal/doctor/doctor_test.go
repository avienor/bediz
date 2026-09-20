package doctor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/version"
)

func TestRunReportsReadyAnimaCapability(t *testing.T) {
	server := newInvokeAIServer(t, "")
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}

	report := Run(context.Background(), client, version.Info{Version: "test"})
	if !report.Ready {
		t.Fatalf("report not ready: %#v", report.Issues)
	}
	if report.InvokeAI.Version != "6.14.1" || !report.InvokeAI.SupportedVersion {
		t.Fatalf("unexpected InvokeAI report: %#v", report.InvokeAI)
	}
	if len(report.Capabilities) != 1 || !report.Capabilities[0].Compatible {
		t.Fatalf("unexpected capabilities: %#v", report.Capabilities)
	}
	if report.UISync["generate"] != "full" {
		t.Fatalf("unexpected UI sync: %#v", report.UISync)
	}
}

func TestRunDetectsMissingInvocationField(t *testing.T) {
	server := newInvokeAIServer(t, "scheduler")
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}

	report := Run(context.Background(), client, version.Info{Version: "test"})
	if report.Ready {
		t.Fatal("report should not be ready")
	}
	found := false
	for _, check := range report.OpenAPI.Invocations {
		if check.Type == "anima_denoise" && len(check.MissingProperties) == 1 && check.MissingProperties[0] == "scheduler" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing property was not reported: %#v", report.OpenAPI.Invocations)
	}
	exitCode, code, _ := Failure(report)
	if exitCode != 4 || code != "unsupported_capability" {
		t.Fatalf("failure = (%d, %q)", exitCode, code)
	}
}

func TestRunClassifiesRejectedAuthentication(t *testing.T) {
	document := openAPIFixture("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "6.14.1"})
		case "/openapi.json":
			_ = json.NewEncoder(w).Encode(document)
		case "/api/v2/models/":
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := httpclient.New(server.URL, "wrong", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}

	report := Run(context.Background(), client, version.Info{Version: "test"})
	if report.InvokeAI.AuthenticationStatus != "rejected" {
		t.Fatalf("authentication status = %q", report.InvokeAI.AuthenticationStatus)
	}
	exitCode, code, _ := Failure(report)
	if exitCode != 5 || code != "authentication_failed" {
		t.Fatalf("failure = (%d, %q)", exitCode, code)
	}
}

func TestRunDoesNotDeriveSchemaIssuesWhenOpenAPIRequestFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "6.14.1"})
		case "/openapi.json":
			http.Error(w, "failed", http.StatusInternalServerError)
		case "/api/v2/models/":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}

	report := Run(context.Background(), client, version.Info{Version: "test"})

	assertIssuePresent(t, report, "invokeai_http_error")
	assertIssueAbsent(t, report, "missing_endpoint")
	assertIssueAbsent(t, report, "incompatible_invocation")
	assertCapabilityFailurePresent(t, report, "openapi_unavailable")
	assertCapabilityFailurePrefixAbsent(t, report, "missing_endpoint:")
	assertCapabilityFailurePrefixAbsent(t, report, "incompatible_invocation:")
}

func TestRunDoesNotDeriveComponentIssuesWhenModelsRequestFails(t *testing.T) {
	document := openAPIFixture("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "6.14.1"})
		case "/openapi.json":
			_ = json.NewEncoder(w).Encode(document)
		case "/api/v2/models/":
			http.Error(w, "failed", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}

	report := Run(context.Background(), client, version.Info{Version: "test"})

	assertIssuePresent(t, report, "invokeai_http_error")
	assertIssueAbsent(t, report, "missing_component")
	assertCapabilityFailurePresent(t, report, "models_unavailable")
	assertCapabilityFailurePrefixAbsent(t, report, "missing_component:")
}

func TestFailureClassifiesInvokeAIHTTPError(t *testing.T) {
	document := openAPIFixture("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			http.Error(w, "failed", http.StatusInternalServerError)
		case "/openapi.json":
			_ = json.NewEncoder(w).Encode(document)
		case "/api/v2/models/":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{
				{"key": "main", "name": "Anima", "base": "anima", "type": "main"},
				{"key": "vae", "name": "VAE", "base": "anima", "type": "vae"},
				{"key": "encoder", "name": "Qwen3", "base": "any", "type": "qwen3_encoder"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}

	report := Run(context.Background(), client, version.Info{Version: "test"})
	exitCode, code, _ := Failure(report)

	if exitCode != result.ExitInvokeAIFailure || code != "invokeai_http_error" {
		t.Fatalf("failure = (%d, %q), want (%d, %q)", exitCode, code, result.ExitInvokeAIFailure, "invokeai_http_error")
	}
}

func TestFailureClassifiesInvalidInvokeAIResponse(t *testing.T) {
	document := openAPIFixture("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte("not-json"))
		case "/openapi.json":
			_ = json.NewEncoder(w).Encode(document)
		case "/api/v2/models/":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{
				{"key": "main", "name": "Anima", "base": "anima", "type": "main"},
				{"key": "vae", "name": "VAE", "base": "anima", "type": "vae"},
				{"key": "encoder", "name": "Qwen3", "base": "any", "type": "qwen3_encoder"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}

	report := Run(context.Background(), client, version.Info{Version: "test"})
	exitCode, code, _ := Failure(report)

	if exitCode != result.ExitInvokeAIFailure || code != "invalid_invokeai_response" {
		t.Fatalf("failure = (%d, %q), want (%d, %q)", exitCode, code, result.ExitInvokeAIFailure, "invalid_invokeai_response")
	}
}

func TestFailureClassifiesInvalidVersionPayloads(t *testing.T) {
	tests := []struct {
		name         string
		version      string
		expectedCode string
	}{
		{name: "empty version", version: "", expectedCode: "invalid_version_response"},
		{name: "unparsable version", version: "latest", expectedCode: "invalid_invokeai_version"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := openAPIFixture("")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/app/version":
					_ = json.NewEncoder(w).Encode(map[string]string{"version": test.version})
				case "/openapi.json":
					_ = json.NewEncoder(w).Encode(document)
				case "/api/v2/models/":
					_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{
						{"key": "main", "name": "Anima", "base": "anima", "type": "main"},
						{"key": "vae", "name": "VAE", "base": "anima", "type": "vae"},
						{"key": "encoder", "name": "Qwen3", "base": "any", "type": "qwen3_encoder"},
					}})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}

			report := Run(context.Background(), client, version.Info{Version: "test"})
			exitCode, code, _ := Failure(report)

			if exitCode != result.ExitInvokeAIFailure || code != test.expectedCode {
				t.Fatalf("failure = (%d, %q), want (%d, %q)", exitCode, code, result.ExitInvokeAIFailure, test.expectedCode)
			}
		})
	}
}

func assertIssuePresent(t *testing.T, report Report, code string) {
	t.Helper()
	for _, issue := range report.Issues {
		if issue.Code == code {
			return
		}
	}
	t.Fatalf("issue %q not found: %#v", code, report.Issues)
}

func assertIssueAbsent(t *testing.T, report Report, code string) {
	t.Helper()
	for _, issue := range report.Issues {
		if issue.Code == code {
			t.Fatalf("unexpected issue %q: %#v", code, report.Issues)
		}
	}
}

func assertCapabilityFailurePresent(t *testing.T, report Report, failure string) {
	t.Helper()
	for _, capabilityReport := range report.Capabilities {
		for _, candidate := range capabilityReport.Failures {
			if candidate == failure {
				return
			}
		}
	}
	t.Fatalf("capability failure %q not found: %#v", failure, report.Capabilities)
}

func assertCapabilityFailurePrefixAbsent(t *testing.T, report Report, prefix string) {
	t.Helper()
	for _, capabilityReport := range report.Capabilities {
		for _, failure := range capabilityReport.Failures {
			if strings.HasPrefix(failure, prefix) {
				t.Fatalf("unexpected capability failure prefix %q: %#v", prefix, report.Capabilities)
			}
		}
	}
}

func newInvokeAIServer(t *testing.T, missingProperty string) *httptest.Server {
	t.Helper()
	document := openAPIFixture(missingProperty)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "6.14.1"})
		case "/openapi.json":
			_ = json.NewEncoder(w).Encode(document)
		case "/api/v2/models/":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{
				{"key": "main", "name": "Anima", "base": "anima", "type": "main", "format": "checkpoint"},
				{"key": "vae", "name": "VAE", "base": "anima", "type": "vae", "format": "checkpoint"},
				{"key": "encoder", "name": "Qwen3", "base": "any", "type": "qwen3_encoder", "format": "checkpoint"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
}

func openAPIFixture(missingProperty string) map[string]any {
	paths := make(map[string]any)
	schemas := make(map[string]any)
	for _, entry := range capability.Matrix {
		for _, endpoint := range entry.Endpoints {
			methods, _ := paths[endpoint.Path].(map[string]any)
			if methods == nil {
				methods = make(map[string]any)
				paths[endpoint.Path] = methods
			}
			methods[strings.ToLower(endpoint.Method)] = map[string]any{}
		}
		for _, invocation := range entry.Invocations {
			properties := map[string]any{"type": map[string]any{"const": invocation.Type}}
			for _, property := range invocation.Properties {
				if property != missingProperty {
					properties[property] = map[string]any{}
				}
			}
			schemas[invocation.Schema] = map[string]any{"properties": properties}
		}
	}
	return map[string]any{
		"paths": paths,
		"components": map[string]any{
			"schemas": schemas,
		},
	}
}
