package directexecution_test

import (
	json "encoding/json/v2"
	"net/http"
	"reflect"
	"slices"
	"testing"

	"github.com/avienor/bediz/internal/directexecution"
	"github.com/avienor/bediz/internal/images"
	"github.com/avienor/bediz/internal/profiles"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/structurederror"
	"github.com/avienor/bediz/internal/upscale"
)

func upscaleRequest(t *testing.T) upscale.Request {
	t.Helper()
	generated := pathRequest(t)
	return upscale.Request{SchemaVersion: 1, Source: *generated.Source, Model: "sdxl-main", Scale: new(2), Seed: new(uint32(42)), Components: &upscale.Components{TileControlNet: new("tile")}}
}

func upscaleFake(t *testing.T) *invokeAIFake {
	t.Helper()
	fake := newInvokeAIFake(t)
	fake.responses[modelsPath] = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"key":"sdxl-main","hash":"main-hash","name":"SDXL Main","base":"sdxl","type":"main","variant":"normal"},{"key":"spandrel","hash":"upscale-hash","name":"Upscale","base":"any","type":"spandrel_image_to_image"},{"key":"tile","hash":"tile-hash","name":"Tile","base":"sdxl","type":"controlnet"}]}`))
	}
	fake.responses[enqueuePath] = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"queue_id":"default","batch":{"batch_id":"batch-a"},"item_ids":[19],"enqueued":1,"requested":1}`))
	}
	return fake
}

func upscalePreference(t *testing.T) result.Warning {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := profiles.Create(profiles.Document{SchemaVersion: 1, Name: "preset", Upscale: &profiles.Upscale{Components: &profiles.UpscaleComponents{VAE: new("absent-vae")}}}, false); err != nil {
		t.Fatal(err)
	}
	return result.Warning{Code: "profile_preference_skipped", Message: `profile "preset" vae preference could not be used; ordinary component resolution continues`, Details: map[string]any{"profile": "preset", "component": "vae", "reason": "not_found"}}
}

func TestUpscaleScaleFailureKeepsUploadedSourceWithoutProfileWarnings(t *testing.T) {
	upscalePreference(t)
	fake := upscaleFake(t)
	request := upscaleRequest(t)
	request.Profile = "preset"
	_, err := directexecution.Upscale(t.Context(), fake.client(t), request, directexecution.Options{})
	if err == nil {
		t.Fatal("wrong scale succeeded")
	}
	failure := structurederror.Classify(result.OperationUpscale, err)
	if failure.Error.Code != result.CodeInvokeAIOperationFailed || failure.Error.Details["reason"] != "scale_not_applied" || failure.Error.Details["expected_width"] != 128 || failure.Error.Details["actual_width"] != 64 || failure.Error.Details["source_uploaded"] != true || len(failure.Warnings) != 0 {
		t.Fatalf("failure = %#v", failure)
	}
	source, ok := failure.Error.Details["source_image"].(images.Reference)
	if !ok || source.ImageName != "uploaded.png" {
		t.Fatalf("source image = %#v", failure.Error.Details["source_image"])
	}
	wantRequests := []string{"GET " + versionPath, "GET " + modelsPath, "GET /openapi.json", "POST " + uploadPath, "POST " + enqueuePath, "GET " + versionPath, "GET /openapi.json", "GET " + modelsPath, "POST " + recallPath, "GET " + itemPath, "GET /api/v1/images/i/output.png"}
	if got := fake.sequence(); !slices.Equal(got, wantRequests) {
		t.Fatalf("requests = %q, want %q", got, wantRequests)
	}
}

func TestUpscaleEnqueueFailureKeepsUploadedSourceAndProfileWarnings(t *testing.T) {
	wantWarning := upscalePreference(t)
	fake := upscaleFake(t)
	fake.responses[enqueuePath] = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnprocessableEntity) }
	request := upscaleRequest(t)
	request.Profile = "preset"
	_, err := directexecution.Upscale(t.Context(), fake.client(t), request, directexecution.Options{NoWait: true})
	if err == nil {
		t.Fatal("rejected enqueue succeeded")
	}
	failure := structurederror.Classify(result.OperationUpscale, err)
	if failure.Error.Code != result.CodeInvokeAIOperationFailed || failure.Error.Details["source_uploaded"] != true || !reflect.DeepEqual(failure.Warnings, []result.Warning{wantWarning}) {
		t.Fatalf("failure = %#v", failure)
	}
	wantRequests := []string{"GET " + versionPath, "GET " + modelsPath, "GET /openapi.json", "POST " + uploadPath, "POST " + enqueuePath}
	if got := fake.sequence(); !slices.Equal(got, wantRequests) {
		t.Fatalf("requests = %q, want %q", got, wantRequests)
	}
}

func TestUpscaleMalformedNamedUploadKeepsSourceWithoutProfileWarnings(t *testing.T) {
	upscalePreference(t)
	fake := upscaleFake(t)
	fake.responses[uploadPath] = func(w http.ResponseWriter, _ *http.Request) {
		image := imageReference("uploaded.png")
		image.ImageURL = "http://["
		w.WriteHeader(http.StatusCreated)
		_ = json.MarshalWrite(w, image)
	}
	request := upscaleRequest(t)
	request.Profile = "preset"
	_, err := directexecution.Upscale(t.Context(), fake.client(t), request, directexecution.Options{NoWait: true})
	if err == nil {
		t.Fatal("malformed upload succeeded")
	}
	failure := structurederror.Classify(result.OperationUpscale, err)
	source, ok := failure.Error.Details["source_image"].(images.Reference)
	if failure.Error.Code != result.CodeInvalidInvokeAIResponse || failure.Error.Details["source_uploaded"] != true || !ok || source.ImageName != "uploaded.png" || len(failure.Warnings) != 0 {
		t.Fatalf("failure = %#v", failure)
	}
	wantRequests := []string{"GET " + versionPath, "GET " + modelsPath, "GET /openapi.json", "POST " + uploadPath}
	if got := fake.sequence(); !slices.Equal(got, wantRequests) {
		t.Fatalf("requests = %q, want %q", got, wantRequests)
	}
}

func TestUpscaleRecallFailureIsOnlyAWarning(t *testing.T) {
	fake := upscaleFake(t)
	fake.responses[recallPath] = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }
	receipt, err := directexecution.Upscale(t.Context(), fake.client(t), upscaleRequest(t), directexecution.Options{NoWait: true})
	want := []result.Warning{{Code: "ui_sync_failed", Message: "Upscale was accepted, but the UI Recall patch could not be confirmed."}}
	if err != nil || receipt.SourceImage.ImageName != "uploaded.png" || !receipt.SourceUploaded || !reflect.DeepEqual(receipt.Warnings, want) || len(receipt.Outputs) != 0 {
		t.Fatalf("receipt = %#v, error = %v", receipt, err)
	}
	if got := fake.sequence(); !slices.Equal(got, []string{"GET " + versionPath, "GET " + modelsPath, "GET /openapi.json", "POST " + uploadPath, "POST " + enqueuePath, "GET " + versionPath, "GET /openapi.json", "GET " + modelsPath, "POST " + recallPath}) {
		t.Fatalf("requests = %q", got)
	}
}
