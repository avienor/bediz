package cli_test

import (
	"bytes"
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/avienor/bediz/internal/cli"
)

func newLoRASyncServer(t *testing.T, document map[string]any, inventory []map[string]any, enqueue http.HandlerFunc) (*httptest.Server, *[]string, *[]map[string]any) {
	t.Helper()
	mutations := &[]string{}
	patches := &[]map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", document, inventory) {
			return
		}
		if r.Method == http.MethodPost {
			*mutations = append(*mutations, r.Method+" "+r.URL.Path)
		}
		switch r.URL.Path {
		case "/api/v1/queue/default/enqueue_batch":
			if enqueue != nil {
				enqueue(w, r)
				return
			}
			_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "batch": map[string]any{"batch_id": "sync-batch"}, "item_ids": []int{19}, "enqueued": 1, "requested": 1})
		case "/api/v1/recall/default":
			var patch map[string]any
			if err := json.UnmarshalRead(r.Body, &patch); err != nil {
				t.Error(err)
			}
			*patches = append(*patches, patch)
			_, _ = w.Write([]byte(`{"status":"success"}`))
		case "/api/v1/images/i/source.png", "/api/v1/images/i/generated.png":
			_ = json.MarshalWrite(w, upscaleImage(r.URL.Path[len("/api/v1/images/i/"):], 768, 768))
		case "/api/v1/queue/default/i/19":
			_ = json.MarshalWrite(w, map[string]any{
				"item_id": 19, "queue_id": "default", "batch_id": "sync-batch", "session_id": "session-19", "status": "completed", "priority": 0,
				"created_at": "2026-01-01 00:00:00.000", "updated_at": "2026-01-01 00:00:05.000",
				"field_values": []map[string]any{{"node_path": "seed", "field_name": "value", "value": 41}}, "session": completedItemResults("generated.png"),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, mutations, patches
}

func TestGenerateRecallsExactLoRAListForEveryFamilyAndMode(t *testing.T) {
	for _, family := range []struct {
		base, main, name string
		inventory        []map[string]any
		fields           []any
	}{
		{"sdxl", "sdxl-main", "SDXL Main", sdxlInventory(), []any{"scheduler", "vae", "output_count", "board_id"}},
		{"anima", "main-key", "Anima Main", animaModelInventory(), []any{"scheduler", "guidance", "vae", "qwen3_encoder", "output_count", "board_id"}},
		{"flux", "flux-dev", "FLUX dev", fluxCLIInventory(), []any{"scheduler", "guidance", "vae", "t5_encoder", "clip_embed", "output_count", "board_id"}},
		{"flux", "flux-schnell", "FLUX schnell", fluxCLIInventory(), []any{"scheduler", "vae", "t5_encoder", "clip_embed", "output_count", "board_id"}},
	} {
		for _, mode := range []string{"txt2img", "img2img"} {
			for _, withLoRAs := range []bool{false, true} {
				for _, noWait := range []bool{false, true} {
					t.Run(family.main+"/"+mode+map[bool]string{false: "/empty", true: "/loras"}[withLoRAs]+map[bool]string{false: "/wait", true: "/no-wait"}[noWait], func(t *testing.T) {
						isolateUserConfigDir(t)
						inventory := append(slices.Clone(family.inventory),
							map[string]any{"key": "lora-a", "hash": "hash-a", "name": "Default LoRA", "base": family.base, "type": "lora", "default_settings": map[string]any{"weight": 1.25}},
							map[string]any{"key": "lora-b", "hash": "hash-b", "name": "Explicit LoRA", "base": family.base, "type": "lora"},
							map[string]any{"key": "other-type", "hash": "hash-other", "name": "Explicit LoRA", "base": family.base, "type": "control_lora"})
						server, mutations, patches := newLoRASyncServer(t, sdxlOpenAPIFixture(t), inventory, nil)
						args := []string{"--model", family.main, "--prompt", "lighthouse", "--width", "768", "--height", "768", "--steps", "4", "--seed", "41", "--url", server.URL}
						fields := slices.Clone(family.fields)
						if mode == "img2img" {
							args = append(args, "--image", "source.png", "--strength", "0.6")
							fields = append(fields, "source_image", "strength")
						}
						if noWait {
							args = append(args, "--no-wait")
						}
						loras := []any{}
						if withLoRAs {
							args = append(args, "--lora", "lora-b=-0.5", "--lora", "Default LoRA")
							loras = []any{map[string]any{"model_name": "Explicit LoRA", "weight": -0.5}, map[string]any{"model_name": "Default LoRA", "weight": 1.25}}
						}
						code, envelope := runImg2Img(t, args...)
						want := map[string]any{"model": family.name, "positive_prompt": "lighthouse", "negative_prompt": "", "width": float64(768), "height": float64(768), "steps": float64(4), "seed": float64(41), "loras": loras}
						if family.base == "sdxl" {
							want["cfg_scale"] = float64(7)
						}
						if code != 0 || !reflect.DeepEqual(*mutations, []string{"POST /api/v1/queue/default/enqueue_batch", "POST /api/v1/recall/default"}) || !reflect.DeepEqual(*patches, []map[string]any{want}) {
							t.Fatalf("code=%d mutations=%v patches=%#v envelope=%#v", code, *mutations, *patches, envelope)
						}
						data := envelope["data"].(map[string]any)
						warnings := envelope["warnings"].([]any)
						if len(warnings) != 1 || warnings[0].(map[string]any)["code"] != "ui_sync_partial" || !reflect.DeepEqual(data["warnings"], warnings) || !reflect.DeepEqual(warnings[0].(map[string]any)["details"].(map[string]any)["not_restored"], fields) {
							t.Fatalf("warnings = %#v", warnings)
						}
						if withLoRAs && !reflect.DeepEqual(data["resolved_settings"].(map[string]any)["loras"], []any{map[string]any{"model_key": "lora-b", "weight": -0.5}, map[string]any{"model_key": "lora-a", "weight": 1.25}}) {
							t.Fatalf("resolved LoRAs = %#v", data)
						}
						if len(data["outputs"].([]any)) != map[bool]int{false: 1, true: 0}[noWait] {
							t.Fatalf("outputs = %#v", data["outputs"])
						}
					})
				}
			}
		}
	}
}

func TestGenerateLoRANameCollisionPreservesExecutionWithoutRecall(t *testing.T) {
	for _, collisionBase := range []string{"sdxl", "anima"} {
		for _, noWait := range []bool{false, true} {
			t.Run(collisionBase+map[bool]string{false: "/wait", true: "/no-wait"}[noWait], func(t *testing.T) {
				isolateUserConfigDir(t)
				inventory := append(sdxlInventory(),
					map[string]any{"key": "lora-a", "hash": "hash-a", "name": "Shared LoRA", "base": "sdxl", "type": "lora"},
					map[string]any{"key": "other-lora", "hash": "hash-other", "name": "Shared LoRA", "base": collisionBase, "type": "lora"})
				server, mutations, patches := newLoRASyncServer(t, sdxlOpenAPIFixture(t), inventory, nil)
				args := []string{"--model", "sdxl-main", "--prompt", "test", "--seed", "41", "--lora", "lora-a=0", "--url", server.URL}
				if noWait {
					args = append(args, "--no-wait")
				}
				code, envelope := runImg2Img(t, args...)
				if code != 0 || envelope["ok"] != true || !reflect.DeepEqual(*mutations, []string{"POST /api/v1/queue/default/enqueue_batch"}) || len(*patches) != 0 {
					t.Fatalf("code=%d mutations=%v patches=%v envelope=%#v", code, *mutations, *patches, envelope)
				}
				data := envelope["data"].(map[string]any)
				settings := data["resolved_settings"].(map[string]any)
				if !reflect.DeepEqual(settings["loras"], []any{map[string]any{"model_key": "lora-a", "weight": float64(0)}}) || data["queue"].(map[string]any)["batch_id"] != "sync-batch" || !reflect.DeepEqual(data["warnings"], envelope["warnings"]) || envelope["warnings"].([]any)[0].(map[string]any)["code"] != "ui_sync_failed" {
					t.Fatalf("receipt = %#v", data)
				}
			})
		}
	}
}

func TestIncompatibleRecallLoRAsOnlyDisablesSynchronization(t *testing.T) {
	for _, defect := range []string{"missing", "non-array", "missing model_name", "missing weight"} {
		t.Run(defect, func(t *testing.T) {
			isolateUserConfigDir(t)
			document := sdxlOpenAPIFixture(t)
			schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
			properties := schemas["RecallParameter"].(map[string]any)["properties"].(map[string]any)
			switch defect {
			case "missing":
				delete(properties, "loras")
			case "non-array":
				properties["loras"] = map[string]any{"anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "null"}}}
			case "missing model_name":
				delete(schemas["LoRARecallParameter"].(map[string]any)["properties"].(map[string]any), "model_name")
			case "missing weight":
				delete(schemas["LoRARecallParameter"].(map[string]any)["properties"].(map[string]any), "weight")
			}
			inventory := append(upscaleInventory(), map[string]any{"key": "lora-a", "hash": "hash-a", "name": "Alien Style", "base": "sdxl", "type": "lora"})
			server, mutations, patches := newLoRASyncServer(t, document, inventory, nil)
			for _, withLoRA := range []bool{false, true} {
				*mutations = nil
				args := []string{"--no-wait", "--model", "sdxl-main", "--prompt", "test", "--seed", "41", "--url", server.URL}
				if withLoRA {
					args = append(args, "--lora", "lora-a")
				}
				code, envelope := runImg2Img(t, args...)
				data, ok := envelope["data"].(map[string]any)
				if code != 0 || !ok || !reflect.DeepEqual(*mutations, []string{"POST /api/v1/queue/default/enqueue_batch"}) || len(*patches) != 0 || envelope["warnings"].([]any)[0].(map[string]any)["code"] != "ui_sync_failed" || !reflect.DeepEqual(data["warnings"], envelope["warnings"]) || data["queue"].(map[string]any)["batch_id"] != "sync-batch" {
					t.Fatalf("code=%d mutations=%v patches=%v envelope=%#v", code, *mutations, *patches, envelope)
				}
			}
			*mutations = nil
			code, upscaleEnvelope := runUpscale(t, server.URL, "upscale", "--no-wait", "--image", "source.png", "--model", "sdxl-main", "--tile-controlnet", "tile", "--seed", "41")
			if code != 0 || !upscaleEnvelope.OK || !reflect.DeepEqual(*mutations, []string{"POST /api/v1/queue/default/enqueue_batch"}) || upscaleEnvelope.Data.Queue.BatchID != "sync-batch" {
				t.Fatalf("upscale code=%d mutations=%v envelope=%#v", code, *mutations, upscaleEnvelope)
			}
			assertUpscaleSyncWarning(t, upscaleEnvelope, "ui_sync_failed")
			var stdout, stderr bytes.Buffer
			doctorCode := cli.New(&stdout, &stderr).Run(t.Context(), []string{"doctor", "--url", server.URL, "--json"})
			var doctorEnvelope map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &doctorEnvelope); err != nil || stderr.Len() != 0 {
				t.Fatalf("doctor stdout=%s stderr=%s err=%v", stdout.String(), stderr.String(), err)
			}
			if doctorCode != 4 || doctorEnvelope["ok"] != false || doctorEnvelope["error"].(map[string]any)["code"] != "unsupported_capability" {
				t.Fatalf("doctor exit=%d envelope=%#v", doctorCode, doctorEnvelope)
			}
			report := doctorEnvelope["error"].(map[string]any)["details"].(map[string]any)["report"].(map[string]any)
			if len(report["ui_sync"].(map[string]any)) != 0 {
				t.Fatalf("ui_sync = %#v", report["ui_sync"])
			}
			foundRecall := false
			for _, raw := range report["capabilities"].([]any) {
				entry := raw.(map[string]any)
				if entry["operation"] == "recall" {
					foundRecall = true
					if entry["compatible"] != false || !reflect.DeepEqual(entry["failures"], []any{"incompatible_recall_schema:loras"}) {
						t.Fatalf("Recall capability = %#v", entry)
					}
				}
				if (entry["operation"] == "generate" || entry["operation"] == "upscale") && entry["family"] == "sdxl" && entry["compatible"] != true {
					t.Fatalf("Direct Execution compatibility changed: %#v", entry)
				}
			}
			if !foundRecall {
				t.Fatal("missing Recall capability")
			}
			// Standalone Recall still succeeds and leaves the LoRA list untouched.
			stdout.Reset()
			stderr.Reset()
			*patches = nil
			code = cli.New(&stdout, &stderr).Run(t.Context(), []string{"recall", "--prompt", "standalone", "--url", server.URL, "--json"})
			if code != 0 || stderr.Len() != 0 || !reflect.DeepEqual(*patches, []map[string]any{{"positive_prompt": "standalone"}}) {
				t.Fatalf("standalone code=%d patch=%v stdout=%s stderr=%s", code, *patches, stdout.String(), stderr.String())
			}
		})
	}
}

func TestStandaloneRecallRejectsLoRAs(t *testing.T) {
	for _, document := range []string{`{"schema_version":1,"positive_prompt":"test","loras":[]}`, `{"schema_version":1,"positive_prompt":"test","loras":[{"model":"lora-a","weight":1}]}`} {
		var stdout, stderr bytes.Buffer
		code := cli.NewWithIO(strings.NewReader(document), &stdout, &stderr).Run(t.Context(), []string{"recall", "--request", "-", "--json"})
		var envelope map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if code != 2 || stderr.Len() != 0 || envelope["error"].(map[string]any)["code"] != "invalid_request" {
			t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
		}
	}
}

func TestLoRAGenerationDoesNotRecallAfterFailedOrInconclusiveEnqueue(t *testing.T) {
	for _, status := range []int{422, 503, 200} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			isolateUserConfigDir(t)
			inventory := append(sdxlInventory(), map[string]any{"key": "lora-a", "hash": "hash-a", "name": "Alien Style", "base": "sdxl", "type": "lora"})
			server, mutations, patches := newLoRASyncServer(t, sdxlOpenAPIFixture(t), inventory, func(w http.ResponseWriter, _ *http.Request) {
				if status != 200 {
					http.Error(w, "rejected", status)
					return
				}
				_, _ = w.Write([]byte(`{"queue_id":"default","batch":{"batch_id":"sync-batch"},"item_ids":[],"enqueued":0,"requested":1}`))
			})
			code, envelope := runImg2Img(t, "--no-wait", "--model", "sdxl-main", "--prompt", "test", "--seed", "41", "--lora", "lora-a", "--url", server.URL)
			wantCode := "outcome_unknown"
			if status == 422 {
				wantCode = "invokeai_operation_failed"
			}
			if code != 6 || envelope["error"].(map[string]any)["code"] != wantCode || !reflect.DeepEqual(*mutations, []string{"POST /api/v1/queue/default/enqueue_batch"}) || len(*patches) != 0 {
				t.Fatalf("code=%d mutations=%v patches=%v envelope=%#v", code, *mutations, *patches, envelope)
			}
		})
	}
}
