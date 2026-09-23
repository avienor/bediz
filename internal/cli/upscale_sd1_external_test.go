package cli_test

import (
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/result"
)

// sd1UpscaleInventory installs both upscale families so every SD1.5 case also
// proves that SDXL components are never offered for an SD1.5 main model.
func sd1UpscaleInventory() []map[string]any {
	return append(upscaleInventory(),
		map[string]any{"key": "sd1-main", "hash": "sd1-main-hash", "name": "Dreamshaper 8", "base": "sd-1", "type": "main", "variant": "normal", "format": "diffusers"},
		map[string]any{"key": "sd1-tile", "hash": "sd1-tile-hash", "name": "Tile", "base": "sd-1", "type": "controlnet", "format": "diffusers"},
		map[string]any{"key": "sd1-vae", "hash": "sd1-vae-hash", "name": "SD1 VAE", "base": "sd-1", "type": "vae", "format": "diffusers"},
	)
}

func TestUpscaleSD1SubmitsStockSD1BranchWithSD1Components(t *testing.T) {
	var enqueues atomic.Int32
	type endpoint struct {
		NodeID string `json:"node_id"`
		Field  string `json:"field"`
	}
	type graphPayload struct {
		Nodes map[string]map[string]any `json:"nodes"`
		Edges []struct {
			Source      endpoint `json:"source"`
			Destination endpoint `json:"destination"`
		} `json:"edges"`
	}
	var graph graphPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), sd1UpscaleInventory()) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/images/i/source.png":
			_ = json.MarshalWrite(w, upscaleImage("source.png", 512, 512))
		case "/api/v1/queue/default/enqueue_batch":
			enqueues.Add(1)
			var payload struct {
				Batch struct {
					Graph *graphPayload `json:"graph"`
				} `json:"batch"`
			}
			payload.Batch.Graph = &graph
			if err := json.UnmarshalRead(r.Body, &payload); err != nil {
				t.Errorf("decode enqueue: %v", err)
			}
			_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "batch": map[string]any{"batch_id": "sd1-batch"}, "item_ids": []int{31}, "enqueued": 1, "requested": 1})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	code, envelope := runUpscale(t, server.URL, "upscale", "--no-wait", "--image", "source.png", "--model", "Dreamshaper 8",
		"--tile-controlnet", "sd1-tile", "--vae", "SD1 VAE", "--scale", "2", "--seed", "42")
	if code != 0 || !envelope.OK || enqueues.Load() != 1 {
		t.Fatalf("code=%d envelope=%#v enqueues=%d", code, envelope, enqueues.Load())
	}
	s := envelope.Data.ResolvedSettings
	if s.ModelKey != "sd1-main" || s.OutputWidth != 1024 || s.OutputHeight != 1024 || s.Scheduler != "kdpm_2" || s.Guidance != 2 ||
		!reflect.DeepEqual(s.ComponentKeys, map[string]string{"upscale_model": "spandrel", "tile_controlnet": "sd1-tile", "vae": "sd1-vae"}) || !slices.Equal(s.Seeds, []uint32{42}) {
		t.Fatalf("resolved settings = %#v", s)
	}
	types := map[string]int{}
	for _, node := range graph.Nodes {
		types[node["type"].(string)]++
	}
	if types["main_model_loader"] != 1 || types["clip_skip"] != 1 || types["compel"] != 2 || types["vae_loader"] != 1 ||
		types["sdxl_model_loader"] != 0 || types["sdxl_compel_prompt"] != 0 || types["controlnet"] != 2 {
		t.Fatalf("node types = %#v", types)
	}
	for _, node := range graph.Nodes {
		switch node["type"] {
		case "main_model_loader":
			if node["model"].(map[string]any)["key"] != "sd1-main" {
				t.Fatalf("main loader = %#v", node)
			}
		case "clip_skip":
			if node["skipped_layers"] != float64(0) {
				t.Fatalf("clip skip = %#v", node)
			}
		case "controlnet":
			if node["control_model"].(map[string]any)["key"] != "sd1-tile" {
				t.Fatalf("control model = %#v", node)
			}
		case "vae_loader":
			if node["vae_model"].(map[string]any)["key"] != "sd1-vae" {
				t.Fatalf("VAE loader = %#v", node)
			}
		}
	}
	for _, edge := range graph.Edges {
		if edge.Destination.Field == "style" || edge.Source.Field == "clip2" {
			t.Fatalf("SD1.5 branch has SDXL-only edge %#v", edge)
		}
	}
}

func TestUpscaleSD1ComponentResolutionNeverEnqueues(t *testing.T) {
	without := func(keys ...string) []map[string]any {
		return slices.DeleteFunc(sd1UpscaleInventory(), func(model map[string]any) bool { return slices.Contains(keys, model["key"].(string)) })
	}
	with := func(models ...map[string]any) []map[string]any { return append(sd1UpscaleInventory(), models...) }
	tests := []struct {
		name           string
		inventory      []map[string]any
		args           []string
		wantCode       string
		wantCandidates []string
		wantGuidance   string
	}{
		{"one SD1.5 Tile requires choice", sd1UpscaleInventory(), []string{"--model", "sd1-main"}, result.CodeSelectionRequired, []string{"sd1-tile"}, ""},
		{"several SD1.5 Tile require choice", with(map[string]any{"key": "sd1-other", "hash": "sd1-other-hash", "name": "Other", "base": "sd-1", "type": "controlnet"}),
			[]string{"--model", "sd1-main"}, result.CodeSelectionRequired, []string{"sd1-other", "sd1-tile"}, ""},
		{"missing SD1.5 Tile", without("sd1-tile"), []string{"--model", "sd1-main"}, result.CodeMissingComponent, nil, "lllyasviel/control_v11f1e_sd15_tile"},
		{"non normal SD1.5 main", func() []map[string]any {
			models := sd1UpscaleInventory()
			models[4]["variant"] = "inpaint"
			return models
		}(), []string{"--model", "sd1-main", "--tile-controlnet", "sd1-tile"}, result.CodeUnsupportedCapability, nil, ""},
		{"SDXL Tile for SD1.5 main", sd1UpscaleInventory(), []string{"--model", "sd1-main", "--tile-controlnet", "tile"}, result.CodeUnsupportedCapability, nil, ""},
		{"SDXL VAE for SD1.5 main", sd1UpscaleInventory(), []string{"--model", "sd1-main", "--tile-controlnet", "sd1-tile", "--vae", "vae"}, result.CodeUnsupportedCapability, nil, ""},
		{"SD1.5 Tile for SDXL main", sd1UpscaleInventory(), []string{"--model", "sdxl-main", "--tile-controlnet", "sd1-tile"}, result.CodeUnsupportedCapability, nil, ""},
		{"SD1.5 VAE for SDXL main", sd1UpscaleInventory(), []string{"--model", "sdxl-main", "--tile-controlnet", "tile", "--vae", "sd1-vae"}, result.CodeUnsupportedCapability, nil, ""},
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
					_ = json.MarshalWrite(w, upscaleImage("source.png", 512, 512))
				case "/api/v1/queue/default/enqueue_batch":
					enqueues.Add(1)
					http.Error(w, "unexpected enqueue", http.StatusInternalServerError)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			args := append([]string{"upscale", "--no-wait", "--image", "source.png", "--seed", "42"}, tc.args...)
			code, envelope := runUpscale(t, server.URL, args...)
			if code != result.ExitStatus(tc.wantCode) || envelope.Error == nil || envelope.Error.Code != tc.wantCode || enqueues.Load() != 0 {
				t.Fatalf("code=%d envelope=%#v enqueues=%d", code, envelope, enqueues.Load())
			}
			if tc.wantCandidates != nil {
				candidates, _ := envelope.Error.Details["candidates"].([]any)
				keys := make([]string, 0, len(candidates))
				for _, candidate := range candidates {
					keys = append(keys, candidate.(map[string]any)["key"].(string))
				}
				if envelope.Error.Details["kind"] != "tile_controlnet" || envelope.Error.Details["selector"] != "" || !slices.Equal(keys, tc.wantCandidates) {
					t.Fatalf("selection details = %#v", envelope.Error.Details)
				}
			}
			if tc.wantGuidance != "" {
				guidance, _ := envelope.Error.Details["installation_guidance"].(string)
				if envelope.Error.Details["component_type"] != "tile_controlnet" || envelope.Error.Details["required_base"] != "sd-1" || !strings.Contains(guidance, tc.wantGuidance) {
					t.Fatalf("missing details = %#v", envelope.Error.Details)
				}
			}
		})
	}
}
