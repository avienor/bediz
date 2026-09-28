package cli_test

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

func TestInvokeAIRejectionIncludesValidationDetail(t *testing.T) {
	failure, _ := rejectedScan(t, http.StatusUnprocessableEntity, `{"detail":[{"loc":["query","scan_path"],"msg":"Input should be a valid string","type":"string_type"}]}`, "")
	want := map[string]any{
		"status": float64(422),
		"invokeai_detail": []any{map[string]any{
			"loc": []any{"query", "scan_path"}, "msg": "Input should be a valid string", "type": "string_type",
		}},
	}
	if !reflect.DeepEqual(failure.Details, want) {
		t.Fatalf("error details=%#v, want %#v", failure.Details, want)
	}
}

func TestInvokeAIRejectionSanitizesValidationDetail(t *testing.T) {
	const token = "connection-token-sentinel"
	failure, stdout := rejectedScan(t, http.StatusUnprocessableEntity, `{"detail":[{"loc":["body","batch","graph","nodes","denoise","width"],"msg":"Invalid\nvalue Bearer connection-token-sentinel\u001b","type":"value_error","input":{"access_token":"connection-token-sentinel","prompt":"private-input"},"ctx":{"error":"private-traceback"},"url":"https://example.org/private"}],"traceback":"private-traceback"}`, token)
	for _, private := range []string{token, "private-input", "private-traceback", "example.org/private"} {
		if strings.Contains(stdout, private) {
			t.Fatal("backend-only or secret data leaked")
		}
	}
	want := []any{map[string]any{
		"loc": []any{"body", "batch", "graph", "nodes", "denoise", "width"}, "msg": "Invalid value Bearer [REDACTED]", "type": "value_error",
	}}
	if !reflect.DeepEqual(failure.Details["invokeai_detail"], want) {
		t.Fatalf("validation detail=%#v, want %#v", failure.Details["invokeai_detail"], want)
	}
}

func TestInvokeAIRejectionIncludesStringDetail(t *testing.T) {
	failure, _ := rejectedScan(t, http.StatusBadRequest, `{"detail":"The search path could not be scanned"}`, "")
	if failure.Details["invokeai_detail"] != "The search path could not be scanned" {
		t.Fatalf("detail=%#v, want the backend's rejection reason", failure.Details["invokeai_detail"])
	}
}

func TestInvokeAIRejectionBoundsLongValidationDetail(t *testing.T) {
	location := []any{"body", "batch", "graph", "nodes", float64(0), strings.Repeat("long-location", 200), "width"}
	for range 20 {
		location = append(location, "nested")
	}
	var entries []any
	for range 20 {
		entries = append(entries, map[string]any{
			"loc": location, "msg": strings.Repeat("<é>", 4000), "type": strings.Repeat("value_error", 200),
			"input": strings.Repeat("private-input", 400),
		})
	}
	body, err := json.Marshal(map[string]any{"detail": entries})
	if err != nil {
		t.Fatal(err)
	}
	failure, stdout := rejectedScan(t, http.StatusUnprocessableEntity, string(body), "")
	detail, ok := failure.Details["invokeai_detail"].([]any)
	if !ok || len(detail) == 0 || len(detail) > 8 {
		t.Fatalf("validation detail has %d entries, want 1 through 8", len(detail))
	}
	var encoded struct {
		Error struct {
			Details map[string]jsontext.Value `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &encoded); err != nil {
		t.Fatal(err)
	}
	if size := len(encoded.Error.Details["invokeai_detail"]); size > 4096 {
		t.Fatalf("encoded detail length=%d, want at most 4096 bytes", size)
	}
	first := detail[0].(map[string]any)
	message := first["msg"].(string)
	if len(message) > 512 || !utf8.ValidString(message) || !strings.HasPrefix(message, "<é>") || !strings.HasSuffix(message, "…") || len(first["type"].(string)) > 128 {
		t.Fatalf("validation message or type exceeds its limit: %#v", first)
	}
	path := first["loc"].([]any)
	if len(path) > 16 || path[4] != float64(0) {
		t.Fatalf("validation location=%#v, want at most 16 segments with index 0 preserved", path)
	}
	for _, segment := range path {
		if text, ok := segment.(string); ok && (len(text) > 128 || !utf8.ValidString(text)) {
			t.Fatalf("location segment exceeds its limit: %q", text)
		}
	}
	if strings.Contains(stdout, "private-input") {
		t.Fatal("validation input leaked")
	}
}

func TestInvokeAIRejectionDetailAcrossMutations(t *testing.T) {
	for _, command := range mutatingCommands(t) {
		t.Run(command.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			server := command.server(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"detail":[{"loc":["body","seed"],"msg":"Invalid seed","type":"value_error","input":"private-input"}]}`))
			})
			t.Cleanup(server.Close)
			code, failure, stdout := runMutatingCommand(t, command, server.URL)
			if code != result.ExitInvokeAIFailure || failure.Code != result.CodeInvokeAIOperationFailed || failure.Details["status"] != float64(422) || strings.Contains(stdout, "private-input") {
				t.Fatalf("exit=%d stdout=%s", code, stdout)
			}
			_, hasDetail := failure.Details["invokeai_detail"]
			private := command.operation == result.OperationModelsInstall || command.operation == result.OperationAuthHFLogin || command.operation == result.OperationAuthHFLogout
			if hasDetail == private {
				t.Fatalf("private=%t, detail present=%t: %s", private, hasDetail, stdout)
			}
		})
	}
}

func TestInvokeAIRejectionDoesNotGuessMalformedLocation(t *testing.T) {
	failure, stdout := rejectedScan(t, http.StatusUnprocessableEntity, `{"detail":[{"loc":["body",{"token":"private-location"},-1,1.5,"width"],"msg":"Invalid width","type":"value_error"}]}`, "")
	want := []any{map[string]any{"msg": "Invalid width", "type": "value_error"}}
	if strings.Contains(stdout, "private-location") || !reflect.DeepEqual(failure.Details["invokeai_detail"], want) {
		t.Fatalf("validation detail=%#v, want the message without a guessed location", failure.Details["invokeai_detail"])
	}
}

func TestInvokeAIRejectionOmitsUnusableDetail(t *testing.T) {
	for _, body := range []string{
		"not JSON", `null`, `[]`, `{}`, `{"detail":null}`, `{"detail":true}`,
		`{"detail":{"token":"private-backend-data"}}`, `{"detail":[]}`,
		`{"detail":[{"msg":""},{"msg":{"token":"private-backend-data"}}]}`,
		`{"Detail":"private-backend-data"}`,
		`{"detail":"private-backend-data","detail":"ambiguous"}`,
		`{"detail":"private-backend-data"} {}`,
		`{"detail":"` + string([]byte{0xff}) + `"}`,
	} {
		t.Run(body, func(t *testing.T) {
			failure, stdout := rejectedScan(t, http.StatusBadRequest, body, "")
			if _, present := failure.Details["invokeai_detail"]; present || strings.Contains(stdout, "private-backend-data") {
				t.Fatalf("unusable detail surfaced: %s", stdout)
			}
		})
	}
}

func TestInvokeAIRejectionBoundsValidationEntryCount(t *testing.T) {
	var entries []any
	for range 20 {
		entries = append(entries, map[string]any{"loc": []any{"body", "seed"}, "msg": "Invalid seed", "type": "value_error"})
	}
	body, err := json.Marshal(map[string]any{"detail": entries})
	if err != nil {
		t.Fatal(err)
	}
	failure, _ := rejectedScan(t, http.StatusUnprocessableEntity, string(body), "")
	detail, ok := failure.Details["invokeai_detail"].([]any)
	if !ok || len(detail) != 8 {
		t.Fatalf("validation detail has %d entries, want the first 8", len(detail))
	}
}

func TestInvokeAIRejectionBoundsStringDetail(t *testing.T) {
	body, err := json.Marshal(map[string]any{"detail": strings.Repeat("é", 3000)})
	if err != nil {
		t.Fatal(err)
	}
	failure, _ := rejectedScan(t, http.StatusBadRequest, string(body), "")
	detail, ok := failure.Details["invokeai_detail"].(string)
	if !ok || len(detail) > 512 || !utf8.ValidString(detail) || !strings.HasPrefix(detail, "é") || !strings.HasSuffix(detail, "…") {
		t.Fatalf("string detail is not a bounded UTF-8 prefix: %q", detail)
	}
}

func TestInvokeAIRejectionOmitsServerErrorDetail(t *testing.T) {
	failure, _ := rejectedScan(t, http.StatusInternalServerError, `{"detail":"private-server-traceback"}`, "")
	if _, present := failure.Details["invokeai_detail"]; present {
		t.Fatal("server error detail surfaced")
	}
}

func rejectedScan(t *testing.T, status int, body, token string) (*result.Error, string) {
	t.Helper()
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/models/scan_folder" || r.URL.Query().Get("scan_path") != "/server/models" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		wantAuth := ""
		if token != "" {
			wantAuth = "Bearer " + token
		}
		if r.Header.Get("Authorization") != wantAuth {
			t.Error("unexpected backend authentication")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	var stdout, stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)
	code := app.Run(t.Context(), []string{"models", "scan", "--path", "/server/models", "--url", server.URL, "--token", token, "--json"})
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON value: %v", err)
	}
	if code != result.ExitInvokeAIFailure || stderr.Len() != 0 || envelope.SchemaVersion != 1 || envelope.OK || envelope.Operation != "models.scan" || envelope.Error == nil || envelope.Error.Code != result.CodeInvokeAIOperationFailed || envelope.Warnings == nil || len(envelope.Warnings) != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%q", code, stdout.String(), stderr.String())
	}
	if envelope.Error.Details["status"] != float64(status) {
		t.Fatalf("error details=%#v, want status %d", envelope.Error.Details, status)
	}
	return envelope.Error, stdout.String()
}
