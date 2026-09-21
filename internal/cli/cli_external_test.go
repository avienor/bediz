package cli_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("test write failure")
}

func writeTestPNG(t *testing.T, path string) []byte {
	t.Helper()
	content, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return content
}

func TestModelsListJSONReturnsSafeModelSummaries(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/models/" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{
			{
				"key": "model-z", "name": "Zeta", "base": "sdxl", "type": "main", "format": "diffusers",
				"file_size": 200, "description": "second", "path": "/server/secret/zeta", "hash": "secret-hash-z",
			},
			{
				"key": "model-a", "name": "Alpha", "base": "anima", "type": "main", "format": "checkpoint",
				"file_size": 100, "description": "first", "source": "https://example.invalid/?token=secret",
			},
		}})
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"models", "list", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	var envelope struct {
		SchemaVersion int    `json:"schema_version"`
		OK            bool   `json:"ok"`
		Operation     string `json:"operation"`
		Data          struct {
			Models []map[string]any `json:"models"`
		} `json:"data"`
		Warnings []result.Warning `json:"warnings"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	wantModels := []map[string]any{
		{"key": "model-a", "name": "Alpha", "base": "anima", "type": "main", "format": "checkpoint", "size_bytes": float64(100), "description": "first"},
		{"key": "model-z", "name": "Zeta", "base": "sdxl", "type": "main", "format": "diffusers", "size_bytes": float64(200), "description": "second"},
	}
	if envelope.SchemaVersion != 1 || !envelope.OK || envelope.Operation != "models.list" || !reflect.DeepEqual(envelope.Data.Models, wantModels) || len(envelope.Warnings) != 0 {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestModelsListAcceptsRequestDocument(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/models/" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{}})
	}))
	defer server.Close()

	requestPath := filepath.Join(t.TempDir(), "models-list.json")
	if err := os.WriteFile(requestPath, []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"models", "list", "--request", requestPath, "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if !envelope.OK || envelope.Operation != "models.list" || stderr.Len() != 0 {
		t.Fatalf("unexpected result: envelope=%#v stderr=%q", envelope, stderr.String())
	}
}

func TestModelsListAcceptsRequestDocumentFromStandardInput(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{}})
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(`{"schema_version":1}`), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"models", "list", "--request", "-", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
}

func TestModelsListRejectsUnsupportedRequestSchemaVersion(t *testing.T) {
	isolateUserConfigDir(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(`{"schema_version":2}`), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"models", "list", "--request", "-", "--url", "http://127.0.0.1:1", "--json"})

	if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestModelsListRejectsNonCanonicalRequestDocuments(t *testing.T) {
	isolateUserConfigDir(t)
	tests := []struct {
		name    string
		request string
	}{
		{name: "unknown field", request: `{"schema_version":1,"typo":true}`},
		{name: "trailing value", request: `{"schema_version":1}{"schema_version":1}`},
		{name: "malformed JSON", request: `{"schema_version":1`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app := cli.NewWithIO(strings.NewReader(test.request), &stdout, &stderr)

			exitCode := app.Run(t.Context(), []string{"models", "list", "--request", "-", "--url", "http://127.0.0.1:1", "--json"})

			if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 {
				t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
			}
			var envelope result.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
			}
			if envelope.Error == nil || envelope.Error.Code != "invalid_request" {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
		})
	}
}

func TestModelsListFlagsCompileToTypedFilters(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if !reflect.DeepEqual(query["base_models"], []string{"anima", "sdxl"}) ||
			query.Get("model_type") != "main" || query.Get("model_format") != "checkpoint" || query.Get("model_name") != "Exact Name" {
			t.Errorf("unexpected query: %v", query)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{}})
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{
		"models", "list", "--base", "anima", "--base", "sdxl", "--type", "main", "--format", "checkpoint", "--name", "Exact Name",
		"--url", server.URL, "--json",
	})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
}

func TestModelsListRejectsMixedRequestDocumentAndOperationFlags(t *testing.T) {
	isolateUserConfigDir(t)
	requestPath := filepath.Join(t.TempDir(), "models-list.json")
	if err := os.WriteFile(requestPath, []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"models", "list", "--request", requestPath, "--base", "anima", "--json"})

	if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.OK || envelope.Operation != "models.list" || envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestModelsListClassifiesConnectionFailure(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"models", "list", "--url", url, "--json"})

	if exitCode != result.ExitConnection || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "connection_failed" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestModelsListClassifiesAuthenticationFailure(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "denied", http.StatusUnauthorized)
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"models", "list", "--url", server.URL, "--token", "wrong", "--json"})

	if exitCode != result.ExitConnection || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "authentication_failed" || strings.Contains(stdout.String(), "wrong") {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestModelsListClassifiesInvalidInvokeAIResponse(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"models", "list", "--url", server.URL, "--json"})

	if exitCode != result.ExitInvokeAIFailure || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "invalid_invokeai_response" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestModelsListClassifiesLocalInterruption(t *testing.T) {
	isolateUserConfigDir(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(ctx, []string{"models", "list", "--url", "http://127.0.0.1:1", "--json"})

	if exitCode != result.ExitInterrupted || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "interrupted" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestImagesListJSONReturnsPaginatedImageReferences(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if r.URL.Path != "/api/v1/images/" || query.Get("offset") != "0" || query.Get("limit") != "20" ||
			query.Get("is_intermediate") != "false" || query.Get("order_dir") != "DESC" || query.Get("starred_first") != "false" {
			t.Errorf("unexpected request path=%q query=%v", r.URL.Path, query)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"offset": 0,
			"limit":  20,
			"total":  1,
			"items": []map[string]any{{
				"image_name": "image-1.png", "image_url": "api/v1/images/i/image-1.png/full", "thumbnail_url": "api/v1/images/i/image-1.png/thumbnail",
				"image_origin": "external", "image_category": "user", "width": 640, "height": 480,
				"created_at": "2026-09-20 10:00:00.000", "updated_at": "2026-09-20 10:01:00.000",
				"is_intermediate": false, "session_id": "session-1", "node_id": "node-1", "starred": true,
				"has_workflow": true, "board_id": "board-1", "deleted_at": nil, "image_subfolder": "private",
			}},
		})
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "list", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope struct {
		OK        bool   `json:"ok"`
		Operation string `json:"operation"`
		Data      struct {
			Offset int              `json:"offset"`
			Limit  int              `json:"limit"`
			Total  int              `json:"total"`
			Items  []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	wantItems := []map[string]any{{
		"image_name": "image-1.png", "image_url": server.URL + "/api/v1/images/i/image-1.png/full", "thumbnail_url": server.URL + "/api/v1/images/i/image-1.png/thumbnail",
		"image_origin": "external", "image_category": "user", "width": float64(640), "height": float64(480),
		"created_at": "2026-09-20 10:00:00.000", "updated_at": "2026-09-20 10:01:00.000", "is_intermediate": false,
		"session_id": "session-1", "node_id": "node-1", "starred": true, "has_workflow": true, "board_id": "board-1",
	}}
	if !envelope.OK || envelope.Operation != "images.list" || envelope.Data.Offset != 0 || envelope.Data.Limit != 20 || envelope.Data.Total != 1 || !reflect.DeepEqual(envelope.Data.Items, wantItems) {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestImagesListFlagsCompileToTypedRequest(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("offset") != "10" || query.Get("limit") != "5" || query.Get("board_id") != "none" {
			t.Errorf("unexpected query: %v", query)
		}
		if _, exists := query["is_intermediate"]; exists {
			t.Errorf("is_intermediate should be omitted when intermediates are included: %v", query)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"offset": 10, "limit": 5, "total": 0, "items": []any{}})
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{
		"images", "list", "--offset", "10", "--limit", "5", "--board", "none", "--include-intermediate",
		"--url", server.URL, "--json",
	})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
}

func TestImagesListAcceptsTypedRequestDocument(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("offset") != "4" || query.Get("limit") != "2" || query.Get("board_id") != "none" {
			t.Errorf("unexpected query: %v", query)
		}
		if _, exists := query["is_intermediate"]; exists {
			t.Errorf("unexpected intermediate filter: %v", query)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"offset": 4, "limit": 2, "total": 0, "items": []any{}})
	}))
	defer server.Close()
	request := `{"schema_version":1,"offset":4,"limit":2,"board_id":"none","include_intermediate":true}`
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(request), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "list", "--request", "-", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
}

func TestImagesListRejectsOutOfRangeLimitAsInvalidRequest(t *testing.T) {
	isolateUserConfigDir(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "list", "--limit", "101", "--url", "http://127.0.0.1:1", "--json"})

	if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestImagesGetJSONReturnsExactImageReference(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/images/i/image-1.png" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"image_name": "image-1.png", "image_url": "api/v1/images/i/image-1.png/full", "thumbnail_url": "api/v1/images/i/image-1.png/thumbnail",
			"image_origin": "internal", "image_category": "general", "width": 1024, "height": 768,
			"created_at": "created", "updated_at": "updated", "is_intermediate": false, "starred": false, "has_workflow": false,
		})
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "get", "image-1.png", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope struct {
		OK        bool   `json:"ok"`
		Operation string `json:"operation"`
		Data      struct {
			Image map[string]any `json:"image"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if !envelope.OK || envelope.Operation != "images.get" || envelope.Data.Image["image_name"] != "image-1.png" ||
		envelope.Data.Image["image_url"] != server.URL+"/api/v1/images/i/image-1.png/full" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestImagesGetAcceptsTypedRequestDocument(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/images/i/from-request.png" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"image_name": "from-request.png", "image_url": "api/v1/images/i/from-request.png/full", "thumbnail_url": "api/v1/images/i/from-request.png/thumbnail",
			"image_origin": "internal", "image_category": "general", "width": 1, "height": 1,
			"created_at": "created", "updated_at": "updated", "is_intermediate": false, "starred": false, "has_workflow": false,
		})
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(`{"schema_version":1,"image_name":"from-request.png"}`), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "get", "--request", "-", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
}

func TestImagesGetRejectsRequestWithoutImageName(t *testing.T) {
	isolateUserConfigDir(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(`{"schema_version":1}`), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "get", "--request", "-", "--url", "http://127.0.0.1:1", "--json"})

	if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestImagesGetClassifiesMissingImage(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "get", "missing.png", "--url", server.URL, "--json"})

	if exitCode != result.ExitInvokeAIFailure || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "not_found" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestQueueListJSONReturnsOnlyRequestedSummaryPage(t *testing.T) {
	isolateUserConfigDir(t)
	var summaries atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/queue/default/item_ids":
			if r.Method != http.MethodGet || r.URL.Query().Get("order_dir") != "DESC" {
				t.Errorf("unexpected item id request: %s %s", r.Method, r.URL.String())
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"item_ids": []int{9, 8, 7}, "total_count": 3})
		case "/api/v1/queue/default/item_summaries_by_ids":
			summaries.Add(1)
			if r.Method != http.MethodPost {
				t.Errorf("summary method = %s, want POST", r.Method)
			}
			var body struct {
				ItemIDs []int `json:"item_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode summary request: %v", err)
				return
			}
			if !reflect.DeepEqual(body.ItemIDs, []int{8}) {
				t.Errorf("hydrated item ids = %v, want [8]", body.ItemIDs)
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"item_id": 8, "status": "completed", "batch_id": "batch-8", "origin": "generate", "destination": "generate",
				"created_at": "created", "started_at": "started", "completed_at": "completed", "device": "cuda:0",
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "list", "--offset", "1", "--limit", "1", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 || summaries.Load() != 1 {
		t.Fatalf("exit code = %d, summary requests = %d, stderr = %q, stdout = %q", exitCode, summaries.Load(), stderr.String(), stdout.String())
	}
	var envelope struct {
		OK        bool   `json:"ok"`
		Operation string `json:"operation"`
		Data      struct {
			Offset int              `json:"offset"`
			Limit  int              `json:"limit"`
			Total  int              `json:"total"`
			Items  []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if !envelope.OK || envelope.Operation != "queue.list" || envelope.Data.Offset != 1 || envelope.Data.Limit != 1 || envelope.Data.Total != 3 ||
		len(envelope.Data.Items) != 1 || envelope.Data.Items[0]["item_id"] != float64(8) || envelope.Data.Items[0]["queue_id"] != "default" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestQueueListPreservesNewestFirstItemIDOrder(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/queue/default/item_ids":
			_ = json.NewEncoder(w).Encode(map[string]any{"item_ids": []int{9, 8}, "total_count": 2})
		case "/api/v1/queue/default/item_summaries_by_ids":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"item_id": 8, "status": "pending", "batch_id": "batch-8", "created_at": "older"},
				{"item_id": 9, "status": "completed", "batch_id": "batch-9", "created_at": "newer"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "list", "--limit", "2", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope struct {
		Data struct {
			Items []struct {
				ItemID int `json:"item_id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if len(envelope.Data.Items) != 2 || envelope.Data.Items[0].ItemID != 9 || envelope.Data.Items[1].ItemID != 8 {
		t.Fatalf("queue items are not newest first: %#v", envelope.Data.Items)
	}
}

func TestQueueListKeepsFinalPageWithinAvailableItemIDs(t *testing.T) {
	isolateUserConfigDir(t)
	var summaries atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/queue/default/item_ids":
			_ = json.NewEncoder(w).Encode(map[string]any{"item_ids": []int{9, 8, 7}, "total_count": 3})
		case "/api/v1/queue/default/item_summaries_by_ids":
			summaries.Add(1)
			var body struct {
				ItemIDs []int `json:"item_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode summary request: %v", err)
				return
			}
			if !reflect.DeepEqual(body.ItemIDs, []int{7}) {
				t.Errorf("hydrated item ids = %v, want [7]", body.ItemIDs)
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"item_id": 7, "status": "completed", "batch_id": "batch-7", "created_at": "oldest"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "list", "--offset", "2", "--limit", "5", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 || summaries.Load() != 1 {
		t.Fatalf("exit code = %d, summary requests = %d, stderr = %q, stdout = %q", exitCode, summaries.Load(), stderr.String(), stdout.String())
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Offset int              `json:"offset"`
			Limit  int              `json:"limit"`
			Total  int              `json:"total"`
			Items  []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if !envelope.OK || envelope.Data.Offset != 2 || envelope.Data.Limit != 5 || envelope.Data.Total != 3 ||
		len(envelope.Data.Items) != 1 || envelope.Data.Items[0]["item_id"] != float64(7) {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestQueueListSkipsHydrationWhenOffsetIsPastAvailableItemIDs(t *testing.T) {
	for _, test := range []struct {
		name   string
		offset int
	}{
		{name: "offset at available item count", offset: 3},
		{name: "offset beyond available item count", offset: 10},
	} {
		t.Run(test.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			var summaries atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/queue/default/item_ids":
					_ = json.NewEncoder(w).Encode(map[string]any{"item_ids": []int{9, 8, 7}, "total_count": 3})
				case "/api/v1/queue/default/item_summaries_by_ids":
					summaries.Add(1)
					http.Error(w, "summaries are outside the requested page", http.StatusInternalServerError)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app := cli.New(&stdout, &stderr)

			exitCode := app.Run(t.Context(), []string{"queue", "list", "--offset", strconv.Itoa(test.offset), "--limit", "2", "--url", server.URL, "--json"})

			if exitCode != result.ExitSuccess || stderr.Len() != 0 || summaries.Load() != 0 {
				t.Fatalf("exit code = %d, summary requests = %d, stderr = %q, stdout = %q", exitCode, summaries.Load(), stderr.String(), stdout.String())
			}
			var envelope struct {
				OK   bool `json:"ok"`
				Data struct {
					Offset int              `json:"offset"`
					Limit  int              `json:"limit"`
					Total  int              `json:"total"`
					Items  []map[string]any `json:"items"`
				} `json:"data"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
			}
			if !envelope.OK || envelope.Data.Offset != test.offset || envelope.Data.Limit != 2 || envelope.Data.Total != 3 || len(envelope.Data.Items) != 0 {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
		})
	}
}

func TestQueueListAcceptsTypedRequestDocument(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/queue/custom/item_ids" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"item_ids": []int{}, "total_count": 0})
	}))
	defer server.Close()
	request := `{"schema_version":1,"queue_id":"custom","offset":3,"limit":4}`
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(request), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "list", "--request", "-", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
}

func TestQueueListRejectsOutOfRangeLimitAsInvalidRequest(t *testing.T) {
	isolateUserConfigDir(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "list", "--limit", "0", "--url", "http://127.0.0.1:1", "--json"})

	if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestQueueGetJSONReturnsNormalizedItemAndOutputImages(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/queue/default/i/8":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"item_id": 8, "queue_id": "default", "batch_id": "batch-8", "session_id": "session-8", "status": "completed", "priority": 0,
				"origin": "generate", "destination": "generate", "created_at": "created", "updated_at": "updated", "started_at": "started", "completed_at": "completed",
				"error_type": nil, "error_message": nil, "error_traceback": "/server/private/traceback",
				"session": map[string]any{"results": map[string]any{
					"node-1": map[string]any{"type": "image_output", "image": map[string]any{"image_name": "output.png"}, "width": 512, "height": 512},
					"node-2": map[string]any{"type": "integer_output", "value": 42},
				}},
			})
		case "/api/v1/images/i/output.png":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"image_name": "output.png", "image_url": "api/v1/images/i/output.png/full", "thumbnail_url": "api/v1/images/i/output.png/thumbnail",
				"image_origin": "internal", "image_category": "general", "width": 512, "height": 512,
				"created_at": "created", "updated_at": "updated", "is_intermediate": false, "starred": false, "has_workflow": true,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "get", "8", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope struct {
		OK        bool   `json:"ok"`
		Operation string `json:"operation"`
		Data      struct {
			Item map[string]any `json:"item"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	images, ok := envelope.Data.Item["images"].([]any)
	if !envelope.OK || envelope.Operation != "queue.get" || envelope.Data.Item["item_id"] != float64(8) || !ok || len(images) != 1 {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
	if strings.Contains(stdout.String(), "traceback") || strings.Contains(stdout.String(), "/server/private") {
		t.Fatalf("server traceback leaked: %s", stdout.String())
	}
}

func TestQueueGetSucceedsWhenHistoricalOutputImageIsMissing(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/queue/default/i/8":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"item_id": 8, "queue_id": "default", "batch_id": "batch-8", "session_id": "session-8", "status": "completed", "priority": 0,
				"created_at": "created", "updated_at": "updated",
				"session": map[string]any{"results": map[string]any{
					"node-1": map[string]any{"type": "image_output", "image": map[string]any{"image_name": "deleted.png"}},
				}},
			})
		case "/api/v1/images/i/deleted.png":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "get", "8", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope struct {
		Data struct {
			Item struct {
				Images []any `json:"images"`
			} `json:"item"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if len(envelope.Data.Item.Images) != 0 {
		t.Fatalf("missing historical output must be omitted, got %#v", envelope.Data.Item.Images)
	}
}

func TestQueueGetCollectsUniqueOutputImageNamesInLexicographicOrder(t *testing.T) {
	isolateUserConfigDir(t)
	var requests struct {
		sync.Mutex
		names []string
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/queue/default/i/8":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"item_id": 8, "queue_id": "default", "batch_id": "batch-8", "session_id": "session-8", "status": "completed", "priority": 0,
				"created_at": "created", "updated_at": "updated",
				"session": map[string]any{"results": map[string]any{
					"node-1": map[string]any{"type": "image_output", "image": map[string]any{"image_name": "zulu.png"}},
					"node-2": map[string]any{"type": "image_output", "image": map[string]any{"image_name": "alpha.png"}},
					"node-3": map[string]any{"type": "image_output", "image": map[string]any{"image_name": "alpha.png"}},
					"node-4": map[string]any{"type": "integer_output", "value": 42},
				}},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/images/i/"):
			name := strings.TrimPrefix(r.URL.Path, "/api/v1/images/i/")
			requests.Lock()
			requests.names = append(requests.names, name)
			requests.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"image_name": name, "image_url": "api/v1/images/i/" + name + "/full", "thumbnail_url": "api/v1/images/i/" + name + "/thumbnail",
				"image_origin": "internal", "image_category": "general", "width": 512, "height": 512,
				"created_at": "created", "updated_at": "updated", "is_intermediate": false, "starred": false, "has_workflow": true,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "get", "8", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	requests.Lock()
	requested := requests.names
	requests.Unlock()
	if !reflect.DeepEqual(requested, []string{"alpha.png", "zulu.png"}) {
		t.Fatalf("output image requests = %v, want [alpha.png zulu.png]", requested)
	}
	var envelope struct {
		Data struct {
			Item struct {
				Images []struct {
					ImageName string `json:"image_name"`
				} `json:"images"`
			} `json:"item"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	var names []string
	for _, image := range envelope.Data.Item.Images {
		names = append(names, image.ImageName)
	}
	if !reflect.DeepEqual(names, []string{"alpha.png", "zulu.png"}) {
		t.Fatalf("output images = %v, want [alpha.png zulu.png]", names)
	}
}

// Output image failures reach the CLI wrapped by the queue hydration context
// message, so classification must match through the error chain.
func TestQueueGetClassifiesWrappedOutputImageFailures(t *testing.T) {
	tests := []struct {
		name         string
		imageHandler func(t *testing.T, w http.ResponseWriter)
		exitCode     int
		errorCode    string
	}{
		{
			name: "authentication failure",
			imageHandler: func(_ *testing.T, w http.ResponseWriter) {
				http.Error(w, "denied", http.StatusUnauthorized)
			},
			exitCode:  result.ExitConnection,
			errorCode: "authentication_failed",
		},
		{
			name: "connection failure",
			imageHandler: func(t *testing.T, w http.ResponseWriter) {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Errorf("hijack output image connection: %v", err)
					return
				}
				_ = conn.Close()
			},
			exitCode:  result.ExitConnection,
			errorCode: "connection_failed",
		},
		{
			name: "invalid response",
			imageHandler: func(_ *testing.T, w http.ResponseWriter) {
				_, _ = w.Write([]byte("not-json"))
			},
			exitCode:  result.ExitInvokeAIFailure,
			errorCode: "invalid_invokeai_response",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/queue/default/i/8":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"item_id": 8, "queue_id": "default", "batch_id": "batch-8", "session_id": "session-8", "status": "completed", "priority": 0,
						"created_at": "created", "updated_at": "updated",
						"session": map[string]any{"results": map[string]any{
							"node-1": map[string]any{"type": "image_output", "image": map[string]any{"image_name": "output.png"}},
						}},
					})
				case "/api/v1/images/i/output.png":
					test.imageHandler(t, w)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app := cli.New(&stdout, &stderr)

			exitCode := app.Run(t.Context(), []string{"queue", "get", "8", "--url", server.URL, "--json"})

			if exitCode != test.exitCode || stderr.Len() != 0 {
				t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
			}
			var envelope result.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
			}
			if envelope.OK || envelope.Operation != "queue.get" || envelope.Error == nil || envelope.Error.Code != test.errorCode {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
		})
	}
}

func TestQueueGetDoesNotExposeInvokeAIErrorBodies(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error_traceback":"/server/private/traceback"}`, http.StatusInternalServerError)
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "get", "8", "--url", server.URL, "--json"})

	if exitCode != result.ExitInvokeAIFailure || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if strings.Contains(stdout.String(), "traceback") || strings.Contains(stdout.String(), "/server/private") {
		t.Fatalf("InvokeAI error body leaked: %s", stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "invokeai_operation_failed" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestQueueGetAcceptsTypedRequestDocument(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/queue/custom/i/5" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"item_id": 5, "queue_id": "custom", "batch_id": "batch-5", "session_id": "session-5", "status": "pending", "priority": 0,
			"created_at": "created", "updated_at": "updated", "session": map[string]any{"results": map[string]any{}},
		})
	}))
	defer server.Close()
	request := `{"schema_version":1,"queue_id":"custom","item_id":5}`
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(request), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "get", "--request", "-", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
}

func TestQueueGetRejectsRequestWithoutPositiveItemID(t *testing.T) {
	isolateUserConfigDir(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(`{"schema_version":1,"queue_id":"default"}`), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "get", "--request", "-", "--url", "http://127.0.0.1:1", "--json"})

	if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestImagesUploadJSONUploadsOneValidatedLocalFile(t *testing.T) {
	isolateUserConfigDir(t)
	imagePath := filepath.Join(t.TempDir(), "source.png")
	imageContent := writeTestPNG(t, imagePath)
	var uploads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "6.14.1"})
		case "/api/v1/images/upload":
			uploads.Add(1)
			query := r.URL.Query()
			if r.Method != http.MethodPost || query.Get("image_category") != "user" || query.Get("is_intermediate") != "false" {
				t.Errorf("unexpected upload request: %s %s", r.Method, r.URL.String())
			}
			file, header, err := r.FormFile("file")
			if err != nil {
				t.Errorf("read uploaded form file: %v", err)
				return
			}
			defer func() { _ = file.Close() }()
			body, err := io.ReadAll(file)
			if err != nil {
				t.Errorf("read uploaded image: %v", err)
				return
			}
			if header.Filename != "source.png" || !bytes.Equal(body, imageContent) {
				t.Errorf("uploaded file name=%q body=%q", header.Filename, body)
			}
			if header.Header.Get("Content-Type") != "image/png" {
				t.Errorf("uploaded file content type = %q, want image/png", header.Header.Get("Content-Type"))
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"image_name": "uploaded.png", "image_url": "api/v1/images/i/uploaded.png/full", "thumbnail_url": "api/v1/images/i/uploaded.png/thumbnail",
				"image_origin": "external", "image_category": "user", "width": 320, "height": 240,
				"created_at": "created", "updated_at": "updated", "is_intermediate": false, "starred": false, "has_workflow": false,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "upload", imagePath, "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 || uploads.Load() != 1 {
		t.Fatalf("exit code = %d, uploads = %d, stderr = %q, stdout = %q", exitCode, uploads.Load(), stderr.String(), stdout.String())
	}
	var envelope struct {
		OK        bool   `json:"ok"`
		Operation string `json:"operation"`
		Data      struct {
			Image map[string]any `json:"image"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if !envelope.OK || envelope.Operation != "images.upload" || envelope.Data.Image["image_name"] != "uploaded.png" ||
		envelope.Data.Image["image_url"] != server.URL+"/api/v1/images/i/uploaded.png/full" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestImagesUploadAcceptsTypedRequestDocument(t *testing.T) {
	isolateUserConfigDir(t)
	imagePath := filepath.Join(t.TempDir(), "source.png")
	writeTestPNG(t, imagePath)
	request, err := json.Marshal(map[string]any{"schema_version": 1, "path": imagePath})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "6.14.1"})
		case "/api/v1/images/upload":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"image_name": "uploaded.png", "image_url": "api/v1/images/i/uploaded.png/full", "thumbnail_url": "api/v1/images/i/uploaded.png/thumbnail",
				"image_origin": "external", "image_category": "user", "width": 1, "height": 1,
				"created_at": "created", "updated_at": "updated", "is_intermediate": false, "starred": false, "has_workflow": false,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewWithIO(bytes.NewReader(request), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "upload", "--request", "-", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
}

func TestImagesUploadRejectsUnsupportedRequestSchemaVersion(t *testing.T) {
	isolateUserConfigDir(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(`{"schema_version":2,"path":"/tmp/source.png"}`), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "upload", "--request", "-", "--url", "http://127.0.0.1:1", "--json"})

	if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestImagesUploadLostResponseReturnsUnknownOutcomeWithoutRetry(t *testing.T) {
	isolateUserConfigDir(t)
	imagePath := filepath.Join(t.TempDir(), "source.png")
	writeTestPNG(t, imagePath)
	var uploads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "6.14.1"})
		case "/api/v1/images/upload":
			uploads.Add(1)
			_, _ = io.Copy(io.Discard, r.Body)
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack upload connection: %v", err)
				return
			}
			_ = connection.Close()
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "upload", imagePath, "--url", server.URL, "--json"})

	if exitCode != result.ExitInvokeAIFailure || stderr.Len() != 0 || uploads.Load() != 1 {
		t.Fatalf("exit code = %d, uploads = %d, stderr = %q, stdout = %q", exitCode, uploads.Load(), stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.OK || envelope.Operation != "images.upload" || envelope.Error == nil || envelope.Error.Code != "outcome_unknown" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestImagesUploadRejectsRelativePathBeforeConnecting(t *testing.T) {
	isolateUserConfigDir(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "upload", "relative.png", "--url", "http://127.0.0.1:1", "--json"})

	if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.OK || envelope.Operation != "images.upload" || envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestImagesUploadRejectsNonRegularPathBeforeConnecting(t *testing.T) {
	isolateUserConfigDir(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "upload", t.TempDir(), "--url", "http://127.0.0.1:1", "--json"})

	if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

// /proc/self/mem names a regular file whose reads fail with EIO at offset zero,
// so the only reachable invalid request is the non-EOF upload content read
// failure; a swallowed read error would instead reach the connection attempt.
func TestImagesUploadRejectsUnreadableFileContentBeforeConnecting(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the Linux proc filesystem provides a regular file whose reads fail")
	}
	isolateUserConfigDir(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "upload", "/proc/self/mem", "--url", "http://127.0.0.1:1", "--json"})

	if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.OK || envelope.Operation != "images.upload" || envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestImagesUploadRejectsUnsupportedInvokeAIVersionBeforeMutation(t *testing.T) {
	isolateUserConfigDir(t)
	imagePath := filepath.Join(t.TempDir(), "source.png")
	writeTestPNG(t, imagePath)
	var uploads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/app/version" {
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "6.15.0"})
			return
		}
		if r.URL.Path == "/api/v1/images/upload" {
			uploads.Add(1)
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "upload", imagePath, "--url", server.URL, "--json"})

	if exitCode != result.ExitUnsupportedCapability || uploads.Load() != 0 || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, uploads = %d, stderr = %q, stdout = %q", exitCode, uploads.Load(), stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "unsupported_capability" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestConfigSetJSONNeverPrintsToken(t *testing.T) {
	isolateUserConfigDir(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"config", "set", "--token", "top-secret", "--json"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	if strings.Contains(stdout.String(), "top-secret") {
		t.Fatalf("token leaked to stdout: %s", stdout.String())
	}
	var setEnvelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &setEnvelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v", err)
	}
	if !setEnvelope.OK || setEnvelope.Operation != "config.set" {
		t.Fatalf("unexpected set envelope: %#v", setEnvelope)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = app.Run(t.Context(), []string{"config", "get", "--json"})
	if exitCode != result.ExitSuccess {
		t.Fatalf("get exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	var getEnvelope struct {
		OK        bool   `json:"ok"`
		Operation string `json:"operation"`
		Data      struct {
			Stored struct {
				TokenConfigured bool `json:"token_configured"`
			} `json:"stored"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &getEnvelope); err != nil {
		t.Fatalf("config get stdout is not one JSON object: %v", err)
	}
	if !getEnvelope.OK || getEnvelope.Operation != "config.get" || !getEnvelope.Data.Stored.TokenConfigured {
		t.Fatalf("unexpected get envelope: %#v", getEnvelope)
	}
}

func TestConfigGetFailsWhenHumanOutputCannotBeWritten(t *testing.T) {
	isolateUserConfigDir(t)
	var stderr bytes.Buffer
	app := cli.New(errorWriter{}, &stderr)

	exitCode := app.Run(t.Context(), []string{"config", "get"})

	if exitCode != result.ExitInvokeAIFailure {
		t.Fatalf("exit code = %d, want %d", exitCode, result.ExitInvokeAIFailure)
	}
	if !strings.Contains(stderr.String(), "output_write_failed") {
		t.Fatalf("stderr = %q, want output_write_failed", stderr.String())
	}
}

func TestConfigGetHumanOutputShowsUnsetStoredValues(t *testing.T) {
	isolateUserConfigDir(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"config", "get"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	want := "Configuration: " + filepath.Join(userConfigDir, "bediz", "config.json") + "\n" +
		"Stored URL: not set\n" +
		"Stored token: not configured\n" +
		"Effective URL: http://127.0.0.1:9090 (default)\n" +
		"Effective token: not configured\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func isolateUserConfigDir(t *testing.T) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "config")
	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", dir)
	case "darwin":
		t.Setenv("HOME", dir)
	default:
		t.Setenv("XDG_CONFIG_HOME", dir)
	}
}

func TestInvalidCommandUsesJSONEnvelope(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"unknown", "--json"})

	if exitCode != result.ExitInvalidRequest {
		t.Fatalf("exit code = %d, want %d", exitCode, result.ExitInvalidRequest)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v", err)
	}
	if envelope.OK || envelope.Operation != "cli" || envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestGlobalJSONFlagBeforeVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"--json", "version"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if !envelope.OK || envelope.Operation != "version" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestConfigHelpListsNestedCommands(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"config", "--help"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, command := range []string{"get", "set"} {
		if !bytes.Contains(stdout.Bytes(), []byte(command)) {
			t.Fatalf("help does not list %q command; stdout = %q", command, stdout.String())
		}
	}
}

func TestConfigGetHelpIsGeneratedByCommandTree(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"config", "get", "--help"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("--json")) {
		t.Fatalf("help does not list inherited --json flag; stdout = %q", stdout.String())
	}
}

func TestConfigSetHelpListsConfigurationFlags(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"config", "set", "--help"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, flag := range []string{"--url", "--token", "--unset-url", "--unset-token"} {
		if !bytes.Contains(stdout.Bytes(), []byte(flag)) {
			t.Fatalf("help does not list %q; stdout = %q", flag, stdout.String())
		}
	}
}

func TestDoctorHelpListsConnectionFlags(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"doctor", "--help"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, flag := range []string{"--url", "--token", "--timeout", "--json"} {
		if !bytes.Contains(stdout.Bytes(), []byte(flag)) {
			t.Fatalf("help does not list %q; stdout = %q", flag, stdout.String())
		}
	}
}

func TestDoctorHelpInJSONModeUsesFailureEnvelope(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "JSON flag after help flag", args: []string{"doctor", "--help", "--json"}},
		{name: "JSON flag before command", args: []string{"--json", "doctor", "--help"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app := cli.New(&stdout, &stderr)

			exitCode := app.Run(t.Context(), test.args)

			if exitCode != result.ExitInvalidRequest {
				t.Fatalf("exit code = %d, want %d", exitCode, result.ExitInvalidRequest)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
			var envelope result.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
			}
			if envelope.OK || envelope.Operation != "doctor" || envelope.Error == nil || envelope.Error.Code != "invalid_request" {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
		})
	}
}

func TestDoctorFlagErrorKeepsOperationInJSONEnvelope(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"doctor", "--unknown", "--json"})

	if exitCode != result.ExitInvalidRequest {
		t.Fatalf("exit code = %d, want %d", exitCode, result.ExitInvalidRequest)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.OK || envelope.Operation != "doctor" || envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}
