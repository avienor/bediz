package generation_test

import (
	json "encoding/json/v2"
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/avienor/bediz/internal/generation"
)

func TestCompileFLUXMatchesInvokeAI614Fixtures(t *testing.T) {
	for _, tc := range []struct {
		main     generation.ModelIdentifier
		board    string
		guidance *float64
		fixture  string
	}{
		{fluxDev, "", new(4.0), "testdata/flux_6_14_dev_enqueue.json"},
		{fluxDev, "board-1", new(4.0), "testdata/flux_6_14_dev_board_enqueue.json"},
		{fluxSchnell, "", nil, "testdata/flux_6_14_schnell_enqueue.json"},
		{fluxSchnell, "board-1", nil, "testdata/flux_6_14_schnell_board_enqueue.json"},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			r := generation.Request{SchemaVersion: 1, Model: tc.main.Key, PositivePrompt: "a lighthouse", Width: new(768), Height: new(512), Steps: new(4), Scheduler: new("euler"), Guidance: tc.guidance, Seed: new(uint32(41)), OutputCount: new(1), BoardID: tc.board}
			got, err := generation.CompileFLUX(generation.Resolution{Request: r, Models: generation.ResolvedModels{Main: tc.main, VAE: fluxVAE, T5Encoder: fluxT5, CLIPEmbed: fluxCLIP}, Seeds: []uint32{41}})
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			fixture, err := os.ReadFile(tc.fixture)
			if err != nil {
				t.Fatal(err)
			}
			var actual, expected map[string]any
			if err := json.Unmarshal(encoded, &actual); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(fixture, &expected); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("graph differs from 6.14.1 %s\ngot: %s\nwant: %s", tc.fixture, encoded, fixture)
			}
		})
	}
}

func TestFLUXFixturesUseInvokeAI614OpenAPIVocabulary(t *testing.T) {
	encoded, err := os.ReadFile("../doctor/testdata/invokeai_6_14_anima_openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var vocabulary struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]any `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(encoded, &vocabulary); err != nil {
		t.Fatal(err)
	}
	inputSchema := map[string]string{"model_loader": "FluxModelLoaderInvocation", "positive_prompt": "StringInvocation", "positive_conditioning": "FluxTextEncoderInvocation", "seed": "IntegerInvocation", "denoise": "FluxDenoiseInvocation", "metadata": "CoreMetadataInvocation", "decode": "FluxVaeDecodeInvocation"}
	outputSchema := map[string]string{"model_loader": "FluxModelLoaderOutput", "positive_prompt": "StringOutput", "positive_conditioning": "FluxConditioningOutput", "seed": "IntegerOutput", "denoise": "LatentsOutput", "metadata": "MetadataOutput"}
	for _, path := range []string{"testdata/flux_6_14_dev_enqueue.json", "testdata/flux_6_14_dev_board_enqueue.json", "testdata/flux_6_14_schnell_enqueue.json", "testdata/flux_6_14_schnell_board_enqueue.json"} {
		t.Run(path, func(t *testing.T) {
			encoded, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var fixture struct {
				Batch struct {
					Graph struct {
						Nodes map[string]map[string]any `json:"nodes"`
						Edges []generation.Edge         `json:"edges"`
					} `json:"graph"`
				} `json:"batch"`
			}
			if err := json.Unmarshal(encoded, &fixture); err != nil {
				t.Fatal(err)
			}
			for id, node := range fixture.Batch.Graph.Nodes {
				properties := vocabulary.Components.Schemas[inputSchema[id]].Properties
				for field := range node {
					if _, ok := properties[field]; !ok {
						t.Errorf("%s.%s absent from %s", id, field, inputSchema[id])
					}
				}
			}
			for _, edge := range fixture.Batch.Graph.Edges {
				if _, ok := vocabulary.Components.Schemas[outputSchema[edge.Source.NodeID]].Properties[edge.Source.Field]; !ok {
					t.Errorf("source %s.%s absent from %s", edge.Source.NodeID, edge.Source.Field, outputSchema[edge.Source.NodeID])
				}
				if _, ok := vocabulary.Components.Schemas[inputSchema[edge.Destination.NodeID]].Properties[edge.Destination.Field]; !ok {
					t.Errorf("destination %s.%s absent from %s", edge.Destination.NodeID, edge.Destination.Field, inputSchema[edge.Destination.NodeID])
				}
			}
		})
	}
}

func TestCompileFLUXStock614Topology(t *testing.T) {
	for _, tc := range []struct {
		main     generation.ModelIdentifier
		board    string
		guidance *float64
	}{
		{fluxDev, "", new(4.0)}, {fluxDev, "board-1", new(4.0)},
		{fluxSchnell, "", nil}, {fluxSchnell, "board-1", nil},
	} {
		t.Run(tc.main.Variant+tc.board, func(t *testing.T) {
			request := generation.Request{SchemaVersion: 1, Model: tc.main.Key, PositivePrompt: "a lighthouse", Width: new(768), Height: new(512), Steps: new(4), Scheduler: new("euler"), Guidance: tc.guidance, Seed: new(uint32(41)), OutputCount: new(2), BoardID: tc.board}
			batch, err := generation.CompileFLUX(generation.Resolution{Request: request, Models: generation.ResolvedModels{Main: tc.main, VAE: fluxVAE, T5Encoder: fluxT5, CLIPEmbed: fluxCLIP}, Seeds: []uint32{41, 42}})
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(batch)
			if err != nil {
				t.Fatal(err)
			}
			var payload struct {
				Batch struct {
					Graph struct {
						Nodes map[string]map[string]any `json:"nodes"`
						Edges []generation.Edge         `json:"edges"`
					} `json:"graph"`
					Data [][]generation.BatchDatum `json:"data"`
				} `json:"batch"`
			}
			if err := json.Unmarshal(encoded, &payload); err != nil {
				t.Fatal(err)
			}
			nodes := payload.Batch.Graph.Nodes
			for id, kind := range map[string]string{"model_loader": "flux_model_loader", "positive_conditioning": "flux_text_encoder", "denoise": "flux_denoise", "metadata": "core_metadata", "decode": "flux_vae_decode", "seed": "integer", "positive_prompt": "string"} {
				if nodes[id]["type"] != kind {
					t.Errorf("%s node = %#v", id, nodes[id])
				}
			}
			loader := nodes["model_loader"]
			for field, key := range map[string]string{"model": tc.main.Key, "vae_model": fluxVAE.Key, "t5_encoder_model": fluxT5.Key, "clip_embed_model": fluxCLIP.Key} {
				if loader[field].(map[string]any)["key"] != key {
					t.Errorf("loader %s = %#v", field, loader[field])
				}
			}
			if nodes["denoise"]["cfg_scale"] != float64(1) || nodes["metadata"]["cfg_scale"] != float64(1) || nodes["decode"]["is_intermediate"] != false {
				t.Errorf("nodes = %#v", nodes)
			}
			if tc.board != "" && nodes["decode"]["board"].(map[string]any)["board_id"] != tc.board {
				t.Errorf("board = %#v", nodes["decode"])
			}
			if tc.board == "" {
				if _, ok := nodes["decode"]["board"]; ok {
					t.Error("unexpected board")
				}
			}
			if tc.guidance == nil {
				if _, ok := nodes["denoise"]["guidance"]; ok {
					t.Error("schnell guidance must be omitted")
				}
			} else if nodes["denoise"]["guidance"] != *tc.guidance {
				t.Errorf("guidance = %#v", nodes["denoise"])
			}
			if !slices.Equal(payload.Batch.Data[0][0].Items, []uint32{42, 41}) {
				t.Errorf("batch data = %#v", payload.Batch.Data)
			}
			if !hasFluxEdge(payload.Batch.Graph.Edges, "model_loader", "max_seq_len", "positive_conditioning", "t5_max_seq_len") {
				t.Error("loader max_seq_len edge missing")
			}
		})
	}
}

func hasFluxEdge(edges []generation.Edge, sourceNode, sourceField, destinationNode, destinationField string) bool {
	for _, edge := range edges {
		if edge.Source.NodeID == sourceNode && edge.Source.Field == sourceField && edge.Destination.NodeID == destinationNode && edge.Destination.Field == destinationField {
			return true
		}
	}
	return false
}
