package generation_test

import (
	json "encoding/json/v2"
	"os"
	"reflect"
	"testing"

	"github.com/avienor/bediz/internal/generation"
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
			got, err := generation.CompileSDXL(generation.Resolution{Request: request, Models: models, Seeds: []uint32{41}})
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

func TestCompileSDXLAlignsBatchSeedsWithReturnedItemOrder(t *testing.T) {
	request := generation.Request{SchemaVersion: 1, Model: sdxlMain.Key, PositivePrompt: "test", Width: new(1024), Height: new(1024), Steps: new(30), Scheduler: new("euler"), Guidance: new(7.0), Seed: new(uint32(5)), OutputCount: new(3)}
	got, err := generation.CompileSDXL(generation.Resolution{Request: request, Models: generation.ResolvedModels{Main: sdxlMain}, Seeds: []uint32{5, 6, 7}})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]generation.BatchDatum{{{NodePath: "seed", FieldName: "value", Items: []uint32{7, 6, 5}}}}
	if got.Batch.Runs != 1 || !reflect.DeepEqual(got.Batch.Data, want) {
		t.Fatalf("batch = %#v, want one run with %#v", got.Batch, want)
	}
}
