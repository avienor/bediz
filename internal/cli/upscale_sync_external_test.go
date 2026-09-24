package cli_test

import (
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/result"
)

var upscaleNotRestored = []any{
	"source_image", "upscale_model", "scale", "creativity", "structure", "tile_controlnet",
	"tile_size", "tile_overlap", "scheduler", "guidance", "vae", "board_id",
}

func assertUpscaleSyncWarning(t *testing.T, envelope upscaleReceiptEnvelope, code string) {
	t.Helper()
	if len(envelope.Warnings) != 1 || !reflect.DeepEqual(envelope.Data.Warnings, envelope.Warnings) || envelope.Warnings[0].Code != code {
		t.Fatalf("receipt warnings = %#v, envelope warnings = %#v, want one %s", envelope.Data.Warnings, envelope.Warnings, code)
	}
	if code == "ui_sync_partial" && !reflect.DeepEqual(envelope.Warnings[0].Details["not_restored"], upscaleNotRestored) {
		t.Fatalf("not_restored = %#v, want %#v", envelope.Warnings[0].Details["not_restored"], upscaleNotRestored)
	}
}

func TestUpscaleRecallsSharedGenerateControlsAfterAcceptedEnqueue(t *testing.T) {
	for _, test := range []struct {
		name      string
		model     string
		tile      string
		wantModel string
	}{
		{name: "SDXL", model: "sdxl-main", tile: "tile", wantModel: "SDXL Main"},
		{name: "SD1.5", model: "sd1-main", tile: "sd1-tile", wantModel: "Dreamshaper 8"},
	} {
		t.Run(test.name, func(t *testing.T) {
			mutations := []string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), sd1UpscaleInventory()) {
					return
				}
				switch r.URL.Path {
				case "/api/v1/images/i/source.png":
					_ = json.MarshalWrite(w, upscaleImage("source.png", 512, 512))
					return
				}
				mutations = append(mutations, r.Method+" "+r.URL.Path)
				switch r.URL.Path {
				case "/api/v1/queue/default/enqueue_batch":
					_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "batch": map[string]any{"batch_id": "upscale-batch"}, "item_ids": []int{19}, "enqueued": 1, "requested": 1})
				case "/api/v1/recall/default":
					var patch map[string]any
					if err := json.UnmarshalRead(r.Body, &patch); err != nil {
						t.Error(err)
					}
					want := map[string]any{
						"model": test.wantModel, "positive_prompt": "mountain", "negative_prompt": "blur",
						"steps": float64(5), "seed": float64(42),
					}
					if !reflect.DeepEqual(patch, want) {
						t.Errorf("Recall patch = %#v, want %#v", patch, want)
					}
					_, _ = w.Write([]byte(`{"status":"success"}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			code, envelope := runUpscale(t, server.URL, "upscale", "--no-wait", "--image", "source.png", "--model", test.model,
				"--tile-controlnet", test.tile, "--prompt", "mountain", "--negative-prompt", "blur", "--steps", "5", "--seed", "42",
				"--scale", "2", "--guidance", "4.5", "--board", "board-7")
			if code != result.ExitSuccess || !envelope.OK ||
				!slices.Equal(mutations, []string{"POST /api/v1/queue/default/enqueue_batch", "POST /api/v1/recall/default"}) {
				t.Fatalf("code=%d mutations=%v envelope=%#v", code, mutations, envelope)
			}
			assertUpscaleSyncWarning(t, envelope, "ui_sync_partial")
		})
	}
}

func TestUpscaleRecallFailurePreservesSuccessfulReceipt(t *testing.T) {
	for _, test := range []struct {
		name           string
		noWait         bool
		ambiguousModel bool
		lostResponse   bool
		missingRecall  bool
		wantRecall     int32
	}{
		{name: "no wait Recall rejected", noWait: true, wantRecall: 1},
		{name: "completed Recall rejected", wantRecall: 1},
		{name: "no wait Recall response lost", noWait: true, lostResponse: true, wantRecall: 1},
		{name: "completed Recall response lost", lostResponse: true, wantRecall: 1},
		{name: "display name collision", noWait: true, ambiguousModel: true},
		{name: "missing Recall endpoint", noWait: true, missingRecall: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			models := upscaleInventory()
			if test.ambiguousModel {
				models = append(models, map[string]any{"key": "other-main", "hash": "other-hash", "name": "SDXL Main", "base": "sd-1", "type": "main", "variant": "normal"})
			}
			openAPI := sdxlOpenAPIFixture(t)
			if test.missingRecall {
				delete(openAPI["paths"].(map[string]any), "/api/v1/recall/{queue_id}")
			}
			var enqueues, recalls, polls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveAnimaPreflight(w, r, "6.14.1", openAPI, models) {
					return
				}
				switch r.URL.Path {
				case "/api/v1/images/i/source.png":
					_ = json.MarshalWrite(w, upscaleImage("source.png", 512, 512))
				case "/api/v1/queue/default/enqueue_batch":
					enqueues.Add(1)
					_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "batch": map[string]any{"batch_id": "upscale-batch"}, "item_ids": []int{19}, "enqueued": 1, "requested": 1})
				case "/api/v1/recall/default":
					if enqueues.Load() != 1 {
						t.Error("Recall arrived before conclusive enqueue")
					}
					recalls.Add(1)
					if test.lostResponse {
						connection, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						_ = connection.Close()
						return
					}
					http.Error(w, "Recall failed", http.StatusInternalServerError)
				case "/api/v1/queue/default/i/19":
					polls.Add(1)
					_ = json.MarshalWrite(w, map[string]any{
						"item_id": 19, "queue_id": "default", "batch_id": "upscale-batch", "session_id": "session-19",
						"status": "completed", "priority": 0,
						"created_at": "2026-01-01 00:00:00.000", "updated_at": "2026-01-01 00:00:05.000",
						"field_values": []map[string]any{{"node_path": "seed", "field_name": "value", "value": 42}},
						"session":      completedItemResults("upscaled.png"),
					})
				case "/api/v1/images/i/upscaled.png":
					_ = json.MarshalWrite(w, upscaleImage("upscaled.png", 2048, 2048))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			args := []string{"upscale", "--image", "source.png", "--model", "sdxl-main", "--tile-controlnet", "tile", "--seed", "42"}
			if test.noWait {
				args = append(args, "--no-wait")
			}
			code, envelope := runUpscale(t, server.URL, args...)
			if code != result.ExitSuccess || !envelope.OK || enqueues.Load() != 1 || recalls.Load() != test.wantRecall ||
				envelope.Data.Queue.BatchID != "upscale-batch" || !slices.Equal(envelope.Data.ResolvedSettings.Seeds, []uint32{42}) {
				t.Fatalf("code=%d enqueues=%d recalls=%d envelope=%#v", code, enqueues.Load(), recalls.Load(), envelope)
			}
			assertUpscaleSyncWarning(t, envelope, "ui_sync_failed")
			if test.noWait && (polls.Load() != 0 || len(envelope.Data.Outputs) != 0) {
				t.Fatalf("no-wait polls=%d outputs=%#v", polls.Load(), envelope.Data.Outputs)
			}
			if !test.noWait && (polls.Load() != 1 || len(envelope.Data.Outputs) != 1 || envelope.Data.Outputs[0].Image.ImageName != "upscaled.png") {
				t.Fatalf("completed polls=%d outputs=%#v", polls.Load(), envelope.Data.Outputs)
			}
		})
	}
}

func TestUpscaleDoesNotRecallAfterFailedOrInconclusiveEnqueue(t *testing.T) {
	for _, test := range []struct {
		name     string
		response func(http.ResponseWriter)
		wantCode string
	}{
		{name: "rejected", response: func(w http.ResponseWriter) { http.Error(w, "rejected", http.StatusUnprocessableEntity) }, wantCode: result.CodeInvokeAIOperationFailed},
		{name: "incomplete", response: func(w http.ResponseWriter) {
			_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "batch": map[string]any{"batch_id": "upscale-batch"}, "item_ids": []int{}, "enqueued": 0, "requested": 1})
		}, wantCode: result.CodeOutcomeUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			var enqueues, recalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), upscaleInventory()) {
					return
				}
				switch r.URL.Path {
				case "/api/v1/images/i/source.png":
					_ = json.MarshalWrite(w, upscaleImage("source.png", 512, 512))
				case "/api/v1/queue/default/enqueue_batch":
					enqueues.Add(1)
					test.response(w)
				case "/api/v1/recall/default":
					recalls.Add(1)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			code, envelope := runUpscale(t, server.URL, "upscale", "--no-wait", "--image", "source.png", "--model", "sdxl-main", "--tile-controlnet", "tile", "--seed", "42")
			if code == result.ExitSuccess || envelope.OK || envelope.Error == nil || envelope.Error.Code != test.wantCode || enqueues.Load() != 1 || recalls.Load() != 0 {
				t.Fatalf("code=%d enqueues=%d recalls=%d envelope=%#v", code, enqueues.Load(), recalls.Load(), envelope)
			}
		})
	}
}
