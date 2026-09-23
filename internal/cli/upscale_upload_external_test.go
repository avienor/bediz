package cli_test

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"sync"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

var uploadSourcePNG = []byte("\x89PNG\r\n\x1a\nlocal upscale source")

// upscaleUploadServer is a fake InvokeAI that records every request in order,
// so tests can assert the complete sequence rather than only the final result.
type upscaleUploadServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string
}

type upscaleUploadHandlers struct {
	inventory []map[string]any
	version   string
	upload    http.HandlerFunc
	enqueue   http.HandlerFunc
	item      http.HandlerFunc
	output    http.HandlerFunc
}

func uploadedSourceImage() map[string]any {
	image := upscaleImage("uploaded.png", 513, 257)
	image["image_origin"] = "external"
	image["image_category"] = "user"
	return image
}

func acceptUpload(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusCreated)
	_ = json.MarshalWrite(w, uploadedSourceImage())
}

func acceptEnqueue(w http.ResponseWriter, _ *http.Request) {
	_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "batch": map[string]any{"batch_id": "upscale-batch"}, "item_ids": []int{19}, "enqueued": 1, "requested": 1})
}

func newUpscaleUploadServer(t *testing.T, handlers upscaleUploadHandlers) *upscaleUploadServer {
	t.Helper()
	if handlers.inventory == nil {
		handlers.inventory = upscaleInventory()
	}
	if handlers.version == "" {
		handlers.version = "6.14.1"
	}
	if handlers.upload == nil {
		handlers.upload = acceptUpload
	}
	if handlers.enqueue == nil {
		handlers.enqueue = acceptEnqueue
	}
	if handlers.item == nil {
		handlers.item = func(w http.ResponseWriter, _ *http.Request) {
			_ = json.MarshalWrite(w, completedBatchItem(19, "upscale-batch", 42, "output.png"))
		}
	}
	if handlers.output == nil {
		handlers.output = func(w http.ResponseWriter, _ *http.Request) {
			_ = json.MarshalWrite(w, upscaleImage("output.png", 1024, 512))
		}
	}
	server := &upscaleUploadServer{}
	openAPI := sdxlOpenAPIFixture(t)
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.mu.Lock()
		server.requests = append(server.requests, r.Method+" "+r.URL.Path)
		server.mu.Unlock()
		if serveAnimaPreflight(w, r, handlers.version, openAPI, handlers.inventory) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/images/upload":
			handlers.upload(w, r)
		case "/api/v1/queue/default/enqueue_batch":
			handlers.enqueue(w, r)
		case "/api/v1/queue/default/i/19":
			handlers.item(w, r)
		case "/api/v1/images/i/output.png":
			handlers.output(w, r)
		case "/api/v1/recall/default":
			_, _ = w.Write([]byte(`{"status":"success"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func (s *upscaleUploadServer) sequence() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}

func (s *upscaleUploadServer) count(request string) int {
	count := 0
	for _, seen := range s.sequence() {
		if seen == request {
			count++
		}
	}
	return count
}

const (
	uploadRequest  = "POST /api/v1/images/upload"
	enqueueRequest = "POST /api/v1/queue/default/enqueue_batch"
	recallRequest  = "POST /api/v1/recall/default"
)

// recallRequests are the read-only Recall checks and the single patch that
// follow a conclusive enqueue.
var recallRequests = []string{"GET /api/v1/app/version", "GET /openapi.json", "GET /api/v2/models/", recallRequest}

// preflightRequests are the read-only validation and resolution requests that
// must all complete, in any order, before the local source is uploaded.
var preflightRequests = []string{"GET /api/v1/app/version", "GET /api/v2/models/", "GET /openapi.json"}

// assertUploadFollowsPreflight requires the sequence to begin with every
// preflight request, then continue with exactly the given requests.
func assertUploadFollowsPreflight(t *testing.T, sequence []string, then ...string) {
	t.Helper()
	if len(sequence) < len(preflightRequests) {
		t.Fatalf("request sequence = %q", sequence)
	}
	preflight := slices.Sorted(slices.Values(sequence[:len(preflightRequests)]))
	if !slices.Equal(preflight, slices.Sorted(slices.Values(preflightRequests))) || !slices.Equal(sequence[len(preflightRequests):], then) {
		t.Fatalf("request sequence = %q\nwant preflight %q, then %q", sequence, preflightRequests, then)
	}
}

func writeUploadSource(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.png")
	if err := os.WriteFile(path, uploadSourcePNG, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runUpscaleContext(t *testing.T, ctx context.Context, serverURL string, args ...string) (int, upscaleReceiptEnvelope) {
	t.Helper()
	isolateUserConfigDir(t)
	var stdout, stderr bytes.Buffer
	arguments := append(slices.Clone(args), "--url", serverURL, "--json")
	code := cli.New(&stdout, &stderr).Run(ctx, arguments)
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	var envelope upscaleReceiptEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v; stdout=%s", err, stdout.String())
	}
	if envelope.Operation != "upscale" {
		t.Fatalf("operation = %q; stdout=%s", envelope.Operation, stdout.String())
	}
	return code, envelope
}

// assertUploadedSourceDetails requires the complete uploaded Image Reference,
// not only its name, in a post-upload failure.
func assertUploadedSourceDetails(t *testing.T, serverURL string, details map[string]any) {
	t.Helper()
	source, ok := details["source_image"].(map[string]any)
	if !ok || details["source_uploaded"] != true {
		t.Fatalf("details lack the uploaded source: %#v", details)
	}
	want := map[string]any{
		"image_name": "uploaded.png", "width": float64(513), "height": float64(257),
		"image_url":     serverURL + "/api/v1/images/i/uploaded.png/full",
		"thumbnail_url": serverURL + "/api/v1/images/i/uploaded.png/thumbnail",
		"image_origin":  "external", "image_category": "user", "is_intermediate": false,
	}
	for field, value := range want {
		if source[field] != value {
			t.Fatalf("source_image.%s = %#v, want %#v; details=%#v", field, source[field], value, details)
		}
	}
}

func TestUpscaleLocalPathUploadsOnceAfterResolutionThenEnqueuesOnce(t *testing.T) {
	path := writeUploadSource(t)
	var enqueued map[string]any
	server := newUpscaleUploadServer(t, upscaleUploadHandlers{
		upload: func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query()
			if query.Get("image_category") != "user" || query.Get("is_intermediate") != "false" || query.Has("board_id") || query.Has("resize_to") || query.Has("metadata") {
				t.Errorf("upload query = %s", r.URL.RawQuery)
			}
			file, header, err := r.FormFile("file")
			if err != nil {
				t.Errorf("read uploaded form file: %v", err)
				return
			}
			defer func() { _ = file.Close() }()
			body, _ := io.ReadAll(file)
			if header.Filename != "source.png" || !bytes.Equal(body, uploadSourcePNG) || header.Header.Get("Content-Type") != "image/png" {
				t.Errorf("uploaded file name=%q type=%q body=%q", header.Filename, header.Header.Get("Content-Type"), body)
			}
			if r.MultipartForm != nil && len(r.MultipartForm.Value) != 0 {
				t.Errorf("upload carries extra form fields: %#v", r.MultipartForm.Value)
			}
			acceptUpload(w, r)
		},
		enqueue: func(w http.ResponseWriter, r *http.Request) {
			if err := json.UnmarshalRead(r.Body, &enqueued); err != nil {
				t.Errorf("decode enqueue: %v", err)
			}
			acceptEnqueue(w, r)
		},
	})
	code, envelope := runUpscale(t, server.URL, "upscale", "--image-path", path, "--model", "sdxl-main", "--tile-controlnet", "tile", "--seed", "42", "--scale", "2")
	if code != 0 || !envelope.OK {
		t.Fatalf("code=%d envelope=%#v", code, envelope)
	}
	assertUploadFollowsPreflight(t, server.sequence(), slices.Concat([]string{uploadRequest, enqueueRequest}, recallRequests, []string{"GET /api/v1/queue/default/i/19", "GET /api/v1/images/i/output.png"})...)
	assertUpscaleSyncWarning(t, envelope, "ui_sync_partial")
	data := envelope.Data
	if !data.SourceUploaded || data.SourceImage.ImageName != "uploaded.png" || data.SourceImage.Width != 513 || data.SourceImage.Height != 257 {
		t.Fatalf("source = %#v uploaded=%v", data.SourceImage, data.SourceUploaded)
	}
	// 513 × 257 at scale 2 targets 1026 × 514, rounded down to multiples of 8.
	if data.ResolvedSettings.OutputWidth != 1024 || data.ResolvedSettings.OutputHeight != 512 || len(data.Outputs) != 1 {
		t.Fatalf("resolved settings = %#v outputs=%#v", data.ResolvedSettings, data.Outputs)
	}
	if source := data.SubmittedRequest["source"]; !reflect.DeepEqual(source, map[string]any{"type": "path", "reference": path}) {
		t.Fatalf("submitted source = %#v", source)
	}
	autoscale := enqueued["batch"].(map[string]any)["graph"].(map[string]any)["nodes"].(map[string]any)["autoscale"].(map[string]any)
	if !reflect.DeepEqual(autoscale["image"], map[string]any{"image_name": "uploaded.png", "width": float64(513), "height": float64(257)}) {
		t.Fatalf("autoscale image = %#v", autoscale["image"])
	}
}

func TestUpscaleLocalPathDocumentMatchesFlags(t *testing.T) {
	path := writeUploadSource(t)
	server := newUpscaleUploadServer(t, upscaleUploadHandlers{})
	code, fromFlags := runUpscale(t, server.URL, "upscale", "--no-wait", "--image-path", path, "--model", "sdxl-main", "--tile-controlnet", "tile", "--seed", "42")
	if code != 0 || !fromFlags.OK || !fromFlags.Data.SourceUploaded {
		t.Fatalf("flags: code=%d %#v", code, fromFlags)
	}
	document, err := json.Marshal(map[string]any{"schema_version": 1, "source": map[string]any{"type": "path", "reference": path}, "model": "sdxl-main", "components": map[string]any{"tile_controlnet": "tile"}, "seed": 42})
	if err != nil {
		t.Fatal(err)
	}
	requestPath := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(requestPath, document, 0o600); err != nil {
		t.Fatal(err)
	}
	code, fromDocument := runUpscale(t, server.URL, "upscale", "--no-wait", "--request", requestPath)
	if code != 0 || !reflect.DeepEqual(fromFlags.Data, fromDocument.Data) {
		t.Fatalf("document: code=%d flags=%#v document=%#v", code, fromFlags.Data, fromDocument.Data)
	}
	if server.count(uploadRequest) != 2 || server.count(enqueueRequest) != 2 {
		t.Fatalf("sequence = %q", server.sequence())
	}
}

func TestUpscaleLocalPathRejectedBeforeNetwork(t *testing.T) {
	directory := t.TempDir()
	unreadable := filepath.Join(directory, "unreadable.png")
	if err := os.WriteFile(unreadable, uploadSourcePNG, 0o000); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		args []string
	}{
		{"relative", []string{"--image-path", "source.png"}},
		{"missing", []string{"--image-path", filepath.Join(directory, "missing.png")}},
		{"directory", []string{"--image-path", directory}},
		{"empty", []string{"--image-path", ""}},
		{"both sources", []string{"--image", "source.png", "--image-path", writeUploadSource(t)}},
		{"no source", nil},
	}
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		cases = append(cases, struct {
			name string
			args []string
		}{"unreadable", []string{"--image-path", unreadable}})
	}
	server := newUpscaleUploadServer(t, upscaleUploadHandlers{})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"upscale", "--model", "sdxl-main", "--tile-controlnet", "tile"}, tc.args...)
			code, envelope := runUpscale(t, server.URL, args...)
			if code != result.ExitInvalidRequest || envelope.Error == nil || envelope.Error.Code != result.CodeInvalidRequest || len(server.sequence()) != 0 {
				t.Fatalf("code=%d envelope=%#v sequence=%q", code, envelope, server.sequence())
			}
		})
	}
	t.Run("relative document path", func(t *testing.T) {
		requestPath := filepath.Join(t.TempDir(), "request.json")
		document := `{"schema_version":1,"source":{"type":"path","reference":"source.png"},"model":"sdxl-main","components":{"tile_controlnet":"tile"}}`
		if err := os.WriteFile(requestPath, []byte(document), 0o600); err != nil {
			t.Fatal(err)
		}
		code, envelope := runUpscale(t, server.URL, "upscale", "--request", requestPath)
		if code != result.ExitInvalidRequest || envelope.Error == nil || envelope.Error.Code != result.CodeInvalidRequest || len(server.sequence()) != 0 {
			t.Fatalf("code=%d envelope=%#v sequence=%q", code, envelope, server.sequence())
		}
	})
}

func TestUpscaleLocalPathResolutionFailuresNeverUpload(t *testing.T) {
	path := writeUploadSource(t)
	withoutSpandrel := slices.DeleteFunc(upscaleInventory(), func(model map[string]any) bool { return model["key"] == "spandrel" })
	for _, tc := range []struct {
		name     string
		handlers upscaleUploadHandlers
		tileFlag bool
		wantCode string
	}{
		{"unsupported version", upscaleUploadHandlers{version: "6.13.0"}, true, result.CodeUnsupportedCapability},
		{"Tile ControlNet selection", upscaleUploadHandlers{}, false, result.CodeSelectionRequired},
		{"missing Spandrel", upscaleUploadHandlers{inventory: withoutSpandrel}, true, result.CodeMissingComponent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := newUpscaleUploadServer(t, tc.handlers)
			args := []string{"upscale", "--image-path", path, "--model", "sdxl-main", "--seed", "42"}
			if tc.tileFlag {
				args = append(args, "--tile-controlnet", "tile")
			}
			code, envelope := runUpscale(t, server.URL, args...)
			if code != result.ExitStatus(tc.wantCode) || envelope.Error == nil || envelope.Error.Code != tc.wantCode {
				t.Fatalf("code=%d envelope=%#v", code, envelope)
			}
			if server.count(uploadRequest) != 0 || server.count(enqueueRequest) != 0 {
				t.Fatalf("sequence = %q", server.sequence())
			}
			if _, ok := envelope.Error.Details["source_image"]; ok {
				t.Fatalf("pre-upload failure reports a source image: %#v", envelope.Error.Details)
			}
		})
	}
}

func TestUpscaleLocalPathUploadFailureNeverEnqueues(t *testing.T) {
	path := writeUploadSource(t)
	for _, tc := range []struct {
		name     string
		upload   http.HandlerFunc
		wantCode string
	}{
		{"inconclusive", func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			panic(http.ErrAbortHandler)
		}, result.CodeOutcomeUnknown},
		{"malformed response", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"image_name":`))
		}, result.CodeOutcomeUnknown},
		{"response without image name", func(w http.ResponseWriter, r *http.Request) {
			image := uploadedSourceImage()
			delete(image, "image_name")
			w.WriteHeader(http.StatusCreated)
			_ = json.MarshalWrite(w, image)
		}, result.CodeOutcomeUnknown},
		{"rejected", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"detail":"Not an image"}`, http.StatusUnsupportedMediaType)
		}, result.CodeInvokeAIOperationFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := newUpscaleUploadServer(t, upscaleUploadHandlers{upload: tc.upload})
			code, envelope := runUpscale(t, server.URL, "upscale", "--image-path", path, "--model", "sdxl-main", "--tile-controlnet", "tile", "--seed", "42")
			if code != result.ExitStatus(tc.wantCode) || envelope.Error == nil || envelope.Error.Code != tc.wantCode {
				t.Fatalf("code=%d envelope=%#v", code, envelope)
			}
			assertUploadFollowsPreflight(t, server.sequence(), uploadRequest)
		})
	}
}

func TestUpscaleLocalPathPostUploadFailuresReportUploadedSource(t *testing.T) {
	path := writeUploadSource(t)
	queueItem := func(status string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			_ = json.MarshalWrite(w, map[string]any{"item_id": 19, "queue_id": "default", "batch_id": "upscale-batch", "session_id": "session-19", "status": status, "priority": 0, "created_at": "2026-01-01", "updated_at": "2026-01-01", "error_type": "ModelError", "error_message": "upscale failed"})
		}
	}
	for _, tc := range []struct {
		name       string
		handlers   upscaleUploadHandlers
		extraArgs  []string
		interrupt  bool
		wantCode   string
		wantReason string
	}{
		{name: "rejected enqueue", handlers: upscaleUploadHandlers{enqueue: func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"detail":"invalid graph"}`, http.StatusUnprocessableEntity)
		}}, wantCode: result.CodeInvokeAIOperationFailed},
		{name: "inconclusive enqueue", handlers: upscaleUploadHandlers{enqueue: func(http.ResponseWriter, *http.Request) {
			panic(http.ErrAbortHandler)
		}}, wantCode: result.CodeOutcomeUnknown},
		{name: "malformed enqueue response", handlers: upscaleUploadHandlers{enqueue: func(w http.ResponseWriter, _ *http.Request) {
			_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "item_ids": []int{}})
		}}, wantCode: result.CodeOutcomeUnknown},
		{name: "malformed queue response", handlers: upscaleUploadHandlers{item: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"item_id":`))
		}}, wantCode: result.CodeInvalidInvokeAIResponse},
		{name: "unknown queue status", handlers: upscaleUploadHandlers{item: queueItem("mystery")}, wantCode: result.CodeInvalidInvokeAIResponse},
		{name: "failed item", handlers: upscaleUploadHandlers{item: queueItem("failed")}, wantCode: result.CodeInvokeAIOperationFailed},
		{name: "canceled item", handlers: upscaleUploadHandlers{item: queueItem("canceled")}, wantCode: result.CodeInvokeAIOperationFailed},
		{name: "scale not applied", handlers: upscaleUploadHandlers{output: func(w http.ResponseWriter, _ *http.Request) {
			_ = json.MarshalWrite(w, upscaleImage("output.png", 513, 257))
		}}, wantCode: result.CodeInvokeAIOperationFailed, wantReason: "scale_not_applied"},
		{name: "wait timeout", handlers: upscaleUploadHandlers{item: queueItem("in_progress")}, extraArgs: []string{"--timeout", "1ms"}, wantCode: result.CodeWaitTimeout},
		{name: "interrupted", handlers: upscaleUploadHandlers{item: queueItem("in_progress")}, interrupt: true, wantCode: result.CodeInterrupted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.interrupt {
				poll := tc.handlers.item
				tc.handlers.item = func(w http.ResponseWriter, r *http.Request) {
					cancel()
					poll(w, r)
				}
			}
			server := newUpscaleUploadServer(t, tc.handlers)
			args := append([]string{"upscale", "--image-path", path, "--model", "sdxl-main", "--tile-controlnet", "tile", "--seed", "42", "--scale", "2"}, tc.extraArgs...)
			code, envelope := runUpscaleContext(t, ctx, server.URL, args...)
			if code != result.ExitStatus(tc.wantCode) || envelope.OK || envelope.Error == nil || envelope.Error.Code != tc.wantCode {
				t.Fatalf("code=%d envelope=%#v", code, envelope)
			}
			assertUploadedSourceDetails(t, server.URL, envelope.Error.Details)
			if tc.wantReason != "" && envelope.Error.Details["reason"] != tc.wantReason {
				t.Fatalf("reason = %#v", envelope.Error.Details["reason"])
			}
			sequence := server.sequence()
			if server.count(uploadRequest) != 1 || server.count(enqueueRequest) != 1 || server.count(recallRequest) > 1 {
				t.Fatalf("sequence = %q", sequence)
			}
			for _, request := range sequence {
				if !slices.Contains([]string{uploadRequest, enqueueRequest, recallRequest}, request) && request[:4] != "GET " {
					t.Fatalf("unexpected mutation %q in sequence %q", request, sequence)
				}
			}
			// Everything after the enqueue is the single Recall patch and read-only waiting, checked above.
			assertUploadFollowsPreflight(t, sequence[:len(preflightRequests)+2], uploadRequest, enqueueRequest)
		})
	}
}
