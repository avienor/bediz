package cli_test

import (
	"bytes"
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

func TestGenerateProfileWarningsSurviveFallbackResolutionFailure(t *testing.T) {
	isolateUserConfigDir(t)
	createGenerateProfile(t, `{"schema_version":1,"name":"preset","generate":{"components":{"vae":"absent-vae"}}}`)
	inventory := animaModelInventory()[:1]
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), inventory) {
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	code, envelope := runImg2Img(t, "--no-wait", "--profile", "preset", "--model", "main-key", "--prompt", "test", "--seed", "42", "--url", server.URL)
	if code != result.ExitUnsupportedCapability || envelope["ok"] != false || envelope["operation"] != "generate" || envelope["schema_version"] != float64(1) {
		t.Fatalf("code=%d envelope=%#v", code, envelope)
	}
	failure := envelope["error"].(map[string]any)
	if failure["code"] != "missing_component" || failure["details"].(map[string]any)["component_type"] != "vae" {
		t.Fatalf("error=%#v", failure)
	}
	assertGenerateProfileSkipWarning(t, envelope)
}

func TestGenerateProfileWarningsSurvivePreEnqueueFailures(t *testing.T) {
	tests := []struct {
		name, source, endpoint, response, wantCode string
		status                                     int
		wantUploaded                               bool
	}{
		{name: "compatibility check", endpoint: "/openapi.json", response: `{}`, wantCode: "unsupported_capability"},
		{name: "image-to-image compatibility check", source: "image", endpoint: "/openapi.json", response: `{}`, wantCode: "unsupported_capability"},
		{name: "source confirmation", source: "image", endpoint: "/api/v1/images/i/source.png", status: http.StatusNotFound, wantCode: "not_found"},
		{name: "contradictory source name", source: "image", endpoint: "/api/v1/images/i/source.png", response: `{"image_name":"other.png","image_url":"/images/other.png","thumbnail_url":"/thumbnails/other.png","width":16,"height":8}`, wantCode: "invalid_invokeai_response"},
		{name: "source below family alignment", source: "image", endpoint: "/api/v1/images/i/source.png", response: `{"image_name":"source.png","image_url":"/images/source.png","thumbnail_url":"/thumbnails/source.png","width":7,"height":8}`, wantCode: "invalid_request"},
		{name: "upload rejection", source: "path", endpoint: "/api/v1/images/upload", status: http.StatusUnprocessableEntity, wantCode: "invokeai_operation_failed"},
		{name: "inconclusive upload", source: "path", endpoint: "/api/v1/images/upload", status: http.StatusBadGateway, wantCode: "outcome_unknown"},
		{name: "upload without image name", source: "path", endpoint: "/api/v1/images/upload", response: `{}`, wantCode: "outcome_unknown"},
		{name: "uploaded dimensions differ", source: "path", endpoint: "/api/v1/images/upload", response: `{"image_name":"uploaded.png","image_url":"/images/uploaded.png","thumbnail_url":"/thumbnails/uploaded.png","width":17,"height":8}`, wantCode: "invalid_invokeai_response", wantUploaded: true},
		{name: "enqueue rejection", endpoint: "/api/v1/queue/default/enqueue_batch", status: http.StatusUnprocessableEntity, wantCode: "invokeai_operation_failed"},
		{name: "inconclusive enqueue", source: "image", endpoint: "/api/v1/queue/default/enqueue_batch", status: http.StatusBadGateway, wantCode: "outcome_unknown"},
		{name: "malformed enqueue acceptance", endpoint: "/api/v1/queue/default/enqueue_batch", response: `{}`, wantCode: "outcome_unknown"},
		{name: "uploaded source enqueue rejection", source: "path", endpoint: "/api/v1/queue/default/enqueue_batch", status: http.StatusUnprocessableEntity, wantCode: "invokeai_operation_failed", wantUploaded: true},
		{name: "uploaded source inconclusive enqueue", source: "path", endpoint: "/api/v1/queue/default/enqueue_batch", status: http.StatusBadGateway, wantCode: "outcome_unknown", wantUploaded: true},
		{name: "uploaded source malformed acceptance", source: "path", endpoint: "/api/v1/queue/default/enqueue_batch", response: `{}`, wantCode: "outcome_unknown", wantUploaded: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			createGenerateProfile(t, `{"schema_version":1,"name":"preset","generate":{"components":{"vae":"absent-vae"}}}`)
			openAPI := sdxlOpenAPIFixture(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == test.endpoint {
					if test.status != 0 {
						w.WriteHeader(test.status)
					}
					_, _ = w.Write([]byte(test.response))
					return
				}
				if serveAnimaPreflight(w, r, "6.14.1", openAPI, sdxlInventory()) {
					return
				}
				switch r.URL.Path {
				case "/api/v1/images/i/source.png":
					_ = json.MarshalWrite(w, upscaleImage("source.png", 16, 8))
				case "/api/v1/images/upload":
					w.WriteHeader(http.StatusCreated)
					_ = json.MarshalWrite(w, upscaleImage("uploaded.png", 16, 8))
				default:
					t.Errorf("unexpected InvokeAI request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			args := []string{"--no-wait", "--profile", "preset", "--model", "sdxl-main", "--prompt", "test", "--seed", "42", "--url", server.URL}
			if test.source == "image" {
				args = append(args, "--image", "source.png")
			} else if test.source == "path" {
				args = append(args, "--image-path", sourcePNG(t, 16, 8))
			}
			code, envelope := runImg2Img(t, args...)
			if code != result.ExitStatus(test.wantCode) || envelope["ok"] != false || envelope["error"].(map[string]any)["code"] != test.wantCode {
				t.Fatalf("code=%d envelope=%#v", code, envelope)
			}
			assertGenerateProfileSkipWarning(t, envelope)
			if test.wantUploaded {
				details := envelope["error"].(map[string]any)["details"].(map[string]any)
				if details["source_uploaded"] != true || details["source_image"].(map[string]any)["image_name"] != "uploaded.png" {
					t.Fatalf("uploaded source details=%#v", details)
				}
			}
		})
	}
}

func TestGenerateProfileWarningsSurviveTooSmallPathSource(t *testing.T) {
	isolateUserConfigDir(t)
	createGenerateProfile(t, `{"schema_version":1,"name":"preset","generate":{"components":{"vae":"absent-vae"}}}`)
	server, requests, _ := img2imgServer(t, 7, 8, sdxlOpenAPIFixture(t), sdxlInventory())
	code, envelope := runImg2Img(t, "--no-wait", "--profile", "preset", "--model", "sdxl-main", "--prompt", "test", "--image-path", sourcePNG(t, 7, 8), "--seed", "42", "--url", server.URL)
	if code != result.ExitInvalidRequest || envelope["error"].(map[string]any)["code"] != "invalid_request" || slices.Contains(*requests, uploadRequest) || slices.Contains(*requests, enqueueRequest) {
		t.Fatalf("code=%d envelope=%#v requests=%q", code, envelope, *requests)
	}
	assertGenerateProfileSkipWarning(t, envelope)
}

func TestGraphOperationsOmitProfileWarningsForInvalidUploadedImageURL(t *testing.T) {
	for _, operation := range []string{"generate", "upscale"} {
		t.Run(operation, func(t *testing.T) {
			isolateUserConfigDir(t)
			createGenerateProfile(t, `{"schema_version":1,"name":"preset","`+operation+`":{"components":{"vae":"absent-vae"}}}`)
			server := newUpscaleUploadServer(t, upscaleUploadHandlers{upload: func(w http.ResponseWriter, _ *http.Request) {
				image := upscaleImage("uploaded.png", 16, 8)
				image["image_url"] = "http://%"
				w.WriteHeader(http.StatusCreated)
				_ = json.MarshalWrite(w, image)
			}})
			args := []string{operation, "--no-wait", "--profile", "preset", "--model", "sdxl-main", "--image-path", sourcePNG(t, 16, 8), "--seed", "42", "--url", server.URL, "--json"}
			if operation == "generate" {
				args = append(args, "--prompt", "test")
			} else {
				args = append(args, "--tile-controlnet", "tile")
			}
			var stdout, stderr bytes.Buffer
			code := cli.New(&stdout, &stderr).Run(t.Context(), args)
			var envelope result.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("invalid JSON: %v; stdout=%s", err, stdout.String())
			}
			if code != result.ExitInvokeAIFailure || stderr.Len() != 0 || envelope.Error == nil || envelope.Error.Code != "invalid_invokeai_response" || len(envelope.Warnings) != 0 || server.count(uploadRequest) != 1 || server.count(enqueueRequest) != 0 {
				t.Fatalf("code=%d envelope=%#v stderr=%q requests=%q", code, envelope, stderr.String(), server.sequence())
			}
			if envelope.Error.Details["source_uploaded"] != true || envelope.Error.Details["source_image"].(map[string]any)["image_name"] != "uploaded.png" {
				t.Fatalf("details=%#v", envelope.Error.Details)
			}
		})
	}
}

func TestGenerateWaitFailureOmitsProfileWarningsAfterAcceptedEnqueue(t *testing.T) {
	for _, source := range []string{"none", "path"} {
		t.Run(source, func(t *testing.T) {
			isolateUserConfigDir(t)
			createGenerateProfile(t, `{"schema_version":1,"name":"preset","generate":{"components":{"vae":"absent-vae"}}}`)
			server := newUpscaleUploadServer(t, upscaleUploadHandlers{
				inventory: sdxlInventory(),
				upload: func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusCreated)
					_ = json.MarshalWrite(w, upscaleImage("uploaded.png", 16, 8))
				},
				item: func(w http.ResponseWriter, _ *http.Request) {
					_ = json.MarshalWrite(w, map[string]any{"item_id": 19, "queue_id": "default", "batch_id": "upscale-batch", "status": "failed", "error_type": "ModelError", "error_message": "generation failed"})
				},
			})
			args := []string{"--profile", "preset", "--model", "sdxl-main", "--prompt", "test", "--seed", "42", "--url", server.URL}
			if source == "path" {
				args = append(args, "--image-path", sourcePNG(t, 16, 8))
			}
			code, envelope := runImg2Img(t, args...)
			if code != result.ExitInvokeAIFailure || envelope["error"].(map[string]any)["code"] != "invokeai_operation_failed" || !reflect.DeepEqual(envelope["warnings"], []any{}) || server.count(enqueueRequest) != 1 {
				t.Fatalf("code=%d envelope=%#v requests=%q", code, envelope, server.sequence())
			}
			details := envelope["error"].(map[string]any)["details"].(map[string]any)
			if details["queue_id"] != "default" || details["batch_id"] != "upscale-batch" || (source == "path" && details["source_uploaded"] != true) {
				t.Fatalf("details=%#v", details)
			}
		})
	}
}

func assertGenerateProfileSkipWarning(t *testing.T, envelope map[string]any) {
	t.Helper()
	want := []any{map[string]any{
		"code":    "profile_preference_skipped",
		"message": `profile "preset" vae preference could not be used; automatic component resolution continues`,
		"details": map[string]any{"profile": "preset", "component": "vae", "reason": "not_found"},
	}}
	if !reflect.DeepEqual(envelope["warnings"], want) {
		encoded, _ := json.Marshal(envelope["warnings"])
		t.Fatalf("warnings=%s, want=%#v", encoded, want)
	}
}
