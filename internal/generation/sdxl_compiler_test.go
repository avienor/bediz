package generation_test

import (
	json "encoding/json/v2"
	"os"
	"reflect"
	"testing"

	"github.com/avienor/bediz/internal/generation"
	"github.com/avienor/bediz/internal/images"
	"github.com/avienor/bediz/internal/sourceimage"
)

func TestCompileSDXLMatchesInvokeAI614Fixtures(t *testing.T) {
	for _, test := range []struct {
		name  string
		vae   bool
		board bool
	}{
		{"bundled VAE", false, false},
		{"bundled VAE with board", false, true},
		{"VAE override", true, false},
		{"VAE override with board", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := generation.Request{SchemaVersion: 1, Model: sdxlMain.Key, PositivePrompt: "a lighthouse", NegativePrompt: "text", Width: new(768), Height: new(512), Steps: new(24), Scheduler: new("heun"), Guidance: new(6.5), Seed: new(uint32(41)), OutputCount: new(1)}
			models := generation.ResolvedModels{Main: sdxlMain}
			if test.vae {
				models.VAE = sdxlVAE
			}
			if test.board {
				request.BoardID = "board-1"
			}
			got, err := generation.Compile(generation.Resolution{Request: request, Models: models, Seeds: []uint32{41}})
			if err != nil {
				t.Fatal(err)
			}
			gotJSON, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			fixture := "testdata/sdxl_6_14_enqueue.json"
			if test.vae {
				fixture = "testdata/sdxl_6_14_vae_enqueue.json"
			}
			wantJSON, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatal(err)
			}
			var gotValue map[string]any
			var wantValue map[string]any
			if err := json.Unmarshal(gotJSON, &gotValue); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(wantJSON, &wantValue); err != nil {
				t.Fatal(err)
			}
			if test.board {
				wantValue["batch"].(map[string]any)["graph"].(map[string]any)["nodes"].(map[string]any)["decode"].(map[string]any)["board"] = map[string]any{"board_id": "board-1"}
			}
			if !reflect.DeepEqual(gotValue, wantValue) {
				t.Fatalf("SDXL graph differs from InvokeAI 6.14.1 fixture\ngot: %s\nwant: %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestCompileSDXLImageToImageUsesSourceStrengthAndResolvedSize(t *testing.T) {
	request := generation.Request{SchemaVersion: 1, Model: sdxlMain.Key, PositivePrompt: "a lighthouse", Width: new(1000), Height: new(744), Steps: new(24), Scheduler: new("heun"), Guidance: new(6.5), Seed: new(uint32(41)), OutputCount: new(1), Source: &sourceimage.Source{Type: "image", Reference: "source.png"}, Strength: new(0.6)}
	compiled, err := generation.Compile(generation.Resolution{Request: request, Models: generation.ResolvedModels{Main: sdxlMain, VAE: sdxlVAE}, Seeds: []uint32{41}, SourceImage: images.Reference{ImageName: "source.png", Width: 1001, Height: 750}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(compiled)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	graph := value["batch"].(map[string]any)["graph"].(map[string]any)
	nodes := graph["nodes"].(map[string]any)
	if nodes["resize"].(map[string]any)["resample_mode"] != "bicubic" || nodes["resize"].(map[string]any)["is_intermediate"] != true || nodes["i2l"].(map[string]any)["fp32"] != true || nodes["i2l"].(map[string]any)["color_compensation"] != "None" || nodes["denoise"].(map[string]any)["denoising_start"] != 0.4 || nodes["metadata"].(map[string]any)["generation_mode"] != "sdxl_img2img" || nodes["metadata"].(map[string]any)["init_image"] != "source.png" || nodes["metadata"].(map[string]any)["strength"] != 0.6 {
		t.Fatalf("img2img nodes: %s", encoded)
	}
	want := map[string]bool{"resize:image>i2l:image": false, "vae_loader:vae>i2l:vae": false, "i2l:latents>denoise:latents": false}
	for _, raw := range graph["edges"].([]any) {
		edge := raw.(map[string]any)
		from := edge["source"].(map[string]any)
		to := edge["destination"].(map[string]any)
		key := from["node_id"].(string) + ":" + from["field"].(string) + ">" + to["node_id"].(string) + ":" + to["field"].(string)
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for edge, found := range want {
		if !found {
			t.Errorf("missing edge %s in %s", edge, encoded)
		}
	}
}

func TestCompileSDXLImageToImageMatchesInvokeAI614Fixtures(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		sourceWidth, sourceHeight int
		vae, board                bool
	}{
		{"no_resize", 768, 512, false, false},
		{"resize", 769, 513, false, false},
		{"vae", 768, 512, true, false},
		{"board", 768, 512, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := generation.Request{SchemaVersion: 1, Model: sdxlMain.Key, PositivePrompt: "a lighthouse", NegativePrompt: "text", Width: new(768), Height: new(512), Steps: new(24), Scheduler: new("heun"), Guidance: new(6.5), Seed: new(uint32(41)), OutputCount: new(1), Source: &sourceimage.Source{Type: "image", Reference: "source.png"}, Strength: new(0.6)}
			models := generation.ResolvedModels{Main: sdxlMain}
			if tc.vae {
				models.VAE = sdxlVAE
			}
			if tc.board {
				request.BoardID = "board-1"
			}
			compiled, err := generation.Compile(generation.Resolution{Request: request, Models: models, Seeds: []uint32{41}, SourceImage: images.Reference{ImageName: "source.png", Width: tc.sourceWidth, Height: tc.sourceHeight}})
			if err != nil {
				t.Fatal(err)
			}
			gotJSON, err := json.Marshal(compiled)
			if err != nil {
				t.Fatal(err)
			}
			wantJSON, err := os.ReadFile("testdata/sdxl_6_14_img2img_" + tc.name + "_enqueue.json")
			if err != nil {
				t.Fatal(err)
			}
			var got, want map[string]any
			if err := json.Unmarshal(gotJSON, &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(wantJSON, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("graph differs from InvokeAI 6.14.1 %s fixture\ngot: %s\nwant: %s", tc.name, gotJSON, wantJSON)
			}
		})
	}
}

func TestCompileSDXLAlignsBatchSeedsWithReturnedItemOrder(t *testing.T) {
	request := generation.Request{SchemaVersion: 1, Model: sdxlMain.Key, PositivePrompt: "test", Width: new(1024), Height: new(1024), Steps: new(30), Scheduler: new("euler"), Guidance: new(7.0), Seed: new(uint32(5)), OutputCount: new(3)}
	got, err := generation.Compile(generation.Resolution{Request: request, Models: generation.ResolvedModels{Main: sdxlMain}, Seeds: []uint32{5, 6, 7}})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]generation.BatchDatum{{{NodePath: "seed", FieldName: "value", Items: []uint32{7, 6, 5}}}}
	if got.Batch.Runs != 1 || !reflect.DeepEqual(got.Batch.Data, want) {
		t.Fatalf("batch = %#v, want one run with %#v", got.Batch, want)
	}
}
