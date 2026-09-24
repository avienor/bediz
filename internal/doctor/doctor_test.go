package doctor

import (
	"cmp"
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
	wantOperations := []string{"models.list", "models.install", "models.install", "models.status", "images.list", "images.get", "images.upload", "queue.list", "queue.get", "boards.list", "boards.get", "generate", "generate", "generate", "upscale", "upscale", "recall", "auth.huggingface.status", "auth.huggingface.login", "auth.huggingface.logout"}
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
	if len(report.Models.Requirements) != 13 {
		t.Fatalf("model requirements count = %d, want 13", len(report.Models.Requirements))
	}
	for _, requirement := range report.Models.Requirements {
		if !requirement.Satisfied {
			t.Fatalf("model requirement not satisfied: %#v", requirement)
		}
	}
	if report.UISync["generate"] != "partial" {
		t.Fatalf("doctor UI synchronization = %#v, want partial generation", report.UISync)
	}
}

func TestDoctorStarterCapabilityRequiresCatalogResponseContract(t *testing.T) {
	document := openAPIFixture(t)
	paths := document["paths"].(map[string]any)
	paths["/api/v2/models/starter_models"].(map[string]any)["get"] = map[string]any{}
	server := newCustomInvokeAIServer(t, "6.14.1", document, baselineModels)
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	report := Run(t.Context(), client, version.Info{Version: "test"})
	var generic, starter *CapabilityReport
	for i := range report.Capabilities {
		entry := &report.Capabilities[i]
		if entry.Operation == result.OperationModelsInstall && entry.Family == "starter" {
			starter = entry
		} else if entry.Operation == result.OperationModelsInstall && entry.Family == "" {
			generic = entry
		}
	}
	if generic == nil || !generic.Compatible || starter == nil || starter.Compatible || !slices.Contains(starter.Failures, "incompatible_starter_catalog_response") {
		t.Fatalf("generic=%#v starter=%#v", generic, starter)
	}
}

func TestDoctorDoesNotAdvertiseInstallWithoutGenericSourceParameter(t *testing.T) {
	document := openAPIFixture(t)
	paths := document["paths"].(map[string]any)
	paths["/api/v2/models/install"].(map[string]any)["post"] = map[string]any{}
	server := newCustomInvokeAIServer(t, "6.14.1", document, baselineModels)
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	report := Run(t.Context(), client, version.Info{Version: "test"})
	for _, entry := range report.Capabilities {
		if entry.Operation == result.OperationModelsInstall {
			if entry.Compatible || !slices.Contains(entry.Failures, "incompatible_install_schema:source") {
				t.Fatalf("install capability = %#v", entry)
			}
			return
		}
	}
	t.Fatal("install capability absent")
}

func TestDoctorDoesNotAdvertiseInstallWithoutInspectableJobResponse(t *testing.T) {
	document := openAPIFixture(t)
	post := document["paths"].(map[string]any)["/api/v2/models/install"].(map[string]any)["post"].(map[string]any)
	delete(post, "responses")
	server := newCustomInvokeAIServer(t, "6.14.1", document, baselineModels)
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	report := Run(t.Context(), client, version.Info{Version: "test"})
	for _, entry := range report.Capabilities {
		if entry.Operation == result.OperationModelsInstall {
			if entry.Compatible || !slices.Contains(entry.Failures, "incompatible_install_schema:job_response") {
				t.Fatalf("install capability = %#v", entry)
			}
			return
		}
	}
	t.Fatal("install capability absent")
}

func TestDoctorChecksHuggingFaceAuthMethodsAndLoginBody(t *testing.T) {
	document := openAPIFixture(t)
	paths := document["paths"].(map[string]any)
	authPath := paths["/api/v2/models/hf_login"].(map[string]any)
	delete(authPath, "delete")
	authPath["post"] = map[string]any{"requestBody": map[string]any{}}
	server := newCustomInvokeAIServer(t, "6.15.0", document, baselineModels)
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	report := Run(t.Context(), client, version.Info{Version: "test"})
	got := make(map[string]CapabilityReport)
	for _, entry := range report.Capabilities {
		got[entry.Operation] = entry
	}
	if !got[result.OperationAuthHFStatus].Compatible {
		t.Fatalf("read-only status unavailable: %#v", got[result.OperationAuthHFStatus])
	}
	if got[result.OperationAuthHFLogin].Compatible || !slices.Contains(got[result.OperationAuthHFLogin].Failures, "unsupported_version") || !slices.Contains(got[result.OperationAuthHFLogin].Failures, "incompatible_hf_login_schema:token") {
		t.Fatalf("login capability: %#v", got[result.OperationAuthHFLogin])
	}
	if got[result.OperationAuthHFLogout].Compatible || !slices.Contains(got[result.OperationAuthHFLogout].Failures, "missing_endpoint:DELETE /api/v2/models/hf_login") {
		t.Fatalf("logout capability: %#v", got[result.OperationAuthHFLogout])
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
	for _, operation := range []string{"models.list", "models.install", "models.status", "images.list", "images.get", "images.upload", "queue.list", "queue.get", "boards.list", "boards.get"} {
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
	for _, operation := range []string{"models.list", "models.status", "images.list", "images.get", "queue.list", "queue.get", "boards.list", "boards.get"} {
		if !compatibility[operation] {
			t.Errorf("read-only capability %q should remain compatible: %#v", operation, report.Capabilities)
		}
	}
	if compatibility["models.install"] || compatibility["images.upload"] || compatibility["generate"] {
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
	if generateReport.UISync != "partial" {
		t.Fatalf("generate UISync = %q, want partial", generateReport.UISync)
	}
	if report.UISync["generate"] != "partial" {
		t.Fatalf("report.UISync = %#v, want partial generation", report.UISync)
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
		assertCapabilityFailureFor(t, report, result.OperationRecall, "", "unsupported_version")
		if report.UISync["generate"] != "" {
			t.Fatalf("unsupported version claims UI synchronization: %#v", report.UISync)
		}
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
			if !slices.Contains(animaInvocationSchemas, schemaName) {
				continue
			}
			schema := schemas[schemaName].(map[string]any)
			properties := schema["properties"].(map[string]any)
			typeProperty := properties["type"].(map[string]any)
			typeName := typeProperty["const"].(string)
			for _, property := range slices.Sorted(maps.Keys(properties)) {
				if schemaName == "CoreMetadataInvocation" && slices.Contains([]string{"cfg_rescale_multiplier", "rand_device"}, property) {
					continue
				}
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
			if !slices.Contains(animaInvocationSchemas, schemaName) {
				continue
			}
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

func TestDoctorReportsVerifiedPartialGenerationUISynchronization(t *testing.T) {
	server := newInvokeAIServer(t)
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}

	report := Run(t.Context(), client, version.Info{Version: "test"})
	if report.UISync["generate"] != "partial" {
		t.Fatalf("report.UISync = %#v, want partial generation", report.UISync)
	}

	var output strings.Builder
	if err := report.Human(&output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "generate/anima compatible: true (UI sync: partial)\n") {
		t.Fatalf("human output missing generate/anima: %q", output.String())
	}
	if strings.Contains(output.String(), "UI sync: full") {
		t.Fatalf("human output overstates UI synchronization: %q", output.String())
	}
}

func TestDoctorReportsSDXLOnlyWithSchemaModelAndRecallCFG(t *testing.T) {
	for _, test := range []struct {
		name     string
		edit     func(map[string]any, *[]map[string]string)
		failure  string
		wantSync string
	}{
		{"ready", func(map[string]any, *[]map[string]string) {}, "", "partial"},
		{"missing SDXL model", func(_ map[string]any, models *[]map[string]string) {
			*models = slices.DeleteFunc(*models, func(model map[string]string) bool { return model["key"] == "sdxl" })
		}, "missing_component:SDXL main model", ""},
		{"missing noise schema", func(document map[string]any, _ *[]map[string]string) {
			delete(document["components"].(map[string]any)["schemas"].(map[string]any), "NoiseInvocation")
		}, "incompatible_invocation:noise", ""},
		{"missing SDXL metadata field", func(document map[string]any, _ *[]map[string]string) {
			delete(document["components"].(map[string]any)["schemas"].(map[string]any)["CoreMetadataInvocation"].(map[string]any)["properties"].(map[string]any), "rand_device")
		}, "incompatible_invocation:core_metadata", ""},
		{"missing CFG Recall", func(document map[string]any, _ *[]map[string]string) {
			delete(document["components"].(map[string]any)["schemas"].(map[string]any)["RecallParameter"].(map[string]any)["properties"].(map[string]any), "cfg_scale")
		}, "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := openAPIFixture(t)
			models := slices.Clone(baselineModels)
			test.edit(document, &models)
			server := newCustomInvokeAIServer(t, "6.14.1", document, models)
			defer server.Close()
			client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			report := Run(t.Context(), client, version.Info{Version: "test"})
			var sdxl CapabilityReport
			for _, entry := range report.Capabilities {
				if entry.Operation == "generate" && entry.Family == "sdxl" {
					sdxl = entry
				}
			}
			if sdxl.Family != "sdxl" || sdxl.UISync != test.wantSync || sdxl.Compatible != (test.failure == "") || (test.failure != "" && !slices.Contains(sdxl.Failures, test.failure)) {
				t.Fatalf("SDXL capability = %#v", sdxl)
			}
		})
	}
}

func TestDoctorReportsSDXLUpscaleOnlyWithTestedRequirements(t *testing.T) {
	for _, test := range []struct {
		name    string
		edit    func(map[string]any, *[]map[string]string)
		failure string
		// entrySync is upscale/sdxl's ui_sync; reportSync is ui_sync.upscale,
		// which upscale/sd-1 keeps partial when only SDXL requirements fail.
		entrySync, reportSync string
		version               string
	}{
		{name: "ready", edit: func(map[string]any, *[]map[string]string) {}, entrySync: "partial", reportSync: "partial"},
		{name: "missing Recall endpoint", edit: func(document map[string]any, _ *[]map[string]string) {
			delete(document["paths"].(map[string]any), "/api/v1/recall/{queue_id}")
		}},
		{name: "incompatible Recall seed", edit: func(document map[string]any, _ *[]map[string]string) {
			delete(document["components"].(map[string]any)["schemas"].(map[string]any)["RecallParameter"].(map[string]any)["properties"].(map[string]any), "seed")
		}},
		{name: "unsupported version", edit: func(map[string]any, *[]map[string]string) {}, failure: "unsupported_version", version: "6.15.0"},
		{name: "non-normal main", reportSync: "partial", edit: func(_ map[string]any, models *[]map[string]string) {
			(*models)[7] = maps.Clone((*models)[7])
			(*models)[7]["variant"] = "inpaint"
		}, failure: "missing_component:SDXL normal main model"},
		{name: "missing Spandrel", edit: func(_ map[string]any, models *[]map[string]string) {
			*models = slices.DeleteFunc(*models, func(model map[string]string) bool { return model["key"] == "upscale" })
		}, failure: "missing_component:Spandrel upscale model"},
		{name: "missing ControlNet", reportSync: "partial", edit: func(_ map[string]any, models *[]map[string]string) {
			*models = slices.DeleteFunc(*models, func(model map[string]string) bool { return model["key"] == "tile" })
		}, failure: "missing_component:SDXL ControlNet"},
		{name: "missing tiled denoiser", edit: func(document map[string]any, _ *[]map[string]string) {
			delete(document["components"].(map[string]any)["schemas"].(map[string]any), "TiledMultiDiffusionDenoiseLatents")
		}, failure: "incompatible_invocation:tiled_multi_diffusion_denoise_latents"},
		{name: "metadata rejects upscale fields", edit: func(document map[string]any, _ *[]map[string]string) {
			delete(document["components"].(map[string]any)["schemas"].(map[string]any)["CoreMetadataInvocation"].(map[string]any), "additionalProperties")
		}, failure: "incompatible_invocation:core_metadata"},
		{name: "missing upload endpoint", edit: func(document map[string]any, _ *[]map[string]string) {
			delete(document["paths"].(map[string]any), "/api/v1/images/upload")
		}, failure: "missing_endpoint:POST /api/v1/images/upload"},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := openAPIFixture(t)
			models := slices.Clone(baselineModels)
			test.edit(document, &models)
			invokeAIVersion := cmp.Or(test.version, "6.14.1")
			server := newCustomInvokeAIServer(t, invokeAIVersion, document, models)
			defer server.Close()
			client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			report := Run(t.Context(), client, version.Info{Version: "test"})
			if report.UISync[result.OperationUpscale] != test.reportSync {
				t.Fatalf("report.UISync = %#v, want upscale %q", report.UISync, test.reportSync)
			}
			for _, entry := range report.Capabilities {
				if entry.Operation != result.OperationUpscale || entry.Family != "sdxl" {
					continue
				}
				if entry.Compatible != (test.failure == "") || entry.UISync != test.entrySync || (test.failure != "" && !slices.Contains(entry.Failures, test.failure)) {
					t.Fatalf("upscale capability = %#v", entry)
				}
				return
			}
			t.Fatal("missing upscale/sdxl capability")
		})
	}
}

func TestDoctorReportsSD1UpscaleOnlyWithTestedRequirements(t *testing.T) {
	for _, test := range []struct {
		name    string
		edit    func(map[string]any, *[]map[string]string)
		failure string
	}{
		{"ready", func(map[string]any, *[]map[string]string) {}, ""},
		{"non-normal main", func(_ map[string]any, models *[]map[string]string) {
			index := slices.IndexFunc(*models, func(model map[string]string) bool { return model["key"] == "sd1" })
			(*models)[index] = maps.Clone((*models)[index])
			(*models)[index]["variant"] = "inpaint"
		}, "missing_component:SD1.5 normal main model"},
		{"missing Spandrel", func(_ map[string]any, models *[]map[string]string) {
			*models = slices.DeleteFunc(*models, func(model map[string]string) bool { return model["key"] == "upscale" })
		}, "missing_component:Spandrel upscale model"},
		{"only SDXL ControlNet", func(_ map[string]any, models *[]map[string]string) {
			*models = slices.DeleteFunc(*models, func(model map[string]string) bool { return model["key"] == "sd1-tile" })
		}, "missing_component:SD1.5 ControlNet"},
		{"missing clip skip", func(document map[string]any, _ *[]map[string]string) {
			delete(document["components"].(map[string]any)["schemas"].(map[string]any), "CLIPSkipInvocation")
		}, "incompatible_invocation:clip_skip"},
		{"missing compel", func(document map[string]any, _ *[]map[string]string) {
			delete(document["components"].(map[string]any)["schemas"].(map[string]any), "CompelInvocation")
		}, "incompatible_invocation:compel"},
		{"missing main loader", func(document map[string]any, _ *[]map[string]string) {
			delete(document["components"].(map[string]any)["schemas"].(map[string]any), "MainModelLoaderInvocation")
		}, "incompatible_invocation:main_model_loader"},
		{"missing upload endpoint", func(document map[string]any, _ *[]map[string]string) {
			delete(document["paths"].(map[string]any), "/api/v1/images/upload")
		}, "missing_endpoint:POST /api/v1/images/upload"},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := openAPIFixture(t)
			models := slices.Clone(baselineModels)
			test.edit(document, &models)
			server := newCustomInvokeAIServer(t, "6.14.1", document, models)
			defer server.Close()
			client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			report := Run(t.Context(), client, version.Info{Version: "test"})
			var sd1, sdxl *CapabilityReport
			for index, entry := range report.Capabilities {
				if entry.Operation == result.OperationUpscale && entry.Family == "sd-1" {
					sd1 = &report.Capabilities[index]
				}
				if entry.Operation == result.OperationUpscale && entry.Family == "sdxl" {
					sdxl = &report.Capabilities[index]
				}
			}
			wantSync := ""
			if test.failure == "" {
				wantSync = "partial"
			}
			if sd1 == nil || sd1.Compatible != (test.failure == "") || sd1.UISync != wantSync || (test.failure != "" && !slices.Contains(sd1.Failures, test.failure)) {
				t.Fatalf("upscale/sd-1 capability = %#v", sd1)
			}
			sdxlAffected := test.name == "missing Spandrel" || test.name == "missing upload endpoint"
			if sdxl == nil || sdxl.Compatible == sdxlAffected {
				t.Fatalf("upscale/sdxl capability = %#v", sdxl)
			}
		})
	}
}

func TestDoctorReportsMissingRecallRequirementsSeparatelyFromDirectExecution(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(map[string]any)
		wantFailure string
	}{
		{"missing endpoint", func(document map[string]any) {
			delete(document["paths"].(map[string]any), "/api/v1/recall/{queue_id}")
		}, "missing_endpoint:POST /api/v1/recall/{queue_id}"},
		{"wrong request schema", func(document map[string]any) {
			path := document["paths"].(map[string]any)["/api/v1/recall/{queue_id}"].(map[string]any)
			post := path["post"].(map[string]any)
			post["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["$ref"] = "#/components/schemas/Other"
		}, "incompatible_recall_schema:request_body"},
		{"missing seed patch field", func(document map[string]any) {
			schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
			delete(schemas["RecallParameter"].(map[string]any)["properties"].(map[string]any), "seed")
		}, "incompatible_recall_schema:seed"},
		{"wrong seed patch type", func(document map[string]any) {
			schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
			seed := schemas["RecallParameter"].(map[string]any)["properties"].(map[string]any)["seed"].(map[string]any)
			seed["anyOf"] = []any{map[string]any{"type": "string"}, map[string]any{"type": "null"}}
		}, "incompatible_recall_schema:seed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := openAPIFixture(t)
			test.mutate(document)
			server := newCustomInvokeAIServer(t, "6.14.1", document, baselineModels)
			defer server.Close()
			client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			report := Run(t.Context(), client, version.Info{Version: "test"})
			if report.Ready {
				t.Fatal("doctor should report missing Recall readiness")
			}
			if len(report.UISync) != 0 {
				t.Fatalf("unverified UI synchronization: %#v", report.UISync)
			}
			for _, entry := range report.Capabilities {
				switch entry.Operation {
				case "generate", "upscale":
					if !entry.Compatible || entry.UISync != "" {
						t.Fatalf("Direct Execution should remain compatible: %#v", entry)
					}
				case "recall":
					if entry.Compatible || !slices.Contains(entry.Failures, test.wantFailure) {
						t.Fatalf("missing precise Recall failure %q: %#v", test.wantFailure, entry)
					}
				}
			}
		})
	}
}

func TestDoctorAcceptsReversedRecallNullableAlternatives(t *testing.T) {
	document := openAPIFixture(t)
	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	seed := schemas["RecallParameter"].(map[string]any)["properties"].(map[string]any)["seed"].(map[string]any)
	seed["anyOf"] = []any{map[string]any{"type": "null"}, map[string]any{"type": "integer"}}
	server := newCustomInvokeAIServer(t, "6.14.1", document, baselineModels)
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	report := Run(t.Context(), client, version.Info{Version: "test"})
	if !report.Ready || report.UISync["generate"] != "partial" {
		t.Fatalf("valid Recall schema should preserve partial readiness: %#v", report)
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
	{"key": "flux-main", "hash": "blake3:flux-main", "name": "FLUX dev", "base": "flux", "type": "main", "format": "checkpoint", "variant": "dev"},
	{"key": "flux-vae", "hash": "blake3:flux-vae", "name": "FLUX VAE", "base": "flux", "type": "vae", "format": "checkpoint"},
	{"key": "flux-t5", "hash": "blake3:flux-t5", "name": "T5", "base": "any", "type": "t5_encoder", "format": "diffusers"},
	{"key": "flux-clip", "hash": "blake3:flux-clip", "name": "CLIP", "base": "any", "type": "clip_embed", "format": "diffusers"},
	{"key": "sdxl", "hash": "blake3:sdxl", "name": "SDXL", "base": "sdxl", "type": "main", "format": "diffusers", "variant": "normal"},
	{"key": "upscale", "hash": "blake3:upscale", "name": "RealESRGAN x4plus", "base": "any", "type": "spandrel_image_to_image", "format": "checkpoint"},
	{"key": "tile", "hash": "blake3:tile", "name": "Tile", "base": "sdxl", "type": "controlnet", "format": "diffusers"},
	{"key": "sd1", "hash": "blake3:sd1", "name": "Dreamshaper 8", "base": "sd-1", "type": "main", "format": "diffusers", "variant": "normal"},
	{"key": "sd1-tile", "hash": "blake3:sd1-tile", "name": "Tile", "base": "sd-1", "type": "controlnet", "format": "diffusers"},
}

func TestDoctorFLUXMainRequirementUsesVariantAndFormat(t *testing.T) {
	for _, tc := range []struct {
		name, variant, format string
		compatible            bool
	}{
		{"dev", "dev", "checkpoint", true},
		{"schnell", "schnell", "gguf_quantized", true},
		{"dev fill", "dev_fill", "checkpoint", false},
		{"unknown", "", "checkpoint", false},
		{"SDNQ", "dev", "sdnq_quantized", false},
		{"diffusers", "dev", "diffusers", false},
		{"unknown format", "schnell", "unknown", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			models := slices.Clone(baselineModels)
			models[3] = maps.Clone(models[3])
			models[3]["variant"], models[3]["format"] = tc.variant, tc.format
			server := newCustomInvokeAIServer(t, "6.14.1", openAPIFixture(t), models)
			defer server.Close()
			client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			report := Run(t.Context(), client, version.Info{Version: "test"})
			for _, entry := range report.Capabilities {
				if entry.Operation == result.OperationGenerate && entry.Family == "flux" {
					if entry.Compatible != tc.compatible {
						t.Fatalf("FLUX capability = %#v", entry)
					}
					if tc.compatible && entry.UISync != "partial" {
						t.Fatalf("UI sync = %q", entry.UISync)
					}
					return
				}
			}
			t.Fatal("FLUX capability absent")
		})
	}
}

var animaInvocationSchemas = []string{"AnimaModelLoaderInvocation", "StringInvocation", "AnimaTextEncoderInvocation", "CollectInvocation", "IntegerInvocation", "AnimaDenoiseInvocation", "CoreMetadataInvocation", "AnimaLatentsToImageInvocation"}

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
	document["paths"].(map[string]any)["/api/v2/models/starter_models"] = map[string]any{"get": map[string]any{"responses": map[string]any{"200": map[string]any{"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/StarterModelResponse"}}}}}}}
	return document
}
