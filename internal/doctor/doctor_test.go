package doctor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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

	report := Run(t.Context(), client, version.Info{Version: "test"})
	if !report.Ready {
		t.Fatalf("report not ready: %#v", report.Issues)
	}
	if report.InvokeAI.Version != "6.14.1" || !report.InvokeAI.SupportedVersion {
		t.Fatalf("unexpected InvokeAI report: %#v", report.InvokeAI)
	}
	foundGenerate := false
	for _, entry := range report.Capabilities {
		if entry.Operation == "generate" && entry.Family == "anima" && entry.Compatible {
			foundGenerate = true
		}
	}
	if !foundGenerate {
		t.Fatalf("Anima generation capability not ready: %#v", report.Capabilities)
	}
	if report.UISync["generate"] != "full" {
		t.Fatalf("unexpected UI sync: %#v", report.UISync)
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
			{Operation: "generate", Family: "anima", Compatible: true, UISync: "full"},
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
	if !strings.Contains(output.String(), "generate/anima compatible: true (UI sync: full)\n") {
		t.Fatalf("generation capability dimensions missing from human output: %q", output.String())
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
	if compatibility["images.upload"] || compatibility["generate"] {
		t.Fatalf("mutating capabilities should require the supported range: %#v", report.Capabilities)
	}
}

func TestRunOrdersRelevantModelsByTypeThenName(t *testing.T) {
	document := openAPIFixture("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "6.14.1"})
		case "/openapi.json":
			_ = json.NewEncoder(w).Encode(document)
		case "/api/v2/models/":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{
				{"key": "vae-b", "name": "Zeta VAE", "base": "anima", "type": "vae"},
				{"key": "main-b", "name": "Zeta", "base": "anima", "type": "main"},
				{"key": "encoder", "name": "Qwen3", "base": "any", "type": "qwen3_encoder"},
				{"key": "vae-a", "name": "Alpha VAE", "base": "anima", "type": "vae"},
				{"key": "near-miss", "name": "Anima V2", "base": "anima-v2", "type": "main"},
				{"key": "main-a", "name": "Alpha", "base": "anima", "type": "main"},
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

	wantRelevant := []ModelSummary{
		{Key: "main-a", Name: "Alpha", Base: "anima", Type: "main"},
		{Key: "main-b", Name: "Zeta", Base: "anima", Type: "main"},
		{Key: "encoder", Name: "Qwen3", Base: "any", Type: "qwen3_encoder"},
		{Key: "vae-a", Name: "Alpha VAE", Base: "anima", Type: "vae"},
		{Key: "vae-b", Name: "Zeta VAE", Base: "anima", Type: "vae"},
	}
	if !reflect.DeepEqual(report.Models.Relevant, wantRelevant) {
		t.Fatalf("relevant models = %#v, want %#v", report.Models.Relevant, wantRelevant)
	}
	wantAvailable := map[string]int{"Anima main model": 2, "Anima-compatible VAE": 2, "Qwen3 text encoder": 1}
	availableByName := make(map[string]int)
	for _, requirement := range report.Models.Requirements {
		if !requirement.Satisfied {
			t.Fatalf("requirement %q not satisfied by fixture: %#v", requirement.Name, report.Models.Requirements)
		}
		availableByName[requirement.Name] = requirement.Available
	}
	for name, want := range wantAvailable {
		if availableByName[name] != want {
			t.Fatalf("available %s models = %d, want %d: %#v", name, availableByName[name], want, report.Models.Requirements)
		}
	}
	compatibility := make(map[string]bool)
	for _, entry := range report.Capabilities {
		compatibility[entry.Operation] = entry.Compatible
	}
	if !compatibility["generate"] || !compatibility["models.list"] {
		t.Fatalf("readiness regressed for sample models: %#v", report.Capabilities)
	}
}

func TestRunDetectsMissingInvocationField(t *testing.T) {
	server := newInvokeAIServer(t, "scheduler")
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}

	report := Run(t.Context(), client, version.Info{Version: "test"})
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

	report := Run(t.Context(), client, version.Info{Version: "test"})
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

	report := Run(t.Context(), client, version.Info{Version: "test"})

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

	report := Run(t.Context(), client, version.Info{Version: "test"})

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

	report := Run(t.Context(), client, version.Info{Version: "test"})
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

	report := Run(t.Context(), client, version.Info{Version: "test"})
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

			report := Run(t.Context(), client, version.Info{Version: "test"})
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
