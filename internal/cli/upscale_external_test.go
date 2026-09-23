package cli_test

import (
	"bytes"
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

type upscaleReceiptEnvelope struct {
	OK        bool   `json:"ok"`
	Operation string `json:"operation"`
	Data      struct {
		SubmittedRequest map[string]any `json:"submitted_request"`
		SourceImage      struct {
			ImageName string `json:"image_name"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"source_image"`
		SourceUploaded   bool `json:"source_uploaded"`
		ResolvedSettings struct {
			Scale         int               `json:"scale"`
			Creativity    int               `json:"creativity"`
			Structure     int               `json:"structure"`
			Steps         int               `json:"steps"`
			Scheduler     string            `json:"scheduler"`
			Guidance      float64           `json:"guidance"`
			TileSize      int               `json:"tile_size"`
			TileOverlap   int               `json:"tile_overlap"`
			OutputWidth   int               `json:"output_width"`
			OutputHeight  int               `json:"output_height"`
			ModelKey      string            `json:"model_key"`
			ComponentKeys map[string]string `json:"component_keys"`
			Seeds         []uint32          `json:"seeds"`
		} `json:"resolved_settings"`
		Queue struct {
			QueueID string `json:"queue_id"`
			BatchID string `json:"batch_id"`
			ItemIDs []int  `json:"item_ids"`
		} `json:"queue"`
		Outputs []struct {
			ItemID int    `json:"item_id"`
			Seed   uint32 `json:"seed"`
			Image  struct {
				ImageName string `json:"image_name"`
				Width     int    `json:"width"`
				Height    int    `json:"height"`
			} `json:"image"`
		} `json:"outputs"`
		Warnings []result.Warning `json:"warnings"`
	} `json:"data"`
	Error    *result.Error    `json:"error"`
	Warnings []result.Warning `json:"warnings"`
}

func upscaleInventory() []map[string]any {
	return []map[string]any{
		{"key": "sdxl-main", "hash": "main-hash", "name": "SDXL Main", "base": "sdxl", "type": "main", "variant": "normal"},
		{"key": "spandrel", "hash": "upscale-hash", "name": "RealESRGAN x4plus", "base": "any", "type": "spandrel_image_to_image"},
		{"key": "tile", "hash": "tile-hash", "name": "Tile", "base": "sdxl", "type": "controlnet"},
		{"key": "vae", "hash": "vae-hash", "name": "VAE", "base": "sdxl", "type": "vae"},
	}
}

func upscaleImage(name string, width, height int) map[string]any {
	return map[string]any{"image_name": name, "image_url": "/api/v1/images/i/" + name + "/full", "thumbnail_url": "/api/v1/images/i/" + name + "/thumbnail", "image_origin": "internal", "image_category": "general", "width": width, "height": height, "created_at": "2026-01-01", "updated_at": "2026-01-01", "is_intermediate": false, "starred": false, "has_workflow": false}
}

func runUpscale(t *testing.T, serverURL string, args ...string) (int, upscaleReceiptEnvelope) {
	t.Helper()
	isolateUserConfigDir(t)
	var stdout, stderr bytes.Buffer
	arguments := append(slices.Clone(args), "--url", serverURL, "--json")
	code := cli.New(&stdout, &stderr).Run(t.Context(), arguments)
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

func TestUpscaleDefaultsNoWaitAndRequestEquivalence(t *testing.T) {
	var enqueues atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), upscaleInventory()) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/images/i/source.png":
			_ = json.MarshalWrite(w, upscaleImage("source.png", 513, 513))
		case "/api/v1/queue/default/enqueue_batch":
			enqueues.Add(1)
			_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "batch": map[string]any{"batch_id": "upscale-batch"}, "item_ids": []int{19}, "enqueued": 1, "requested": 1})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	flags := []string{"upscale", "--no-wait", "--image", "source.png", "--model", "sdxl-main", "--tile-controlnet", "tile", "--seed", "42"}
	code, fromFlags := runUpscale(t, server.URL, flags...)
	if code != 0 || !fromFlags.OK || enqueues.Load() != 1 {
		t.Fatalf("flags: code=%d receipt=%#v enqueues=%d", code, fromFlags, enqueues.Load())
	}
	s := fromFlags.Data.ResolvedSettings
	if s.Scale != 4 || s.Creativity != 0 || s.Structure != 0 || s.Steps != 30 || s.Scheduler != "kdpm_2" || s.Guidance != 2 || s.TileSize != 1024 || s.TileOverlap != 128 || s.OutputWidth != 2048 || s.OutputHeight != 2048 || s.ModelKey != "sdxl-main" || !reflect.DeepEqual(s.ComponentKeys, map[string]string{"upscale_model": "spandrel", "tile_controlnet": "tile"}) || !slices.Equal(s.Seeds, []uint32{42}) || len(fromFlags.Data.Outputs) != 0 || len(fromFlags.Data.Warnings) != 0 || len(fromFlags.Warnings) != 0 || fromFlags.Data.SourceUploaded || fromFlags.Data.SourceImage.ImageName != "source.png" {
		t.Fatalf("default receipt = %#v", fromFlags.Data)
	}
	document := `{"schema_version":1,"source":{"type":"image","reference":"source.png"},"model":"sdxl-main","components":{"tile_controlnet":"tile"},"seed":42}`
	path := t.TempDir() + "/upscale.json"
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	code, fromDocument := runUpscale(t, server.URL, "upscale", "--no-wait", "--request", path)
	if code != 0 || !fromDocument.OK || enqueues.Load() != 2 || !reflect.DeepEqual(fromFlags.Data, fromDocument.Data) {
		t.Fatalf("flag/document mismatch: code=%d flags=%#v document=%#v", code, fromFlags.Data, fromDocument.Data)
	}
}

func TestUpscaleExplicitSettingsFlagAndDocumentEquivalence(t *testing.T) {
	var enqueues atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), upscaleInventory()) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/images/i/source.png":
			_ = json.MarshalWrite(w, upscaleImage("source.png", 513, 513))
		case "/api/v1/queue/default/enqueue_batch":
			enqueues.Add(1)
			_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "batch": map[string]any{"batch_id": "upscale-batch"}, "item_ids": []int{19}, "enqueued": 1, "requested": 1})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	code, fromFlags := runUpscale(t, server.URL, "upscale", "--no-wait", "--image", "source.png", "--model", "sdxl-main", "--prompt", "mountain", "--negative-prompt", "blur",
		"--scale", "2", "--creativity", "-2", "--structure", "3", "--steps", "5", "--scheduler", "euler", "--guidance", "4.5", "--seed", "42",
		"--tile-size", "512", "--tile-overlap", "16", "--board", "board-7", "--upscale-model", "spandrel", "--tile-controlnet", "tile", "--vae", "vae")
	if code != 0 || !fromFlags.OK {
		t.Fatalf("flags: code=%d %#v", code, fromFlags)
	}
	document := `{"schema_version":1,"source":{"type":"image","reference":"source.png"},"model":"sdxl-main","positive_prompt":"mountain","negative_prompt":"blur","scale":2,"creativity":-2,"structure":3,"steps":5,"scheduler":"euler","guidance":4.5,"seed":42,"tile_size":512,"tile_overlap":16,"board_id":"board-7","components":{"upscale_model":"spandrel","tile_controlnet":"tile","vae":"vae"}}`
	path := t.TempDir() + "/request.json"
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	code, fromDocument := runUpscale(t, server.URL, "upscale", "--no-wait", "--request", path)
	if code != 0 || !fromDocument.OK || enqueues.Load() != 2 || !reflect.DeepEqual(fromFlags.Data, fromDocument.Data) {
		t.Fatalf("explicit mismatch: code=%d flags=%#v document=%#v enqueues=%d", code, fromFlags.Data, fromDocument.Data, enqueues.Load())
	}
	settings := fromFlags.Data.ResolvedSettings
	if settings.Scale != 2 || settings.Creativity != -2 || settings.Structure != 3 || settings.Steps != 5 || settings.Scheduler != "euler" || settings.Guidance != 4.5 || settings.TileSize != 512 || settings.TileOverlap != 16 || settings.OutputWidth != 1024 || settings.OutputHeight != 1024 || settings.ComponentKeys["vae"] != "vae" {
		t.Fatalf("explicit settings = %#v", settings)
	}
}

func TestUpscaleRejectsInvalidLocalValuesBeforeNetwork(t *testing.T) {
	base := `{"schema_version":1,"source":{"type":"image","reference":"source.png"},"model":"sdxl-main","components":{"tile_controlnet":"tile"}`
	cases := []struct{ name, field string }{
		{"scale one", `,"scale":1`}, {"scale sixteen", `,"scale":16`}, {"creativity low", `,"creativity":-11`}, {"creativity high", `,"creativity":11`},
		{"creativity decimal", `,"creativity":0.5`}, {"structure low", `,"structure":-11`}, {"structure high", `,"structure":11`}, {"structure decimal", `,"structure":1.5`},
		{"steps zero", `,"steps":0`}, {"scheduler unknown", `,"scheduler":"other"`}, {"guidance low", `,"guidance":0.9`},
		{"tile size small", `,"tile_size":448`}, {"tile size large", `,"tile_size":1600`}, {"tile size unaligned", `,"tile_size":513`},
		{"overlap small", `,"tile_overlap":8`}, {"overlap large", `,"tile_overlap":520`}, {"overlap unaligned", `,"tile_overlap":17`},
		{"overlap at tile", `,"tile_size":512,"tile_overlap":512`}, {"unknown", `,"surprise":true`},
		{"null scale", `,"scale":null`}, {"null seed", `,"seed":null`}, {"null prompt", `,"positive_prompt":null`},
		{"null tile selector", `,"components":{"tile_controlnet":null}`},
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "unexpected network request", 500)
	}))
	defer server.Close()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := t.TempDir() + "/request.json"
			if err := os.WriteFile(path, []byte(base+tc.field+`}`), 0o600); err != nil {
				t.Fatal(err)
			}
			code, envelope := runUpscale(t, server.URL, "upscale", "--request", path)
			if code != result.ExitInvalidRequest || envelope.Error == nil || envelope.Error.Code != result.CodeInvalidRequest || calls.Load() != 0 {
				t.Fatalf("code=%d envelope=%#v network calls=%d", code, envelope, calls.Load())
			}
		})
	}
	code, envelope := runUpscale(t, server.URL, "upscale", "--image", "source.png", "--model", "sdxl-main", "--tile-controlnet", "", "--no-wait")
	if code != result.ExitInvalidRequest || envelope.Error == nil || envelope.Error.Code != result.CodeInvalidRequest || calls.Load() != 0 {
		t.Fatalf("empty selector: code=%d envelope=%#v network calls=%d", code, envelope, calls.Load())
	}
	code, envelope = runUpscale(t, server.URL, "upscale", "--image", "source.png", "--request", "-")
	if code != result.ExitInvalidRequest || envelope.Error == nil || envelope.Error.Code != result.CodeInvalidRequest || calls.Load() != 0 {
		t.Fatalf("mixed flags: code=%d envelope=%#v network calls=%d", code, envelope, calls.Load())
	}
	for _, guidance := range []string{"NaN", "+Inf"} {
		code, envelope = runUpscale(t, server.URL, "upscale", "--image", "source.png", "--model", "sdxl-main", "--guidance", guidance)
		if code != result.ExitInvalidRequest || envelope.Error == nil || envelope.Error.Code != result.CodeInvalidRequest || calls.Load() != 0 {
			t.Fatalf("non-finite guidance %q: code=%d envelope=%#v network calls=%d", guidance, code, envelope, calls.Load())
		}
	}
	code, envelope = runUpscale(t, server.URL, "upscale", "--image-path", "/tmp/source.png", "--model", "sdxl-main")
	if code != result.ExitUnsupportedCapability || envelope.Error == nil || envelope.Error.Code != result.CodeUnsupportedCapability || calls.Load() != 0 {
		t.Fatalf("path source: code=%d envelope=%#v network calls=%d", code, envelope, calls.Load())
	}
}

func TestUpscaleWaitVerifiesOutputDimensionsAndSeed(t *testing.T) {
	for _, tc := range []struct {
		name        string
		outputWidth int
		wantCode    string
	}{{"matching", 1024, ""}, {"scale not applied", 512, result.CodeInvokeAIOperationFailed}} {
		t.Run(tc.name, func(t *testing.T) {
			var enqueues atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), upscaleInventory()) {
					return
				}
				switch r.URL.Path {
				case "/api/v1/images/i/source.png":
					_ = json.MarshalWrite(w, upscaleImage("source.png", 513, 513))
				case "/api/v1/queue/default/enqueue_batch":
					enqueues.Add(1)
					_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "batch": map[string]any{"batch_id": "upscale-batch"}, "item_ids": []int{19}, "enqueued": 1, "requested": 1})
				case "/api/v1/queue/default/i/19":
					_ = json.MarshalWrite(w, completedBatchItem(19, "upscale-batch", 42, "output.png"))
				case "/api/v1/images/i/output.png":
					_ = json.MarshalWrite(w, upscaleImage("output.png", tc.outputWidth, 1024))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			code, envelope := runUpscale(t, server.URL, "upscale", "--image", "source.png", "--model", "sdxl-main", "--tile-controlnet", "tile", "--seed", "42", "--scale", "2")
			if enqueues.Load() != 1 {
				t.Fatalf("enqueue count=%d", enqueues.Load())
			}
			if tc.wantCode == "" {
				if code != 0 || !envelope.OK || len(envelope.Data.Outputs) != 1 || envelope.Data.Outputs[0].Image.Width != 1024 || envelope.Data.Outputs[0].Seed != 42 || envelope.Data.Outputs[0].ItemID != 19 {
					t.Fatalf("completed receipt: code=%d %#v", code, envelope)
				}
			} else {
				if code != 6 || envelope.OK || envelope.Error == nil || envelope.Error.Code != tc.wantCode || envelope.Error.Details["reason"] != "scale_not_applied" ||
					envelope.Error.Details["expected_width"] != float64(1024) || envelope.Error.Details["expected_height"] != float64(1024) ||
					envelope.Error.Details["actual_width"] != float64(512) || envelope.Error.Details["actual_height"] != float64(1024) ||
					envelope.Error.Details["queue_id"] != "default" || envelope.Error.Details["batch_id"] != "upscale-batch" ||
					envelope.Error.Details["item_id"] != float64(19) || envelope.Error.Details["seed"] != float64(42) ||
					envelope.Error.Details["output_image"].(map[string]any)["image_name"] != "output.png" ||
					envelope.Error.Details["source_image"].(map[string]any)["image_name"] != "source.png" {
					t.Fatalf("dimension failure: code=%d %#v", code, envelope)
				}
			}
		})
	}
}

func TestUpscaleScaleEightReportsAlignedExpectedDimensionsWithoutWaiting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), upscaleInventory()) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/images/i/source.png":
			_ = json.MarshalWrite(w, upscaleImage("source.png", 512, 512))
		case "/api/v1/queue/default/enqueue_batch":
			_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "batch": map[string]any{"batch_id": "upscale-batch"}, "item_ids": []int{19}, "enqueued": 1, "requested": 1})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	code, envelope := runUpscale(t, server.URL, "upscale", "--no-wait", "--image", "source.png", "--model", "sdxl-main", "--tile-controlnet", "tile", "--scale", "8", "--seed", "42")
	if code != 0 || !envelope.OK || envelope.Data.ResolvedSettings.OutputWidth != 4096 || envelope.Data.ResolvedSettings.OutputHeight != 4096 || len(envelope.Data.Outputs) != 0 {
		t.Fatalf("scale-eight receipt: code=%d %#v", code, envelope)
	}
}

func TestUpscaleRejectsContradictorySourceImageBeforeEnqueue(t *testing.T) {
	var enqueues atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), upscaleInventory()) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/images/i/source.png":
			_ = json.MarshalWrite(w, upscaleImage("different.png", 512, 512))
		case "/api/v1/queue/default/enqueue_batch":
			enqueues.Add(1)
			http.Error(w, "unexpected enqueue", 500)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	code, envelope := runUpscale(t, server.URL, "upscale", "--no-wait", "--image", "source.png", "--model", "sdxl-main", "--tile-controlnet", "tile", "--seed", "42")
	if code != result.ExitInvokeAIFailure || envelope.Error == nil || envelope.Error.Code != result.CodeInvalidInvokeAIResponse || enqueues.Load() != 0 {
		t.Fatalf("code=%d envelope=%#v enqueues=%d", code, envelope, enqueues.Load())
	}
}

func TestUpscaleResolutionErrorsNeverEnqueue(t *testing.T) {
	tests := []struct {
		name           string
		inventory      []map[string]any
		model          string
		tileFlag       bool
		sourceExists   bool
		wantCode       string
		wantKind       string
		wantCandidates int
	}{
		{"unregistered main family", func() []map[string]any { m := upscaleInventory(); m[0]["base"] = "sd-2"; return m }(), "sdxl-main", true, true, result.CodeUnsupportedCapability, "", 0},
		{"non normal main", func() []map[string]any { m := upscaleInventory(); m[0]["variant"] = "inpaint"; return m }(), "sdxl-main", true, true, result.CodeUnsupportedCapability, "", 0},
		{"shared main name", append(upscaleInventory(), map[string]any{"key": "another-main", "hash": "another-hash", "name": "SDXL Main", "base": "sdxl", "type": "main", "variant": "normal"}), "SDXL Main", true, true, result.CodeSelectionRequired, "main_model", 2},
		{"missing Spandrel", func() []map[string]any {
			m := upscaleInventory()
			return slices.DeleteFunc(m, func(x map[string]any) bool { return x["key"] == "spandrel" })
		}(), "sdxl-main", true, true, result.CodeMissingComponent, "", 0},
		{"ambiguous Spandrel", append(upscaleInventory(), map[string]any{"key": "spandrel-2", "hash": "second", "name": "Other Upscaler", "base": "any", "type": "spandrel_image_to_image"}), "sdxl-main", true, true, result.CodeSelectionRequired, "upscale_model", 2},
		{"one Tile requires choice", upscaleInventory(), "sdxl-main", false, true, result.CodeSelectionRequired, "tile_controlnet", 1},
		{"several Tile require choice", append(upscaleInventory(), map[string]any{"key": "tile-2", "hash": "tile-2-hash", "name": "Other ControlNet", "base": "sdxl", "type": "controlnet"}), "sdxl-main", false, true, result.CodeSelectionRequired, "tile_controlnet", 2},
		{"missing Tile", func() []map[string]any {
			m := upscaleInventory()
			return slices.DeleteFunc(m, func(x map[string]any) bool { return x["key"] == "tile" })
		}(), "sdxl-main", false, true, result.CodeMissingComponent, "", 0},
		{"base mismatched Tile", func() []map[string]any { m := upscaleInventory(); m[2]["base"] = "sd-1"; return m }(), "sdxl-main", true, true, result.CodeUnsupportedCapability, "", 0},
		{"missing source image", upscaleInventory(), "sdxl-main", true, false, result.CodeNotFound, "", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var enqueues atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), tc.inventory) {
					return
				}
				switch r.URL.Path {
				case "/api/v1/images/i/source.png":
					if tc.sourceExists {
						_ = json.MarshalWrite(w, upscaleImage("source.png", 512, 512))
					} else {
						http.NotFound(w, r)
					}
				case "/api/v1/queue/default/enqueue_batch":
					enqueues.Add(1)
					http.Error(w, "unexpected enqueue", 500)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			args := []string{"upscale", "--no-wait", "--image", "source.png", "--model", tc.model, "--seed", "42"}
			if tc.tileFlag {
				args = append(args, "--tile-controlnet", "tile")
			}
			code, envelope := runUpscale(t, server.URL, args...)
			if code != result.ExitStatus(tc.wantCode) || envelope.Error == nil || envelope.Error.Code != tc.wantCode || enqueues.Load() != 0 {
				t.Fatalf("code=%d envelope=%#v enqueues=%d", code, envelope, enqueues.Load())
			}
			if tc.wantKind != "" {
				candidates, ok := envelope.Error.Details["candidates"].([]any)
				if envelope.Error.Details["kind"] != tc.wantKind || !ok || len(candidates) != tc.wantCandidates {
					t.Fatalf("selection details = %#v", envelope.Error.Details)
				}
			}
			if tc.wantCode == result.CodeMissingComponent && !strings.Contains(envelope.Error.Details["installation_guidance"].(string), map[bool]string{true: "RealESRGAN_x4plus", false: "xinsir/controlNet-tile-sdxl-1.0"}[tc.name == "missing Spandrel"]) {
				t.Fatalf("missing guidance = %#v", envelope.Error.Details)
			}
		})
	}
}

func TestUpscaleInconclusiveEnqueueIsNotRetried(t *testing.T) {
	var enqueues atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), upscaleInventory()) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/images/i/source.png":
			_ = json.MarshalWrite(w, upscaleImage("source.png", 512, 512))
		case "/api/v1/queue/default/enqueue_batch":
			enqueues.Add(1)
			panic(http.ErrAbortHandler)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	code, envelope := runUpscale(t, server.URL, "upscale", "--no-wait", "--image", "source.png", "--model", "sdxl-main", "--tile-controlnet", "tile", "--seed", "42")
	if code != result.ExitInvokeAIFailure || envelope.Error == nil || envelope.Error.Code != result.CodeOutcomeUnknown || enqueues.Load() != 1 {
		t.Fatalf("code=%d envelope=%#v enqueues=%d", code, envelope, enqueues.Load())
	}
}

func TestUpscaleWaitReportsFailedItemAndTimeoutWithQueueIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, status, expected string
		timeout                string
	}{
		{"failed", "failed", result.CodeInvokeAIOperationFailed, ""},
		{"timeout", "in_progress", result.CodeWaitTimeout, "1ms"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var enqueues atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), upscaleInventory()) {
					return
				}
				switch r.URL.Path {
				case "/api/v1/images/i/source.png":
					_ = json.MarshalWrite(w, upscaleImage("source.png", 512, 512))
				case "/api/v1/queue/default/enqueue_batch":
					enqueues.Add(1)
					_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "batch": map[string]any{"batch_id": "upscale-batch"}, "item_ids": []int{19}, "enqueued": 1, "requested": 1})
				case "/api/v1/queue/default/i/19":
					_ = json.MarshalWrite(w, map[string]any{"item_id": 19, "queue_id": "default", "batch_id": "upscale-batch", "session_id": "session-19", "status": tc.status, "priority": 0, "created_at": "2026-01-01", "updated_at": "2026-01-01", "error_type": "ModelError", "error_message": "upscale failed"})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			args := []string{"upscale", "--image", "source.png", "--model", "sdxl-main", "--tile-controlnet", "tile", "--seed", "42"}
			if tc.timeout != "" {
				args = append(args, "--timeout", tc.timeout)
			}
			code, envelope := runUpscale(t, server.URL, args...)
			if code != result.ExitInvokeAIFailure || envelope.Error == nil || envelope.Error.Code != tc.expected || envelope.Error.Details["queue_id"] != "default" || envelope.Error.Details["batch_id"] != "upscale-batch" || enqueues.Load() != 1 {
				t.Fatalf("code=%d envelope=%#v enqueues=%d", code, envelope, enqueues.Load())
			}
		})
	}
}
