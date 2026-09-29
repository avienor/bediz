package generation_test

import (
	"bytes"
	"encoding/json/v2"
	"reflect"
	"testing"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/generation"
	"github.com/avienor/bediz/internal/images"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/sourceimage"
)

func TestCompiledGenerationInvocationsAreCoveredByCapabilityMatrix(t *testing.T) {
	animaMain := generation.ModelIdentifier{Key: "anima-main", Hash: "main-hash", Name: "Anima Main", Base: "anima", Type: "main"}
	inventory := []generation.ModelIdentifier{
		animaMain,
		{Key: "anima-vae", Hash: "vae-hash", Name: "Anima VAE", Base: "anima", Type: "vae"},
		{Key: "qwen3", Hash: "encoder-hash", Name: "Qwen3", Base: "any", Type: "qwen3_encoder"},
		sdxlMain, sdxlVAE, fluxDev, fluxSchnell, fluxVAE, fluxT5, fluxCLIP,
	}
	mainModels := map[string][]generation.ModelIdentifier{
		"anima": {animaMain},
		"sdxl":  {sdxlMain},
		"flux":  {fluxDev, fluxSchnell},
	}
	for _, entry := range capability.Matrix {
		if entry.Operation != result.OperationGenerate {
			continue
		}
		t.Run(entry.Family+"/"+entry.Mode, func(t *testing.T) {
			models := mainModels[entry.Family]
			if len(models) == 0 {
				t.Fatalf("no test inventory for generation family %q", entry.Family)
			}
			for _, main := range models {
				t.Run(main.Key, func(t *testing.T) {
					request := generation.Request{
						SchemaVersion: 1, Model: main.Key, PositivePrompt: "a lighthouse",
						Width: new(768), Height: new(512), Seed: new(uint32(41)),
					}
					if entry.Family == "sdxl" {
						request.Components = &generation.Components{VAE: new(sdxlVAE.Key)}
					}
					switch entry.Mode {
					case "txt2img":
					case "img2img":
						request.Source = &sourceimage.Source{Type: "image", Reference: "source.png"}
						request.Strength = new(0.6)
					default:
						t.Fatalf("unrecognized generation mode %q", entry.Mode)
					}
					resolved, err := generation.Resolve(request, inventory, bytes.NewReader(nil))
					if err != nil {
						t.Fatal(err)
					}
					if request.Source != nil {
						resolved.SourceImage = images.Reference{ImageName: "source.png", Width: 769, Height: 513}
					}
					requirements, err := generation.CapabilityEntry(resolved)
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(requirements, entry) {
						t.Fatalf("registry capability = %#v, want matrix entry %#v", requirements, entry)
					}
					compiled, err := generation.Compile(resolved)
					if err != nil {
						t.Fatal(err)
					}
					encoded, err := json.Marshal(compiled.Batch.Graph)
					if err != nil {
						t.Fatal(err)
					}
					var graph struct {
						Nodes map[string]struct {
							Type string `json:"type"`
						} `json:"nodes"`
					}
					if err := json.Unmarshal(encoded, &graph); err != nil {
						t.Fatal(err)
					}
					if len(graph.Nodes) == 0 {
						t.Fatal("compiled generation graph has no invocations")
					}
					covered := make(map[string]bool)
					for _, invocation := range requirements.Invocations {
						covered[invocation.Type] = true
					}
					for id, node := range graph.Nodes {
						if !covered[node.Type] {
							t.Errorf("compiled node %q uses invocation %q absent from the %s %s capability", id, node.Type, entry.Family, entry.Mode)
						}
					}
				})
			}
		})
	}
}

func TestResolveDefaultsDenoisingStrengthForImageToImage(t *testing.T) {
	request := generation.Request{
		SchemaVersion: 1, Model: sdxlMain.Key, PositivePrompt: "a lighthouse", Seed: new(uint32(41)),
		Source: &sourceimage.Source{Type: "image", Reference: "source.png"},
	}
	resolved, err := generation.Resolve(request, []generation.ModelIdentifier{sdxlMain}, bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Request.Strength == nil || *resolved.Request.Strength != 0.75 {
		t.Fatalf("resolved Denoising Strength = %v, want 0.75", resolved.Request.Strength)
	}
	if request.Strength != nil {
		t.Fatal("resolution mutated the submitted Denoising Strength")
	}
}
