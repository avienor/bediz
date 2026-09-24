package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

const deleteImageName = "one-image.png"

type imageDeleteServer struct {
	*httptest.Server
	requests atomic.Int32
	deletes  atomic.Int32
}

func newImageDeleteServer(t *testing.T, version string, deleteImage http.HandlerFunc) *imageDeleteServer {
	t.Helper()
	server := &imageDeleteServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.requests.Add(1)
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/app/version" {
			_ = json.NewEncoder(w).Encode(map[string]string{"version": version})
			return
		}
		if r.Method == http.MethodDelete && r.URL.EscapedPath() == "/api/v1/images/i/"+deleteImageName {
			server.deletes.Add(1)
			deleteImage(w, r)
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL)
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func deletedImage(w http.ResponseWriter, _ *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"deleted_images": []string{deleteImageName}, "failed_images": []string{}, "affected_boards": []string{"none"},
	})
}

func TestImagesDeleteRequiresYesBeforeAnyNetworkRequest(t *testing.T) {
	isolateUserConfigDir(t)
	server := newImageDeleteServer(t, "6.14.1", deletedImage)

	exitCode, envelope, stderr := runBoardsJSON(t, "", "images", "delete", deleteImageName, "--url", server.URL)

	if exitCode != result.ExitInvalidRequest || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	if code, _ := errorDetails(t, envelope); envelope["operation"] != "images.delete" || code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
	if requests := server.requests.Load(); requests != 0 {
		t.Fatalf("sent %d requests, want none", requests)
	}
}

func TestImagesDeleteReportsTheExactDeletedName(t *testing.T) {
	isolateUserConfigDir(t)
	server := newImageDeleteServer(t, "6.14.1", deletedImage)

	exitCode, envelope, stderr := runBoardsJSON(t, "", "images", "delete", deleteImageName, "--yes", "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	if want := map[string]any{"image_name": deleteImageName}; envelope["operation"] != "images.delete" || !reflect.DeepEqual(envelope["data"], want) {
		t.Fatalf("envelope = %#v, want data %#v", envelope, want)
	}
	if deletes := server.deletes.Load(); deletes != 1 {
		t.Fatalf("sent %d deletes, want one", deletes)
	}
}

func TestImagesDeleteAcceptsRequestDocumentWithExecutionApproval(t *testing.T) {
	isolateUserConfigDir(t)
	server := newImageDeleteServer(t, "6.14.1", deletedImage)
	document := `{"schema_version":1,"image_name":"one-image.png"}`

	exitCode, envelope, stderr := runBoardsJSON(t, document, "images", "delete", "--request", "-", "--yes", "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" || !reflect.DeepEqual(envelope["data"], map[string]any{"image_name": deleteImageName}) {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	if server.deletes.Load() != 1 {
		t.Fatalf("sent %d deletes, want one", server.deletes.Load())
	}
}

func TestImagesDeleteRejectsInvalidRequestsBeforeNetwork(t *testing.T) {
	isolateUserConfigDir(t)
	tests := []struct {
		name  string
		stdin string
		args  []string
	}{
		{name: "missing name", args: []string{"--yes"}},
		{name: "missing yes in request document", stdin: `{"schema_version":1,"image_name":"one-image.png"}`, args: []string{"--request", "-"}},
		{name: "yes inside request document", stdin: `{"schema_version":1,"image_name":"one-image.png","yes":true}`, args: []string{"--request", "-"}},
		{name: "name combined with request document", stdin: `{"schema_version":1,"image_name":"one-image.png"}`, args: []string{deleteImageName, "--request", "-", "--yes"}},
		{name: "empty document name", stdin: `{"schema_version":1,"image_name":""}`, args: []string{"--request", "-", "--yes"}},
		{name: "unsupported schema version", stdin: `{"schema_version":2,"image_name":"one-image.png"}`, args: []string{"--request", "-", "--yes"}},
		{name: "bulk selector in document", stdin: `{"schema_version":1,"image_name":"one-image.png","image_names":["two.png"]}`, args: []string{"--request", "-", "--yes"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newImageDeleteServer(t, "6.14.1", deletedImage)
			args := append([]string{"images", "delete", "--url", server.URL}, test.args...)

			exitCode, envelope, stderr := runBoardsJSON(t, test.stdin, args...)

			if exitCode != result.ExitInvalidRequest || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			if code, _ := errorDetails(t, envelope); envelope["operation"] != "images.delete" || code != "invalid_request" {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
			if requests := server.requests.Load(); requests != 0 {
				t.Fatalf("sent %d requests, want none", requests)
			}
		})
	}
}

func TestImagesDeleteRejectsPathLikeNamesBeforeNetwork(t *testing.T) {
	isolateUserConfigDir(t)
	for _, name := range []string{"x/../one-image.png", "../one-image.png", "../uncategorized", "../intermediates", "..%2Funcategorized", "%2e%2e%2fintermediates", "folder/image.png", `folder\image.png`, ".", ".."} {
		t.Run(name, func(t *testing.T) {
			server := newImageDeleteServer(t, "6.14.1", deletedImage)

			exitCode, envelope, stderr := runBoardsJSON(t, "", "images", "delete", name, "--yes", "--url", server.URL)

			if exitCode != result.ExitInvalidRequest || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			if code, _ := errorDetails(t, envelope); code != "invalid_request" {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
			if requests := server.requests.Load(); requests != 0 {
				t.Fatalf("sent %d requests, want none", requests)
			}
		})
	}
}

func TestImagesDeleteClassifiesOneMutationFailureWithoutRetry(t *testing.T) {
	isolateUserConfigDir(t)
	tests := []struct {
		name     string
		serve    http.HandlerFunc
		wantCode string
	}{
		{name: "absent image", serve: func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }, wantCode: "not_found"},
		{name: "conclusive rejection", serve: func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "failed", http.StatusInternalServerError) }, wantCode: "invokeai_operation_failed"},
		{name: "gateway status", serve: func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}, wantCode: "outcome_unknown"},
		{name: "lost response", serve: func(w http.ResponseWriter, _ *http.Request) {
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack delete connection: %v", err)
				return
			}
			_ = connection.Close()
		}, wantCode: "outcome_unknown"},
		{name: "invalid success body", serve: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"deleted_images":`)) }, wantCode: "outcome_unknown"},
		{name: "trailing junk after success", serve: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"deleted_images":["one-image.png"],"failed_images":[]}junk`))
		}, wantCode: "outcome_unknown"},
		{name: "second JSON value after success", serve: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"deleted_images":["one-image.png"],"failed_images":[]}{}`))
		}, wantCode: "outcome_unknown"},
		{name: "unconfirmed success", serve: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"deleted_images":[]}`)) }, wantCode: "outcome_unknown"},
		{name: "missing failure report", serve: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"deleted_images":["one-image.png"]}`))
		}, wantCode: "outcome_unknown"},
		{name: "another deleted name", serve: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"deleted_images":["other.png"]}`))
		}, wantCode: "outcome_unknown"},
		{name: "extra deleted name", serve: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"deleted_images":["one-image.png","other.png"]}`))
		}, wantCode: "outcome_unknown"},
		{name: "failed image", serve: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"deleted_images":["one-image.png"],"failed_images":["one-image.png"]}`))
		}, wantCode: "outcome_unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newImageDeleteServer(t, "6.14.1", test.serve)

			exitCode, envelope, stderr := runBoardsJSON(t, "", "images", "delete", deleteImageName, "--yes", "--url", server.URL)

			if exitCode != result.ExitInvokeAIFailure || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			if code, _ := errorDetails(t, envelope); envelope["operation"] != "images.delete" || code != test.wantCode {
				t.Fatalf("unexpected envelope: %#v, want code %q", envelope, test.wantCode)
			}
			if deletes := server.deletes.Load(); deletes != 1 {
				t.Fatalf("sent %d deletes, want one", deletes)
			}
		})
	}
}

func TestImagesDeleteRequiresSupportedInvokeAIVersionBeforeMutation(t *testing.T) {
	isolateUserConfigDir(t)
	for _, version := range []string{"6.14.0", "6.15.0"} {
		t.Run(version, func(t *testing.T) {
			server := newImageDeleteServer(t, version, deletedImage)

			exitCode, envelope, stderr := runBoardsJSON(t, "", "images", "delete", deleteImageName, "--yes", "--url", server.URL)

			if exitCode != result.ExitUnsupportedCapability || stderr != "" || server.deletes.Load() != 0 {
				t.Fatalf("exit code = %d, stderr = %q, deletes = %d, envelope = %#v", exitCode, stderr, server.deletes.Load(), envelope)
			}
		})
	}
}

func TestImagesDeleteHumanOutputNamesTheDeletedImage(t *testing.T) {
	isolateUserConfigDir(t)
	server := newImageDeleteServer(t, "6.14.1", deletedImage)
	var stdout, stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(""), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "delete", deleteImageName, "--yes", "--url", server.URL})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 || stdout.String() != deleteImageName+"\n" {
		t.Fatalf("exit code = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
}
