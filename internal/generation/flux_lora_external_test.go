package generation_test

import (
	"bytes"
	json "encoding/json/v2"
	"math"
	"os"
	"reflect"
	"testing"

	"github.com/avienor/bediz/internal/generation"
	"github.com/avienor/bediz/internal/images"
	"github.com/avienor/bediz/internal/sourceimage"
)

func TestResolveFLUXLoRAKeepsComponentsAndResolvesWeightsInRequestOrder(t *testing.T) {
	first := generation.ModelIdentifier{Key: "lora-a", Hash: "hash-a", Name: "Realism", Base: "flux", Type: "lora", Format: "lycoris", DefaultSettings: &generation.ModelDefaultSettings{Weight: new(1.25)}}
	second := generation.ModelIdentifier{Key: "lora-b", Hash: "hash-b", Name: "Second LoRA", Base: "flux", Type: "lora", Format: "checkpoint"}
	for _, main := range []generation.ModelIdentifier{fluxDev, fluxSchnell} {
		for _, format := range []string{"checkpoint", "bnb_quantized_nf4b", "gguf_quantized"} {
			t.Run(main.Variant+"/"+format, func(t *testing.T) {
				main.Format = format
				request := generation.Request{SchemaVersion: 1, Model: main.Key, PositivePrompt: "a lighthouse", Seed: new(uint32(41)), Loras: []generation.LoRA{{Model: first.Name}, {Model: second.Key}}}
				resolved, err := generation.Resolve(request, append(fluxInventory(main), first, second), bytes.NewReader(nil))
				if err != nil {
					t.Fatal(err)
				}
				if len(resolved.Loras) != 2 || resolved.Loras[0].Model.Key != first.Key || resolved.Loras[0].Weight != 1.25 || resolved.Loras[1].Model.Key != second.Key || resolved.Loras[1].Weight != 0.75 {
					t.Fatalf("resolved LoRAs = %#v", resolved.Loras)
				}
				if resolved.Models.VAE.Key != fluxVAE.Key || resolved.Models.T5Encoder.Key != fluxT5.Key || resolved.Models.CLIPEmbed.Key != fluxCLIP.Key || request.Loras[0].Weight != nil {
					t.Fatalf("components or submitted weights changed: %#v", resolved)
				}
			})
		}
	}
}

func TestCompileFLUXLoRAMatchesInvokeAI614Fixtures(t *testing.T) {
	first := generation.ModelIdentifier{Key: "lora-a", Hash: "hash-a", Name: "Realism", Base: "flux", Type: "lora"}
	second := generation.ModelIdentifier{Key: "lora-b", Hash: "hash-b", Name: "Second LoRA", Base: "flux", Type: "lora"}
	for _, main := range []generation.ModelIdentifier{fluxDev, fluxSchnell} {
		for _, mode := range []string{"one", "two", "img2img"} {
			t.Run(main.Variant+"/"+mode, func(t *testing.T) {
				request := generation.Request{SchemaVersion: 1, Model: main.Key, PositivePrompt: "a lighthouse", Width: new(768), Height: new(512), Steps: new(4), Scheduler: new("euler"), Seed: new(uint32(41)), OutputCount: new(1), Loras: []generation.LoRA{{Model: first.Key, Weight: new(1.0)}}}
				if mode == "two" {
					request.Loras = append(request.Loras, generation.LoRA{Model: second.Key, Weight: new(0.5)})
				}
				if mode == "img2img" {
					request.Source = &sourceimage.Source{Type: "image", Reference: "source.png"}
					request.Strength = new(1.0)
					if main.Variant == "schnell" {
						request.Strength = new(0.01)
					}
				}
				resolved, err := generation.Resolve(request, append(fluxInventory(main), first, second), bytes.NewReader(nil))
				if err != nil {
					t.Fatal(err)
				}
				resolved.SourceImage = images.Reference{ImageName: "source.png", Width: 768, Height: 512}
				if main.Variant == "dev" {
					resolved.SourceImage.Width, resolved.SourceImage.Height = 1001, 750
				}
				compiled, err := generation.Compile(resolved)
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(compiled)
				if err != nil {
					t.Fatal(err)
				}
				fixture, err := os.ReadFile("testdata/flux_6_14_lora_" + main.Variant + "_" + mode + "_enqueue.json")
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
				if mode == "img2img" {
					actualDenoise := actual["batch"].(map[string]any)["graph"].(map[string]any)["nodes"].(map[string]any)["denoise"].(map[string]any)
					expectedDenoise := expected["batch"].(map[string]any)["graph"].(map[string]any)["nodes"].(map[string]any)["denoise"].(map[string]any)
					// Frontend exponentiation and Go math.Pow can differ by one ULP.
					if math.Abs(actualDenoise["denoising_start"].(float64)-expectedDenoise["denoising_start"].(float64)) > 1e-15 {
						t.Fatalf("denoising_start = %v, want %v", actualDenoise["denoising_start"], expectedDenoise["denoising_start"])
					}
					actualDenoise["denoising_start"] = expectedDenoise["denoising_start"]
				}
				if !reflect.DeepEqual(actual, expected) {
					t.Fatalf("FLUX LoRA graph differs from 6.14.1 fixture: got %s, want %s", encoded, fixture)
				}
			})
		}
	}
}
