package cli_test

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

func TestDoctorUnreachableOmitsUnknownOpenAPIChecks(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()

	report := runDoctorJSONFailure(t, server.URL, result.ExitConnection, result.CodeConnectionFailed)
	want := map[string]jsontext.Value{"available": jsontext.Value("false")}
	if !reflect.DeepEqual(report.OpenAPI, want) {
		t.Fatalf("OpenAPI report has %d fields, available = %s; want only available: false", len(report.OpenAPI), report.OpenAPI["available"])
	}
	for _, issue := range report.Issues {
		if issue.Code == "missing_endpoint" || issue.Code == "incompatible_invocation" {
			t.Fatalf("unread OpenAPI reported a schema incompatibility: %#v", issue)
		}
	}
}

func TestDoctorUnavailableOpenAPIOmitsUnknownChecks(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		exit   int
		code   string
	}{
		{"authentication rejected", http.StatusUnauthorized, `{"detail":"unauthorized"}`, result.ExitConnection, result.CodeAuthenticationFailed},
		{"not found", http.StatusNotFound, `{"detail":"not found"}`, result.ExitInvokeAIFailure, result.CodeInvokeAIHTTPError},
		{"server failure", http.StatusInternalServerError, `{"detail":"unavailable"}`, result.ExitInvokeAIFailure, result.CodeInvokeAIHTTPError},
		{"malformed JSON", http.StatusOK, `{"paths":`, result.ExitInvokeAIFailure, result.CodeInvalidInvokeAIResponse},
		{"partially decoded document", http.StatusOK, `{"paths":{"/api/v2/models/":{"get":{}}},"components":{"schemas":[]}}`, result.ExitInvokeAIFailure, result.CodeInvalidInvokeAIResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v1/app/version":
					_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
				case "/openapi.json":
					w.WriteHeader(test.status)
					_, _ = w.Write([]byte(test.body))
				case "/api/v2/models/":
					_, _ = w.Write([]byte(`{"models":[]}`))
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)

			report := runDoctorJSONFailure(t, server.URL, test.exit, test.code)
			want := map[string]jsontext.Value{"available": jsontext.Value("false")}
			if !reflect.DeepEqual(report.OpenAPI, want) {
				t.Fatalf("OpenAPI report has %d fields, available = %s; want only available: false", len(report.OpenAPI), report.OpenAPI["available"])
			}
			for _, issue := range report.Issues {
				if issue.Code == "missing_endpoint" || issue.Code == "incompatible_invocation" {
					t.Fatalf("unread OpenAPI reported a schema incompatibility: %#v", issue)
				}
			}
		})
	}
}

func TestDoctorIncompatibleOpenAPIRetainsChecks(t *testing.T) {
	document := sdxlOpenAPIFixture(t)
	delete(document["paths"].(map[string]any), "/api/v1/images/upload")
	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	delete(schemas["AnimaDenoiseInvocation"].(map[string]any)["properties"].(map[string]any), "steps")
	body, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write(body)
		case "/api/v2/models/":
			_, _ = w.Write([]byte(`{"models":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	report := runDoctorJSONFailure(t, server.URL, result.ExitUnsupportedCapability, result.CodeUnsupportedCapability)
	if string(report.OpenAPI["available"]) != "true" {
		t.Fatal("loaded OpenAPI was reported as unavailable")
	}
	var endpoints []struct {
		Method    string `json:"method"`
		Path      string `json:"path"`
		Available bool   `json:"available"`
	}
	if err := json.Unmarshal(report.OpenAPI["required_endpoints"], &endpoints); err != nil {
		t.Fatalf("missing endpoint checks: %v", err)
	}
	missingUpload := false
	for _, endpoint := range endpoints {
		if endpoint.Method == "POST" && endpoint.Path == "/api/v1/images/upload" {
			missingUpload = !endpoint.Available
		}
	}
	if !missingUpload {
		t.Fatal("loaded OpenAPI did not report the missing upload endpoint")
	}
	var invocations []struct {
		Schema            string   `json:"schema"`
		Available         bool     `json:"available"`
		MissingProperties []string `json:"missing_properties"`
	}
	if err := json.Unmarshal(report.OpenAPI["required_invocations"], &invocations); err != nil {
		t.Fatalf("missing invocation checks: %v", err)
	}
	missingSteps := false
	for _, invocation := range invocations {
		if invocation.Schema == "AnimaDenoiseInvocation" {
			missingSteps = invocation.Available && slices.Equal(invocation.MissingProperties, []string{"steps"})
		}
	}
	if !missingSteps {
		t.Fatal("loaded OpenAPI did not retain the missing steps property")
	}
	for _, issue := range report.Issues {
		if issue.Code == "incompatible_invocation" && issue.Details.Schema == "AnimaDenoiseInvocation" && slices.Equal(issue.Details.MissingProperties, []string{"steps"}) {
			return
		}
	}
	t.Fatal("loaded OpenAPI did not retain the invocation incompatibility issue")
}

type doctorFailureReport struct {
	OpenAPI map[string]jsontext.Value `json:"openapi"`
	Issues  []struct {
		Code    string `json:"code"`
		Details struct {
			Schema            string   `json:"schema"`
			MissingProperties []string `json:"missing_properties"`
		} `json:"details"`
	} `json:"issues"`
}

func runDoctorJSONFailure(t *testing.T, target string, wantExit int, wantCode string) doctorFailureReport {
	t.Helper()
	isolateUserConfigDir(t)
	var stdout, stderr bytes.Buffer
	code := cli.New(&stdout, &stderr).Run(t.Context(), []string{"doctor", "--url", target, "--json"})
	if code != wantExit || stderr.Len() != 0 {
		t.Fatalf("exit = %d, want %d; stderr = %q; stdout = %s", code, wantExit, stderr.String(), stdout.String())
	}
	var envelope struct {
		SchemaVersion int    `json:"schema_version"`
		OK            bool   `json:"ok"`
		Operation     string `json:"operation"`
		Error         *struct {
			Code    string `json:"code"`
			Details struct {
				Report doctorFailureReport `json:"report"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %s", err, stdout.String())
	}
	if envelope.SchemaVersion != 1 || envelope.OK || envelope.Operation != "doctor" || envelope.Error == nil || envelope.Error.Code != wantCode {
		t.Fatalf("unexpected doctor failure envelope: %s", stdout.String())
	}
	return envelope.Error.Details.Report
}
