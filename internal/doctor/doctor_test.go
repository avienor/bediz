package doctor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/version"
)

func TestRunReportsReadinessForImplementedCapabilities(t *testing.T) {
	server := newInvokeAIServer(t, "")
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}

	report := Run(t.Context(), client, version.Info{Version: "test"})
	if !report.Ready {
		t.Fatalf("report not ready: %#v", report.Issues)
	}
	if report.InvokeAI.Version != "6.14.1" || !report.InvokeAI.SupportedVersion {
		t.Fatalf("unexpected InvokeAI report: %#v", report.InvokeAI)
	}
	operations := make([]string, len(report.Capabilities))
	for _, entry := range report.Capabilities {
		if !entry.Compatible {
			t.Fatalf("implemented capability is not ready: %#v", entry)
		}
	}
	for i, entry := range report.Capabilities {
		operations[i] = entry.Operation
	}
	wantOperations := []string{"models.list", "images.list", "images.get", "images.upload", "queue.list", "queue.get"}
	if !slices.Equal(operations, wantOperations) {
		t.Fatalf("reported operations = %q, want implemented operations %q", operations, wantOperations)
	}
	if len(report.OpenAPI.Invocations) != 0 || len(report.Models.Relevant) != 0 || len(report.Models.Requirements) != 0 || len(report.UISync) != 0 {
		t.Fatalf("doctor reported deferred generation readiness: %#v", report)
	}
}

func TestRunReportsInspectionAndUploadCapabilities(t *testing.T) {
	server := newInvokeAIServer(t, "")
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}

	report := Run(t.Context(), client, version.Info{Version: "test"})

	got := make(map[string]bool)
	for _, entry := range report.Capabilities {
		got[entry.Operation] = entry.Compatible
	}
	for _, operation := range []string{"models.list", "images.list", "images.get", "images.upload", "queue.list", "queue.get"} {
		if !got[operation] {
			t.Errorf("capability %q missing or incompatible: %#v", operation, report.Capabilities)
		}
	}
	encoded, err := json.Marshal(report.Capabilities)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"family":""`) || strings.Contains(string(encoded), `"ui_sync":""`) {
		t.Fatalf("empty capability dimensions should be omitted: %s", encoded)
	}
}

func TestHumanOmitsEmptyCapabilityDimensions(t *testing.T) {
	report := Report{
		Bediz:    version.Info{Version: "test"},
		InvokeAI: InvokeAIReport{Version: "6.14.1"},
		Capabilities: []CapabilityReport{
			{Operation: "models.list", Compatible: true},
		},
	}
	var output strings.Builder

	report.Human(&output)

	if !strings.Contains(output.String(), "models.list compatible: true\n") {
		t.Fatalf("inspection capability missing from human output: %q", output.String())
	}
	if strings.Contains(output.String(), "models.list/") || strings.Contains(output.String(), "UI sync: )") {
		t.Fatalf("empty capability dimensions present in human output: %q", output.String())
	}
}

func TestHumanShowsUnknownInvokeAIVersionFallback(t *testing.T) {
	report := Report{
		Bediz:    version.Info{Version: "test"},
		InvokeAI: InvokeAIReport{URL: "http://127.0.0.1:9090"},
	}
	var output strings.Builder
	report.Human(&output)

	want := "Bediz test\n" +
		"InvokeAI unknown (http://127.0.0.1:9090, supported: false)\n" +
		"Connection: ; authentication: \n" +
		"OpenAPI: false; models: 0\n" +
		"Status: not ready\n"
	if output.String() != want {
		t.Fatalf("human output = %q, want %q", output.String(), want)
	}
}

func TestRunReportsQueueGetIncompatibleWithoutImageInspectionEndpoint(t *testing.T) {
	document := openAPIFixture("")
	paths := document["paths"].(map[string]any)
	delete(paths, "/api/v1/images/i/{image_name}")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "6.14.1"})
		case "/openapi.json":
			_ = json.NewEncoder(w).Encode(document)
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

	report := Run(t.Context(), client, version.Info{Version: "test"})

	for _, entry := range report.Capabilities {
		if entry.Operation == "queue.get" {
			if entry.Compatible {
				t.Fatalf("queue.get should require image inspection: %#v", entry)
			}
			return
		}
	}
	t.Fatal("queue.get capability not reported")
}

func TestRunAllowsReadOnlyInspectionOnEndpointCompatibleUntestedVersion(t *testing.T) {
	document := openAPIFixture("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "6.15.0"})
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

	report := Run(t.Context(), client, version.Info{Version: "test"})

	compatibility := make(map[string]bool)
	for _, entry := range report.Capabilities {
		compatibility[entry.Operation] = entry.Compatible
	}
	for _, operation := range []string{"models.list", "images.list", "images.get", "queue.list", "queue.get"} {
		if !compatibility[operation] {
			t.Errorf("read-only capability %q should remain compatible: %#v", operation, report.Capabilities)
		}
	}
	if compatibility["images.upload"] {
		t.Fatalf("mutating capabilities should require the supported range: %#v", report.Capabilities)
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

	report := Run(t.Context(), client, version.Info{Version: "test"})
	if report.InvokeAI.AuthenticationStatus != "rejected" {
		t.Fatalf("authentication status = %q", report.InvokeAI.AuthenticationStatus)
	}
	failure := Failure(report)
	if failure == nil || failure.Code != result.CodeAuthenticationFailed {
		t.Fatalf("failure = %#v", failure)
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

	report := Run(t.Context(), client, version.Info{Version: "test"})

	assertIssuePresent(t, report, "invokeai_http_error")
	assertIssueAbsent(t, report, "missing_endpoint")
	assertIssueAbsent(t, report, "incompatible_invocation")
	assertCapabilityFailurePresent(t, report, "openapi_unavailable")
	assertCapabilityFailurePrefixAbsent(t, report, "missing_endpoint:")
	assertCapabilityFailurePrefixAbsent(t, report, "incompatible_invocation:")
}

func TestRunDoesNotInventComponentReadinessWhenModelsRequestFails(t *testing.T) {
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

	report := Run(t.Context(), client, version.Info{Version: "test"})

	if report.Ready {
		t.Fatal("report should not be ready when implemented model inspection fails")
	}
	assertIssuePresent(t, report, "invokeai_http_error")
	assertIssueAbsent(t, report, "missing_component")
	assertCapabilityFailurePrefixAbsent(t, report, "models_unavailable")
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

	report := Run(t.Context(), client, version.Info{Version: "test"})
	failure := Failure(report)

	if failure == nil || failure.Code != result.CodeInvokeAIHTTPError {
		t.Fatalf("failure = %#v, want %q", failure, result.CodeInvokeAIHTTPError)
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

	report := Run(t.Context(), client, version.Info{Version: "test"})
	failure := Failure(report)

	if failure == nil || failure.Code != result.CodeInvalidInvokeAIResponse {
		t.Fatalf("failure = %#v, want %q", failure, result.CodeInvalidInvokeAIResponse)
	}
}

func TestFailureClassifiesInvalidVersionPayloads(t *testing.T) {
	tests := []struct {
		name         string
		version      string
		expectedCode string
	}{
		{name: "empty version", version: "", expectedCode: result.CodeInvalidVersionResponse},
		{name: "unparsable version", version: "latest", expectedCode: result.CodeInvalidInvokeAIVersion},
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

			report := Run(t.Context(), client, version.Info{Version: "test"})
			failure := Failure(report)

			if failure == nil || failure.Code != test.expectedCode {
				t.Fatalf("failure = %#v, want %q", failure, test.expectedCode)
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
