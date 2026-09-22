package doctor

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/version"
)

func TestRunReportsReadinessForImplementedCapabilities(t *testing.T) {
	server := newInvokeAIServer(t)
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
	wantOperations := []string{"models.list", "images.list", "images.get", "images.upload", "queue.list", "queue.get", "generate"}
	if !slices.Equal(operations, wantOperations) {
		t.Fatalf("reported operations = %q, want implemented operations %q", operations, wantOperations)
	}
	if len(report.OpenAPI.Invocations) == 0 {
		t.Fatal("doctor reported no required invocations")
	}
	for _, invocation := range report.OpenAPI.Invocations {
		if !invocation.Available || len(invocation.MissingProperties) != 0 {
			t.Fatalf("invocation check failed: %#v", invocation)
		}
	}
	if len(report.Models.Requirements) != 3 {
		t.Fatalf("model requirements count = %d, want 3", len(report.Models.Requirements))
	}
	for _, requirement := range report.Models.Requirements {
		if !requirement.Satisfied {
			t.Fatalf("model requirement not satisfied: %#v", requirement)
		}
	}
	if len(report.UISync) != 0 {
		t.Fatalf("doctor reported deferred UI sync before delivery step 4: %#v", report.UISync)
	}
}

func TestRunReportsInspectionAndUploadCapabilities(t *testing.T) {
	server := newInvokeAIServer(t)
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
	document := openAPIFixture(t)
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
	document := openAPIFixture(t)
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
		t.Fatalf("mutating and graph-producing capabilities should require the supported range: %#v", report.Capabilities)
	}
}

func TestRunClassifiesRejectedAuthentication(t *testing.T) {
	document := openAPIFixture(t)
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
	document := openAPIFixture(t)
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
	assertCapabilityFailurePresent(t, report, "models_unavailable")
	assertCapabilityFailurePrefixAbsent(t, report, "missing_component:")
}

func TestFailureClassifiesInvokeAIHTTPError(t *testing.T) {
	document := openAPIFixture(t)
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
	document := openAPIFixture(t)
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
			document := openAPIFixture(t)
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

func TestRunReportsAnimaGenerationReadyOnlyWhenAllRequirementsPass(t *testing.T) {
	server := newInvokeAIServer(t)
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}

	report := Run(t.Context(), client, version.Info{Version: "test"})
	if !report.Ready {
		t.Fatalf("report not ready: %#v", report.Issues)
	}

	var generateReport *CapabilityReport
	for _, entry := range report.Capabilities {
		if entry.Operation == result.OperationGenerate {
			generateReport = &entry
			break
		}
	}
	if generateReport == nil {
		t.Fatal("doctor capabilities did not contain generate operation")
	}
	if generateReport.Family != "anima" {
		t.Fatalf("Family = %q, want %q", generateReport.Family, "anima")
	}
	if !generateReport.Compatible {
		t.Fatalf("generate should be compatible, got failures: %#v", generateReport.Failures)
	}
	if len(generateReport.Failures) != 0 {
		t.Fatalf("unexpected generate failures: %#v", generateReport.Failures)
	}
	if generateReport.UISync != "" {
		t.Fatalf("generate UISync = %q, want unadvertised empty string", generateReport.UISync)
	}
	if _, ok := report.UISync["generate"]; ok {
		t.Fatalf("report.UISync unexpectedly contains generate: %#v", report.UISync)
	}
	if len(report.Issues) != 0 {
		t.Fatalf("unexpected issues: %#v", report.Issues)
	}
}

func TestRunReportsAnimaGenerationNegativeFixtures(t *testing.T) {
	t.Run("unsupported invokeai version", func(t *testing.T) {
		server := newCustomInvokeAIServer(t, "6.15.0", openAPIFixture(t), baselineModels)
		defer server.Close()
		client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
		if err != nil {
			t.Fatal(err)
		}

		report := Run(t.Context(), client, version.Info{Version: "test"})
		if report.Ready {
			t.Fatal("report should not be ready with unsupported InvokeAI version")
		}
		assertIssuePresent(t, report, "unsupported_invokeai_version")
		assertCapabilityFailureFor(t, report, result.OperationGenerate, "anima", "unsupported_version")
	})

	t.Run("missing required endpoints", func(t *testing.T) {
		endpoints := []struct {
			method string
			path   string
		}{
			{method: "GET", path: "/api/v1/app/version"},
			{method: "GET", path: "/api/v2/models/"},
			{method: "POST", path: "/api/v1/queue/{queue_id}/enqueue_batch"},
			{method: "GET", path: "/api/v1/queue/{queue_id}/i/{item_id}"},
			{method: "GET", path: "/api/v1/images/i/{image_name}"},
		}

		for _, endpoint := range endpoints {
			t.Run(endpoint.method+" "+endpoint.path, func(t *testing.T) {
				document := openAPIFixture(t)
				paths := document["paths"].(map[string]any)
				if methods, ok := paths[endpoint.path].(map[string]any); ok {
					delete(methods, strings.ToLower(endpoint.method))
					if len(methods) == 0 {
						delete(paths, endpoint.path)
					}
				}

				server := newCustomInvokeAIServer(t, "6.14.1", document, baselineModels)
				defer server.Close()
				client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
				if err != nil {
					t.Fatal(err)
				}

				report := Run(t.Context(), client, version.Info{Version: "test"})
				if report.Ready {
					t.Fatalf("report should not be ready when %s %s is missing", endpoint.method, endpoint.path)
				}
				assertIssuePresent(t, report, "missing_endpoint")
				assertCapabilityFailureFor(t, report, result.OperationGenerate, "anima", "missing_endpoint:"+endpoint.method+" "+endpoint.path)
			})
		}
	})

	t.Run("missing required invocation schemas", func(t *testing.T) {
		schemas := []struct {
			schema   string
			typeName string
		}{
			{schema: "AnimaModelLoaderInvocation", typeName: "anima_model_loader"},
			{schema: "StringInvocation", typeName: "string"},
			{schema: "AnimaTextEncoderInvocation", typeName: "anima_text_encoder"},
			{schema: "CollectInvocation", typeName: "collect"},
			{schema: "IntegerInvocation", typeName: "integer"},
			{schema: "AnimaDenoiseInvocation", typeName: "anima_denoise"},
			{schema: "CoreMetadataInvocation", typeName: "core_metadata"},
			{schema: "AnimaLatentsToImageInvocation", typeName: "anima_l2i"},
		}

		for _, item := range schemas {
			t.Run(item.schema, func(t *testing.T) {
				document := openAPIFixture(t)
				components := document["components"].(map[string]any)
				schemaMap := components["schemas"].(map[string]any)
				delete(schemaMap, item.schema)

				server := newCustomInvokeAIServer(t, "6.14.1", document, baselineModels)
				defer server.Close()
				client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
				if err != nil {
					t.Fatal(err)
				}

				report := Run(t.Context(), client, version.Info{Version: "test"})
				if report.Ready {
					t.Fatalf("report should not be ready when %s schema is missing", item.schema)
				}
				assertIssuePresent(t, report, "incompatible_invocation")
				assertCapabilityFailureFor(t, report, result.OperationGenerate, "anima", "incompatible_invocation:"+item.typeName)
			})
		}
	})

	t.Run("missing required invocation field", func(t *testing.T) {
		baseline := openAPIFixture(t)
		components := baseline["components"].(map[string]any)
		schemas := components["schemas"].(map[string]any)
		for _, schemaName := range slices.Sorted(maps.Keys(schemas)) {
			schema := schemas[schemaName].(map[string]any)
			properties := schema["properties"].(map[string]any)
			typeProperty := properties["type"].(map[string]any)
			typeName := typeProperty["const"].(string)
			for _, property := range slices.Sorted(maps.Keys(properties)) {
				t.Run(schemaName+"."+property, func(t *testing.T) {
					document := openAPIFixture(t)
					documentComponents := document["components"].(map[string]any)
					documentSchemas := documentComponents["schemas"].(map[string]any)
					documentSchema := documentSchemas[schemaName].(map[string]any)
					documentProperties := documentSchema["properties"].(map[string]any)
					delete(documentProperties, property)

					server := newCustomInvokeAIServer(t, "6.14.1", document, baselineModels)
					defer server.Close()
					client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
					if err != nil {
						t.Fatal(err)
					}

					report := Run(t.Context(), client, version.Info{Version: "test"})
					if report.Ready {
						t.Fatalf("report should not be ready when %s is missing from %s", property, schemaName)
					}
					assertIssuePresent(t, report, "incompatible_invocation")
					assertCapabilityFailureFor(t, report, result.OperationGenerate, "anima", "incompatible_invocation:"+typeName)
				})
			}
		}
	})

	t.Run("mismatched required invocation type", func(t *testing.T) {
		baseline := openAPIFixture(t)
		components := baseline["components"].(map[string]any)
		schemas := components["schemas"].(map[string]any)
		for _, schemaName := range slices.Sorted(maps.Keys(schemas)) {
			schema := schemas[schemaName].(map[string]any)
			properties := schema["properties"].(map[string]any)
			typeProperty := properties["type"].(map[string]any)
			typeName := typeProperty["const"].(string)
			t.Run(schemaName, func(t *testing.T) {
				document := openAPIFixture(t)
				documentComponents := document["components"].(map[string]any)
				documentSchemas := documentComponents["schemas"].(map[string]any)
				documentSchema := documentSchemas[schemaName].(map[string]any)
				documentProperties := documentSchema["properties"].(map[string]any)
				documentProperties["type"] = map[string]any{"const": "wrong_type"}

				server := newCustomInvokeAIServer(t, "6.14.1", document, baselineModels)
				defer server.Close()
				client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
				if err != nil {
					t.Fatal(err)
				}

				report := Run(t.Context(), client, version.Info{Version: "test"})
				if report.Ready {
					t.Fatalf("report should not be ready when %s identifies the wrong invocation type", schemaName)
				}
				assertIssuePresent(t, report, "incompatible_invocation")
				assertCapabilityFailureFor(t, report, result.OperationGenerate, "anima", "incompatible_invocation:"+typeName)
			})
		}
	})

	t.Run("missing required models", func(t *testing.T) {
		tests := []struct {
			name        string
			models      []map[string]string
			missingName string
		}{
			{
				name: "missing Anima main model",
				models: []map[string]string{
					{"key": "vae", "name": "VAE", "base": "anima", "type": "vae"},
					{"key": "encoder", "name": "Qwen3", "base": "any", "type": "qwen3_encoder"},
				},
				missingName: "Anima main model",
			},
			{
				name: "missing Anima VAE",
				models: []map[string]string{
					{"key": "main", "name": "Anima", "base": "anima", "type": "main"},
					{"key": "encoder", "name": "Qwen3", "base": "any", "type": "qwen3_encoder"},
				},
				missingName: "Anima VAE",
			},
			{
				name: "missing Qwen3 encoder",
				models: []map[string]string{
					{"key": "main", "name": "Anima", "base": "anima", "type": "main"},
					{"key": "vae", "name": "VAE", "base": "anima", "type": "vae"},
				},
				missingName: "Qwen3 encoder",
			},
			{
				name: "encoder with base anima instead of any",
				models: []map[string]string{
					{"key": "main", "name": "Anima", "base": "anima", "type": "main"},
					{"key": "vae", "name": "VAE", "base": "anima", "type": "vae"},
					{"key": "encoder", "name": "Qwen3", "base": "anima", "type": "qwen3_encoder"},
				},
				missingName: "Qwen3 encoder",
			},
		}

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				server := newCustomInvokeAIServer(t, "6.14.1", openAPIFixture(t), test.models)
				defer server.Close()
				client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
				if err != nil {
					t.Fatal(err)
				}

				report := Run(t.Context(), client, version.Info{Version: "test"})
				if report.Ready {
					t.Fatalf("report should not be ready when %s is missing", test.missingName)
				}
				assertIssuePresent(t, report, "missing_component")
				assertCapabilityFailureFor(t, report, result.OperationGenerate, "anima", "missing_component:"+test.missingName)
			})
		}
	})

	t.Run("incomplete required model identifier", func(t *testing.T) {
		models := []map[string]string{
			{"key": "main", "name": "Anima", "base": "anima", "type": "main", "format": "checkpoint"},
			{"key": "vae", "hash": "blake3:vae", "name": "VAE", "base": "anima", "type": "vae", "format": "checkpoint"},
			{"key": "encoder", "hash": "blake3:encoder", "name": "Qwen3", "base": "any", "type": "qwen3_encoder", "format": "checkpoint"},
		}
		server := newCustomInvokeAIServer(t, "6.14.1", openAPIFixture(t), models)
		defer server.Close()
		client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
		if err != nil {
			t.Fatal(err)
		}

		report := Run(t.Context(), client, version.Info{Version: "test"})
		assertCapabilityFailureFor(t, report, result.OperationGenerate, "anima", "missing_component:Anima main model")
	})
}

func TestDoctorDoesNotClaimGenerationUISynchronization(t *testing.T) {
	server := newInvokeAIServer(t)
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}

	report := Run(t.Context(), client, version.Info{Version: "test"})
	if _, ok := report.UISync["generate"]; ok {
		t.Fatalf("report.UISync contains generate: %#v", report.UISync)
	}
	if len(report.UISync) != 0 {
		t.Fatalf("report.UISync is not empty before step 4: %#v", report.UISync)
	}

	var output strings.Builder
	if err := report.Human(&output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "generate/anima compatible: true\n") {
		t.Fatalf("human output missing generate/anima: %q", output.String())
	}
	if strings.Contains(output.String(), "generate/anima compatible: true (UI sync:") {
		t.Fatalf("human output claims UI sync for generate: %q", output.String())
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

func assertCapabilityFailureFor(t *testing.T, report Report, operation, family, failure string) {
	t.Helper()
	for _, capabilityReport := range report.Capabilities {
		if capabilityReport.Operation != operation || capabilityReport.Family != family {
			continue
		}
		if slices.Contains(capabilityReport.Failures, failure) {
			return
		}
		t.Fatalf("capability %s/%s does not contain failure %q: %#v", operation, family, failure, capabilityReport)
	}
	t.Fatalf("capability %s/%s not found: %#v", operation, family, report.Capabilities)
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

var baselineModels = []map[string]string{
	{"key": "main", "hash": "blake3:main", "name": "Anima", "base": "anima", "type": "main", "format": "checkpoint"},
	{"key": "vae", "hash": "blake3:vae", "name": "VAE", "base": "anima", "type": "vae", "format": "checkpoint"},
	{"key": "encoder", "hash": "blake3:encoder", "name": "Qwen3", "base": "any", "type": "qwen3_encoder", "format": "checkpoint"},
}

func newCustomInvokeAIServer(t *testing.T, version string, document any, models any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": version})
		case "/openapi.json":
			_ = json.NewEncoder(w).Encode(document)
		case "/api/v2/models/":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": models})
		default:
			http.NotFound(w, r)
		}
	}))
}

func newInvokeAIServer(t *testing.T) *httptest.Server {
	t.Helper()
	return newCustomInvokeAIServer(t, "6.14.1", openAPIFixture(t), baselineModels)
}

func openAPIFixture(t *testing.T) map[string]any {
	t.Helper()
	encoded, err := os.ReadFile("testdata/invokeai_6_14_anima_openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := jsonv2.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	return document
}
