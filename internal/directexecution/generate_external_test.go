package directexecution_test

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/avienor/bediz/internal/directexecution"
	"github.com/avienor/bediz/internal/generation"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/images"
	"github.com/avienor/bediz/internal/profiles"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/sourceimage"
	"github.com/avienor/bediz/internal/structurederror"
)

const (
	versionPath = "/api/v1/app/version"
	modelsPath  = "/api/v2/models/"
	uploadPath  = "/api/v1/images/upload"
	enqueuePath = "/api/v1/queue/default/enqueue_batch"
	recallPath  = "/api/v1/recall/default"
	itemPath    = "/api/v1/queue/default/i/19"
)

type invokeAIFake struct {
	server    *httptest.Server
	mu        sync.Mutex
	requests  []string
	responses map[string]http.HandlerFunc
}

func newInvokeAIFake(t *testing.T) *invokeAIFake {
	t.Helper()
	openAPI, err := os.ReadFile("../doctor/testdata/invokeai_6_14_anima_openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	fake := &invokeAIFake{responses: map[string]http.HandlerFunc{}}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		fake.requests = append(fake.requests, r.Method+" "+r.URL.Path)
		fake.mu.Unlock()
		if handler := fake.responses[r.URL.Path]; handler != nil {
			handler(w, r)
			return
		}
		switch r.URL.Path {
		case versionPath:
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case modelsPath:
			_, _ = w.Write([]byte(`{"models":[{"key":"sdxl-main","hash":"main-hash","name":"SDXL Main","base":"sdxl","type":"main","format":"diffusers"}]}`))
		case "/openapi.json":
			_, _ = w.Write(openAPI)
		case uploadPath:
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			defer r.MultipartForm.RemoveAll()
			w.WriteHeader(http.StatusCreated)
			_ = json.MarshalWrite(w, imageReference("uploaded.png"))
		case enqueuePath:
			var request generation.EnqueueRequest
			if err := json.UnmarshalRead(r.Body, &request); err != nil {
				t.Error(err)
			}
			_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "batch": map[string]any{"batch_id": "batch-a"}, "item_ids": []int{19}, "enqueued": 1, "requested": 1})
		case recallPath:
			_, _ = w.Write([]byte(`{"status":"success"}`))
		case itemPath:
			_, _ = w.Write([]byte(`{"item_id":19,"queue_id":"default","batch_id":"batch-a","status":"completed","field_values":[{"node_path":"seed","field_name":"value","value":42}],"session":{"results":{"decode":{"type":"image_output","image":{"image_name":"output.png"}}}}}`))
		case "/api/v1/images/i/output.png":
			_ = json.MarshalWrite(w, imageReference("output.png"))
		default:
			t.Errorf("unexpected InvokeAI request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func (fake *invokeAIFake) client(t *testing.T) *httpclient.Client {
	t.Helper()
	client, err := httpclient.New(fake.server.URL, "", httpclient.Options{HTTPClient: fake.server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func (fake *invokeAIFake) sequence() []string {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return slices.Clone(fake.requests)
}

func imageReference(name string) images.Reference {
	return images.Reference{ImageName: name, ImageURL: "/images/" + name, ThumbnailURL: "/thumbnails/" + name, Width: 64, Height: 64}
}

func pathRequest(t *testing.T) generation.Request {
	return pathRequestWithSize(t, 64, 64)
}

func pathRequestWithSize(t *testing.T, width, height int) generation.Request {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return generation.Request{SchemaVersion: 1, Model: "sdxl-main", PositivePrompt: "lighthouse", Seed: new(uint32(42)), Source: &sourceimage.Source{Type: "path", Reference: path}}
}

func TestGenerateUploadsAndEnqueuesOnceAfterPreflightThenSynchronizesAndWaits(t *testing.T) {
	fake := newInvokeAIFake(t)
	request := pathRequest(t)
	receipt, err := directexecution.Generate(t.Context(), fake.client(t), request, directexecution.Options{})
	if err != nil {
		t.Fatal(err)
	}
	wantRequests := []string{
		"GET " + versionPath, "GET " + modelsPath, "GET /openapi.json",
		"POST " + uploadPath, "POST " + enqueuePath,
		"GET " + versionPath, "GET /openapi.json", "GET " + modelsPath, "POST " + recallPath,
		"GET " + itemPath, "GET /api/v1/images/i/output.png",
	}
	if got := fake.sequence(); !slices.Equal(got, wantRequests) {
		t.Fatalf("requests = %q, want %q", got, wantRequests)
	}
	if receipt.SourceImage == nil || receipt.SourceImage.ImageName != "uploaded.png" || receipt.SourceUploaded == nil || !*receipt.SourceUploaded ||
		len(receipt.Outputs) != 1 || receipt.Outputs[0].Image.ImageName != "output.png" || receipt.Outputs[0].Seed != 42 ||
		len(receipt.Warnings) != 1 || receipt.Warnings[0].Code != "ui_sync_partial" {
		t.Fatalf("receipt = %#v", receipt)
	}
}

func skippedPreference(t *testing.T) []result.Warning {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := profiles.Create(profiles.Document{SchemaVersion: 1, Name: "preset", Generate: &profiles.Generate{Components: &profiles.GenerateComponents{VAE: new("absent-vae")}}}, false); err != nil {
		t.Fatal(err)
	}
	return []result.Warning{{
		Code:    "profile_preference_skipped",
		Message: `profile "preset" vae preference could not be used; automatic component resolution continues`,
		Details: map[string]any{"profile": "preset", "component": "vae", "reason": "not_found"},
	}}
}

func TestGenerateEnqueueFailureKeepsUploadedSourceAndProfileWarnings(t *testing.T) {
	wantWarnings := skippedPreference(t)
	fake := newInvokeAIFake(t)
	fake.responses[enqueuePath] = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}
	request := pathRequest(t)
	request.Profile = "preset"
	_, err := directexecution.Generate(t.Context(), fake.client(t), request, directexecution.Options{})
	if err == nil {
		t.Fatal("rejected enqueue succeeded")
	}
	failure := structurederror.Classify(result.OperationGenerate, err)
	if failure.Error.Code != "invokeai_operation_failed" || failure.Error.Details["source_uploaded"] != true ||
		!reflect.DeepEqual(failure.Warnings, wantWarnings) {
		t.Fatalf("failure = %#v", failure)
	}
	wantSource := imageReference("uploaded.png")
	wantSource.ImageURL = fake.server.URL + wantSource.ImageURL
	wantSource.ThumbnailURL = fake.server.URL + wantSource.ThumbnailURL
	if !reflect.DeepEqual(failure.Error.Details["source_image"], wantSource) {
		t.Fatalf("source = %#v, want %#v", failure.Error.Details["source_image"], wantSource)
	}
	wantRequests := []string{"GET " + versionPath, "GET " + modelsPath, "GET /openapi.json", "POST " + uploadPath, "POST " + enqueuePath}
	if got := fake.sequence(); !slices.Equal(got, wantRequests) {
		t.Fatalf("requests = %q, want %q", got, wantRequests)
	}
}

func TestGenerateUncertainEnqueueIsSentOnceAndKeepsUploadedSource(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "gateway response", status: http.StatusBadGateway},
		{name: "incomplete acceptance", status: http.StatusOK, body: `{}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			wantWarnings := skippedPreference(t)
			fake := newInvokeAIFake(t)
			fake.responses[enqueuePath] = func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}
			request := pathRequest(t)
			request.Profile = "preset"
			_, err := directexecution.Generate(t.Context(), fake.client(t), request, directexecution.Options{})
			if err == nil {
				t.Fatal("uncertain enqueue succeeded")
			}
			failure := structurederror.Classify(result.OperationGenerate, err)
			if failure.Error.Code != "outcome_unknown" || failure.Error.Details["source_uploaded"] != true || !reflect.DeepEqual(failure.Warnings, wantWarnings) {
				t.Fatalf("failure = %#v", failure)
			}
			wantRequests := []string{"GET " + versionPath, "GET " + modelsPath, "GET /openapi.json", "POST " + uploadPath, "POST " + enqueuePath}
			if got := fake.sequence(); !slices.Equal(got, wantRequests) {
				t.Fatalf("requests = %q, want %q", got, wantRequests)
			}
		})
	}
}

func TestGenerateMalformedNamedUploadKeepsSourceWithoutProfileWarnings(t *testing.T) {
	for _, invalidURL := range []string{"image", "thumbnail"} {
		t.Run(invalidURL, func(t *testing.T) {
			skippedPreference(t)
			fake := newInvokeAIFake(t)
			source := imageReference("uploaded.png")
			if invalidURL == "image" {
				source.ImageURL = "http://%"
			} else {
				source.ThumbnailURL = "http://%"
			}
			fake.responses[uploadPath] = func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_ = json.MarshalWrite(w, source)
			}
			request := pathRequest(t)
			request.Profile = "preset"
			_, err := directexecution.Generate(t.Context(), fake.client(t), request, directexecution.Options{})
			if err == nil {
				t.Fatal("invalid uploaded image URL succeeded")
			}
			failure := structurederror.Classify(result.OperationGenerate, err)
			if failure.Error.Code != "invalid_invokeai_response" || failure.Error.Details["source_uploaded"] != true ||
				!reflect.DeepEqual(failure.Error.Details["source_image"], source) || len(failure.Warnings) != 0 {
				t.Fatalf("failure = %#v", failure)
			}
			wantRequests := []string{"GET " + versionPath, "GET " + modelsPath, "GET /openapi.json", "POST " + uploadPath}
			if got := fake.sequence(); !slices.Equal(got, wantRequests) {
				t.Fatalf("requests = %q, want %q", got, wantRequests)
			}
		})
	}
}

func TestGenerateInvalidWaitOptionsMakeNoRequests(t *testing.T) {
	for _, options := range []directexecution.Options{{Timeout: -time.Second}, {NoWait: true, Timeout: time.Second}} {
		fake := newInvokeAIFake(t)
		_, err := directexecution.Generate(t.Context(), fake.client(t), pathRequest(t), options)
		if err == nil {
			t.Fatal("invalid wait options succeeded")
		}
		if failure := structurederror.Classify(result.OperationGenerate, err); failure.Error.Code != "invalid_request" || len(fake.sequence()) != 0 {
			t.Fatalf("failure = %#v, requests = %q", failure, fake.sequence())
		}
	}
}

func TestGeneratePreflightFailuresLeaveNoUploadedImageOrEnqueuedWork(t *testing.T) {
	beforeResolution := []string{"GET " + versionPath, "GET " + modelsPath}
	beforeCompatibility := append(slices.Clone(beforeResolution), "GET /openapi.json")
	for _, test := range []struct {
		name         string
		change       func(*testing.T, *generation.Request)
		endpoint     string
		body         string
		wantCode     string
		wantRequests []string
	}{
		{name: "request validation", change: func(_ *testing.T, request *generation.Request) { request.PositivePrompt = "" }, wantCode: "invalid_request"},
		{name: "profile validation", change: func(_ *testing.T, request *generation.Request) { request.Profile = "../invalid" }, wantCode: "invalid_request"},
		{name: "missing profile", change: func(_ *testing.T, request *generation.Request) { request.Profile = "missing" }, wantCode: "not_found"},
		{name: "source preparation", change: func(_ *testing.T, request *generation.Request) { request.Source.Reference = "relative.png" }, wantCode: "invalid_request"},
		{name: "source header", change: func(t *testing.T, request *generation.Request) {
			if err := os.WriteFile(request.Source.Reference, []byte("not an image"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, wantCode: "invalid_request"},
		{name: "supported version", endpoint: versionPath, body: `{"version":"6.15.0"}`, wantCode: "unsupported_capability", wantRequests: []string{"GET " + versionPath}},
		{name: "inventory", endpoint: modelsPath, body: `{`, wantCode: "invalid_invokeai_response", wantRequests: beforeResolution},
		{name: "main model resolution", change: func(_ *testing.T, request *generation.Request) { request.Model = "missing-main" }, wantCode: "invalid_request", wantRequests: beforeResolution},
		{name: "family resolution", endpoint: modelsPath, body: `{"models":[{"key":"sdxl-main","name":"Other Main","hash":"main-hash","base":"sd-1","type":"main"}]}`, wantCode: "unsupported_capability", wantRequests: beforeResolution},
		{name: "family settings", change: func(_ *testing.T, request *generation.Request) { request.Scheduler = new("unsupported") }, wantCode: "invalid_request", wantRequests: beforeResolution},
		{name: "fallback component resolution", endpoint: modelsPath, body: `{"models":[{"key":"sdxl-main","name":"Anima Main","hash":"main-hash","base":"anima","type":"main"}]}`, wantCode: "missing_component", wantRequests: beforeResolution},
		{name: "source alignment", change: func(t *testing.T, request *generation.Request) { *request = pathRequestWithSize(t, 7, 8) }, wantCode: "invalid_request", wantRequests: beforeResolution},
		{name: "compatibility", endpoint: "/openapi.json", body: `{}`, wantCode: "unsupported_capability", wantRequests: beforeCompatibility},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			fake := newInvokeAIFake(t)
			if test.endpoint != "" {
				fake.responses[test.endpoint] = func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(test.body)) }
			}
			request := pathRequest(t)
			if test.change != nil {
				test.change(t, &request)
			}
			_, err := directexecution.Generate(t.Context(), fake.client(t), request, directexecution.Options{})
			if err == nil {
				t.Fatal("preflight failure succeeded")
			}
			failure := structurederror.Classify(result.OperationGenerate, err)
			if failure.Error.Code != test.wantCode {
				t.Fatalf("failure = %#v, want %s", failure, test.wantCode)
			}
			if got := fake.sequence(); !slices.Equal(got, test.wantRequests) {
				t.Fatalf("requests = %q, want %q", got, test.wantRequests)
			}
		})
	}
}

func TestGenerateCompileFailureKeepsUploadedSourceAndProfileWarnings(t *testing.T) {
	wantWarnings := skippedPreference(t)
	fake := newInvokeAIFake(t)
	fake.responses[uploadPath] = func(w http.ResponseWriter, _ *http.Request) {
		source := imageReference("uploaded.png")
		source.Width = 65
		w.WriteHeader(http.StatusCreated)
		_ = json.MarshalWrite(w, source)
	}
	request := pathRequest(t)
	request.Profile = "preset"
	_, err := directexecution.Generate(t.Context(), fake.client(t), request, directexecution.Options{})
	if err == nil {
		t.Fatal("contradictory source dimensions compiled successfully")
	}
	failure := structurederror.Classify(result.OperationGenerate, err)
	source, ok := failure.Error.Details["source_image"].(images.Reference)
	if failure.Error.Code != "invalid_invokeai_response" || failure.Error.Details["source_uploaded"] != true ||
		!ok || source.ImageName != "uploaded.png" || source.Width != 65 || !reflect.DeepEqual(failure.Warnings, wantWarnings) {
		t.Fatalf("failure = %#v", failure)
	}
	wantRequests := []string{"GET " + versionPath, "GET " + modelsPath, "GET /openapi.json", "POST " + uploadPath}
	if got := fake.sequence(); !slices.Equal(got, wantRequests) {
		t.Fatalf("requests = %q, want %q", got, wantRequests)
	}
}

func TestGenerateWaitFailuresKeepUploadedSourceWithoutProfileWarnings(t *testing.T) {
	for _, test := range []struct {
		name, response, wantCode string
		options                  directexecution.Options
		interrupt                bool
	}{
		{name: "failed item", response: `{"item_id":19,"queue_id":"default","batch_id":"batch-a","status":"failed"}`, wantCode: "invokeai_operation_failed"},
		{name: "canceled item", response: `{"item_id":19,"queue_id":"default","batch_id":"batch-a","status":"canceled"}`, wantCode: "invokeai_operation_failed"},
		{name: "unknown status", response: `{"item_id":19,"queue_id":"default","batch_id":"batch-a","status":"unknown"}`, wantCode: "invalid_invokeai_response"},
		{name: "malformed item", response: `{`, wantCode: "invalid_invokeai_response"},
		{name: "timeout", options: directexecution.Options{Timeout: time.Nanosecond}, wantCode: "wait_timeout"},
		{name: "interruption", interrupt: true, wantCode: "interrupted"},
	} {
		t.Run(test.name, func(t *testing.T) {
			skippedPreference(t)
			fake := newInvokeAIFake(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.interrupt {
				fake.responses[itemPath] = func(w http.ResponseWriter, _ *http.Request) {
					cancel()
					_, _ = w.Write([]byte(`{"item_id":19,"queue_id":"default","batch_id":"batch-a","status":"pending"}`))
				}
			} else if test.response != "" {
				fake.responses[itemPath] = func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(test.response)) }
			}
			request := pathRequest(t)
			request.Profile = "preset"
			_, err := directexecution.Generate(ctx, fake.client(t), request, test.options)
			if err == nil {
				t.Fatal("wait failure succeeded")
			}
			// This assertion moved from generation.Wait to the lifecycle seam.
			uploaded, hasSource := errors.AsType[*sourceimage.UploadedError](err)
			if !hasSource || uploaded.Source.ImageName != "uploaded.png" {
				t.Fatalf("post-upload wait error = %#v", err)
			}
			failure := structurederror.Classify(result.OperationGenerate, err)
			if failure.Error.Code != test.wantCode || len(failure.Warnings) != 0 || failure.Error.Details["source_uploaded"] != true {
				t.Fatalf("failure = %#v", failure)
			}
			requests := fake.sequence()
			if requestCount(requests, "POST "+uploadPath) != 1 || requestCount(requests, "POST "+enqueuePath) != 1 || requestCount(requests, "POST "+recallPath) != 1 {
				t.Fatalf("requests = %q", requests)
			}
		})
	}
}

func requestCount(requests []string, request string) int {
	count := 0
	for _, sent := range requests {
		if sent == request {
			count++
		}
	}
	return count
}

func TestGenerateRecallFailureIsOnlyAWarning(t *testing.T) {
	wantWarnings := skippedPreference(t)
	fake := newInvokeAIFake(t)
	fake.responses[recallPath] = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }
	request := pathRequest(t)
	request.Profile = "preset"
	receipt, err := directexecution.Generate(t.Context(), fake.client(t), request, directexecution.Options{})
	wantWarnings = append(wantWarnings, result.Warning{Code: "ui_sync_failed", Message: "Generation was accepted, but the UI Recall patch could not be confirmed."})
	if err != nil || len(receipt.Outputs) != 1 || !reflect.DeepEqual(receipt.Warnings, wantWarnings) {
		t.Fatalf("receipt = %#v, error = %v", receipt, err)
	}
	requests := fake.sequence()
	if requestCount(requests, "POST "+uploadPath) != 1 || requestCount(requests, "POST "+enqueuePath) != 1 || requestCount(requests, "POST "+recallPath) != 1 {
		t.Fatalf("requests = %q", requests)
	}
}

func TestGenerateNoWaitSynchronizesWithoutInspectingQueueItems(t *testing.T) {
	fake := newInvokeAIFake(t)
	receipt, err := directexecution.Generate(t.Context(), fake.client(t), pathRequest(t), directexecution.Options{NoWait: true})
	if err != nil || len(receipt.Outputs) != 0 || receipt.Outputs == nil || receipt.Queue.BatchID != "batch-a" || !slices.Equal(receipt.Queue.ItemIDs, []int{19}) ||
		len(receipt.Warnings) != 1 || receipt.Warnings[0].Code != "ui_sync_partial" {
		t.Fatalf("receipt = %#v, error = %v", receipt, err)
	}
	wantRequests := []string{
		"GET " + versionPath, "GET " + modelsPath, "GET /openapi.json", "POST " + uploadPath, "POST " + enqueuePath,
		"GET " + versionPath, "GET /openapi.json", "GET " + modelsPath, "POST " + recallPath,
	}
	if got := fake.sequence(); !slices.Equal(got, wantRequests) {
		t.Fatalf("requests = %q, want %q", got, wantRequests)
	}
}
