package cli_test

import (
	"bytes"
	json "encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

func fluxCLIInventory() []map[string]any {
	return []map[string]any{
		{"key": "flux-dev", "hash": "dev-hash", "name": "FLUX dev", "base": "flux", "type": "main", "format": "bnb_quantized_nf4b", "variant": "dev"},
		{"key": "flux-schnell", "hash": "schnell-hash", "name": "FLUX schnell", "base": "flux", "type": "main", "format": "bnb_quantized_nf4b", "variant": "schnell"},
		{"key": "flux-vae", "hash": "vae-hash", "name": "FLUX VAE", "base": "flux", "type": "vae"},
		{"key": "flux-t5", "hash": "t5-hash", "name": "T5", "base": "any", "type": "t5_encoder"},
		{"key": "flux-clip", "hash": "clip-hash", "name": "CLIP", "base": "any", "type": "clip_embed"},
	}
}

func TestGenerateFLUXFlagsAndDocumentMatchWithVariantReceipts(t *testing.T) {
	for _, tc := range []struct {
		model    string
		steps    int
		guidance bool
		fields   []string
	}{
		{"flux-dev", 30, true, []string{"scheduler", "guidance", "vae", "t5_encoder", "clip_embed", "output_count", "board_id"}},
		{"flux-schnell", 4, false, []string{"scheduler", "vae", "t5_encoder", "clip_embed", "output_count", "board_id"}},
	} {
		t.Run(tc.model, func(t *testing.T) {
			isolateUserConfigDir(t)
			var enqueues, recalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), fluxCLIInventory()) {
					return
				}
				switch r.URL.Path {
				case "/api/v1/queue/default/enqueue_batch":
					enqueues.Add(1)
					var payload struct {
						Batch struct {
							Graph struct {
								Nodes map[string]map[string]any `json:"nodes"`
							} `json:"graph"`
							Data [][]struct {
								Items []uint32 `json:"items"`
							} `json:"data"`
						} `json:"batch"`
					}
					if err := json.UnmarshalRead(r.Body, &payload); err != nil {
						t.Error(err)
					}
					if payload.Batch.Graph.Nodes["model_loader"]["type"] != "flux_model_loader" || !slices.Equal(payload.Batch.Data[0][0].Items, []uint32{43, 42}) {
						t.Errorf("batch = %#v", payload.Batch)
					}
					_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "enqueued": 2, "requested": 2, "item_ids": []int{23, 22}, "batch": map[string]any{"batch_id": "flux-batch"}})
				case "/api/v1/recall/default":
					recalls.Add(1)
					var patch map[string]any
					if err := json.UnmarshalRead(r.Body, &patch); err != nil {
						t.Error(err)
					}
					if patch["seed"] != float64(42) || patch["model"] == nil || patch["width"] != float64(1024) {
						t.Errorf("Recall patch = %#v", patch)
					}
					_, _ = w.Write([]byte(`{"status":"success"}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			var results []map[string]any
			for _, route := range []string{"flags", "document"} {
				var stdout, stderr bytes.Buffer
				var app *cli.CLI
				args := []string{"generate", "--no-wait", "--url", server.URL, "--json"}
				if route == "flags" {
					app = cli.New(&stdout, &stderr)
					args = append(args, "--model", tc.model, "--prompt", "a lighthouse", "--seed", "42", "--output-count", "2", "--vae", "flux-vae", "--t5-encoder", "flux-t5", "--clip-embed", "flux-clip")
				} else {
					document := `{"schema_version":1,"model":"` + tc.model + `","positive_prompt":"a lighthouse","seed":42,"output_count":2,"components":{"vae":"flux-vae","t5_encoder":"flux-t5","clip_embed":"flux-clip"}}`
					app = cli.NewWithIO(strings.NewReader(document), &stdout, &stderr)
					args = append(args, "--request", "-")
				}
				code := app.Run(t.Context(), args)
				var envelope map[string]any
				if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				if code != 0 || stderr.Len() != 0 || envelope["ok"] != true {
					t.Fatalf("route=%s code=%d stderr=%s envelope=%#v", route, code, stderr.String(), envelope)
				}
				data := envelope["data"].(map[string]any)
				settings := data["resolved_settings"].(map[string]any)
				_, hasGuidance := settings["guidance"]
				if settings["steps"] != float64(tc.steps) || hasGuidance != tc.guidance {
					t.Errorf("settings = %#v", settings)
				}
				warnings := envelope["warnings"].([]any)
				if len(warnings) != 1 || warnings[0].(map[string]any)["code"] != "ui_sync_partial" {
					t.Errorf("warnings = %#v", warnings)
				} else {
					got := warnings[0].(map[string]any)["details"].(map[string]any)["not_restored"].([]any)
					want := make([]any, len(tc.fields))
					for i, field := range tc.fields {
						want[i] = field
					}
					if !reflect.DeepEqual(got, want) {
						t.Errorf("not_restored = %#v", got)
					}
				}
				results = append(results, settings)
			}
			if !reflect.DeepEqual(results[0], results[1]) || enqueues.Load() != 2 || recalls.Load() != 2 {
				t.Fatalf("settings=%#v enqueues=%d recalls=%d", results, enqueues.Load(), recalls.Load())
			}
		})
	}
}

func TestGenerateFLUXRejectsInvalidAndUnsupportedBeforeMutation(t *testing.T) {
	for _, tc := range []struct {
		name, document, code string
		model                map[string]any
	}{
		{"schnell guidance", `{"schema_version":1,"model":"flux-schnell","positive_prompt":"test","guidance":4}`, "invalid_request", nil},
		{"dev negative", `{"schema_version":1,"model":"flux-dev","positive_prompt":"test","negative_prompt":"bad"}`, "invalid_request", nil},
		{"qwen3", `{"schema_version":1,"model":"flux-dev","positive_prompt":"test","components":{"qwen3_encoder":"encoder"}}`, "invalid_request", nil},
		{"unaligned", `{"schema_version":1,"model":"flux-dev","positive_prompt":"test","width":1000,"height":1024}`, "invalid_request", nil},
		{"diffusers", `{"schema_version":1,"model":"flux-diffusers","positive_prompt":"test"}`, "unsupported_capability", map[string]any{"key": "flux-diffusers", "hash": "diffusers-hash", "name": "FLUX Diffusers", "base": "flux", "type": "main", "format": "diffusers", "variant": "dev"}},
		{"unknown format", `{"schema_version":1,"model":"flux-unknown","positive_prompt":"test"}`, "unsupported_capability", map[string]any{"key": "flux-unknown", "hash": "unknown-hash", "name": "FLUX Unknown", "base": "flux", "type": "main", "format": "unknown", "variant": "schnell"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			var mutations atomic.Int32
			models := fluxCLIInventory()
			if tc.model != nil {
				models = append(models, tc.model)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), models) {
					return
				}
				mutations.Add(1)
				http.NotFound(w, r)
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			code := cli.NewWithIO(strings.NewReader(tc.document), &stdout, &stderr).Run(t.Context(), []string{"generate", "--no-wait", "--request", "-", "--url", server.URL, "--json"})
			var envelope result.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if code != result.ExitStatus(tc.code) || envelope.Error == nil || envelope.Error.Code != tc.code || mutations.Load() != 0 || stderr.Len() != 0 {
				t.Fatalf("code=%d envelope=%#v mutations=%d", code, envelope, mutations.Load())
			}
		})
	}
}

func TestRecallFLUXAcceptsAlignedAndRejectsUnsupportedModels(t *testing.T) {
	for _, tc := range []struct {
		name     string
		model    map[string]any
		size     int
		wantCode string
	}{
		{"dev", fluxCLIInventory()[0], 64, ""},
		{"schnell", fluxCLIInventory()[1], 80, ""},
		{"unaligned", fluxCLIInventory()[0], 72, "invalid_request"},
		{"too small", fluxCLIInventory()[0], 48, "invalid_request"},
		{"fill", map[string]any{"key": "flux-fill", "hash": "fill-hash", "name": "FLUX fill", "base": "flux", "type": "main", "format": "checkpoint", "variant": "dev_fill"}, 64, "unsupported_capability"},
		{"SDNQ", map[string]any{"key": "flux-sdnq", "hash": "sdnq-hash", "name": "FLUX SDNQ", "base": "flux", "type": "main", "format": "sdnq_quantized", "variant": "dev"}, 64, "unsupported_capability"},
		{"diffusers", map[string]any{"key": "flux-diffusers", "hash": "diffusers-hash", "name": "FLUX Diffusers", "base": "flux", "type": "main", "format": "diffusers", "variant": "dev"}, 64, "unsupported_capability"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			var posts atomic.Int32
			models := append(fluxCLIInventory(), tc.model)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), models) {
					return
				}
				if r.URL.Path == "/api/v1/recall/default" {
					posts.Add(1)
					_, _ = w.Write([]byte(`{"status":"success"}`))
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			code := cli.New(&stdout, &stderr).Run(t.Context(), []string{"recall", "--model", tc.model["key"].(string), "--width", fmt.Sprint(tc.size), "--height", fmt.Sprint(tc.size), "--url", server.URL, "--json"})
			var envelope result.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if tc.wantCode == "" {
				if code != 0 || !envelope.OK || posts.Load() != 1 {
					t.Fatalf("code=%d envelope=%#v posts=%d", code, envelope, posts.Load())
				}
			} else if code != result.ExitStatus(tc.wantCode) || envelope.Error == nil || envelope.Error.Code != tc.wantCode || posts.Load() != 0 {
				t.Fatalf("code=%d envelope=%#v posts=%d", code, envelope, posts.Load())
			}
		})
	}
}

func TestGenerateFLUXRecallFailureKeepsReceiptAndUnknownEnqueueIsNotRetried(t *testing.T) {
	for _, tc := range []struct {
		name         string
		inconclusive bool
		wantCode     string
	}{
		{"Recall failure", false, ""}, {"unknown enqueue", true, "outcome_unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			var enqueues, recalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), fluxCLIInventory()) {
					return
				}
				switch r.URL.Path {
				case "/api/v1/queue/default/enqueue_batch":
					enqueues.Add(1)
					if tc.inconclusive {
						conn, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						_ = conn.Close()
						return
					}
					_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "enqueued": 1, "requested": 1, "item_ids": []int{17}, "batch": map[string]any{"batch_id": "flux-batch"}})
				case "/api/v1/recall/default":
					recalls.Add(1)
					http.Error(w, "Recall failed", http.StatusInternalServerError)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			code := cli.New(&stdout, &stderr).Run(t.Context(), []string{"generate", "--no-wait", "--model", "flux-dev", "--prompt", "test", "--seed", "1", "--url", server.URL, "--json"})
			var envelope result.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if tc.inconclusive {
				if code != result.ExitStatus(tc.wantCode) || envelope.Error == nil || envelope.Error.Code != tc.wantCode || recalls.Load() != 0 {
					t.Fatalf("code=%d envelope=%#v recalls=%d", code, envelope, recalls.Load())
				}
			} else if code != 0 || !envelope.OK || len(envelope.Warnings) != 1 || envelope.Warnings[0].Code != "ui_sync_failed" || recalls.Load() != 1 {
				t.Fatalf("code=%d envelope=%#v recalls=%d", code, envelope, recalls.Load())
			}
			if enqueues.Load() != 1 || stderr.Len() != 0 {
				t.Fatalf("enqueues=%d stderr=%s", enqueues.Load(), stderr.String())
			}
		})
	}
}
