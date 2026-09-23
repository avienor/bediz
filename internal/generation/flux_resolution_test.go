package generation_test

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/avienor/bediz/internal/generation"
	"github.com/avienor/bediz/internal/operation"
)

var fluxDev = generation.ModelIdentifier{Key: "flux-dev", Hash: "dev-hash", Name: "FLUX dev", Base: "flux", Type: "main", Variant: "dev", Format: "bnb_quantized_nf4b"}
var fluxSchnell = generation.ModelIdentifier{Key: "flux-schnell", Hash: "schnell-hash", Name: "FLUX schnell", Base: "flux", Type: "main", Variant: "schnell", Format: "bnb_quantized_nf4b"}
var fluxVAE = generation.ModelIdentifier{Key: "flux-vae", Hash: "vae-hash", Name: "FLUX VAE", Base: "flux", Type: "vae"}
var fluxT5 = generation.ModelIdentifier{Key: "flux-t5", Hash: "t5-hash", Name: "T5", Base: "any", Type: "t5_encoder"}
var fluxCLIP = generation.ModelIdentifier{Key: "flux-clip", Hash: "clip-hash", Name: "CLIP", Base: "any", Type: "clip_embed"}

func fluxInventory(main generation.ModelIdentifier) []generation.ModelIdentifier {
	return []generation.ModelIdentifier{main, fluxVAE, fluxT5, fluxCLIP}
}

func TestResolveFLUXVariantDefaultsAndRequiredComponents(t *testing.T) {
	for _, tc := range []struct {
		main     generation.ModelIdentifier
		steps    int
		guidance *float64
	}{{fluxDev, 30, new(4.0)}, {fluxSchnell, 4, nil}} {
		t.Run(tc.main.Variant, func(t *testing.T) {
			request := generation.Request{SchemaVersion: 1, Model: tc.main.Key, PositivePrompt: "a lighthouse", Seed: new(uint32(41))}
			got, err := generation.ResolveFLUX(request, fluxInventory(tc.main), bytes.NewReader(nil))
			if err != nil {
				t.Fatal(err)
			}
			if *got.Request.Width != 1024 || *got.Request.Height != 1024 || *got.Request.Steps != tc.steps || *got.Request.Scheduler != "euler" || *got.Request.OutputCount != 1 || !reflect.DeepEqual(got.Request.Guidance, tc.guidance) || !reflect.DeepEqual(got.Seeds, []uint32{41}) || got.Models.VAE != fluxVAE || got.Models.T5Encoder != fluxT5 || got.Models.CLIPEmbed != fluxCLIP || request.Width != nil {
				t.Fatalf("resolution = %#v", got)
			}
		})
	}
}

func TestResolveFLUXRejectsUnsupportedVariantsAndFormat(t *testing.T) {
	for _, tc := range []struct{ variant, format string }{{"dev_fill", "checkpoint"}, {"", "checkpoint"}, {"future", "checkpoint"}, {"dev", "sdnq_quantized"}, {"dev", "diffusers"}, {"dev", "unknown"}, {"schnell", "olive"}} {
		main := fluxDev
		main.Variant, main.Format = tc.variant, tc.format
		request := generation.Request{SchemaVersion: 1, Model: main.Key, PositivePrompt: "test", Seed: new(uint32(1))}
		_, err := generation.ResolveFLUX(request, fluxInventory(main), bytes.NewReader(nil))
		if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok {
			t.Fatalf("variant=%q format=%q: %v", tc.variant, tc.format, err)
		}
	}
}

func TestResolveFLUXAcceptsStockLoaderFormats(t *testing.T) {
	for _, format := range []string{"checkpoint", "bnb_quantized_nf4b", "gguf_quantized"} {
		t.Run(format, func(t *testing.T) {
			main := fluxDev
			main.Format = format
			request := generation.Request{SchemaVersion: 1, Model: main.Key, PositivePrompt: "test", Seed: new(uint32(1))}
			if _, err := generation.ResolveFLUX(request, fluxInventory(main), bytes.NewReader(nil)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestResolveFLUXValidatesVariantSettingsAndComponents(t *testing.T) {
	for _, tc := range []struct {
		name   string
		main   generation.ModelIdentifier
		change func(*generation.Request)
	}{
		{"schnell guidance", fluxSchnell, func(r *generation.Request) { r.Guidance = new(4.0) }},
		{"dev negative", fluxDev, func(r *generation.Request) { r.NegativePrompt = "bad" }},
		{"schnell negative", fluxSchnell, func(r *generation.Request) { r.NegativePrompt = "bad" }},
		{"unaligned", fluxDev, func(r *generation.Request) { r.Width, r.Height = new(1000), new(1024) }},
		{"bad scheduler", fluxDev, func(r *generation.Request) { r.Scheduler = new("ddim") }},
		{"inapplicable encoder", fluxDev, func(r *generation.Request) { r.Components = &generation.Components{Qwen3Encoder: new("unused")} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := generation.Request{SchemaVersion: 1, Model: tc.main.Key, PositivePrompt: "test", Seed: new(uint32(1))}
			tc.change(&request)
			_, err := generation.ResolveFLUX(request, fluxInventory(tc.main), bytes.NewReader(nil))
			if _, ok := errors.AsType[*operation.InvalidRequestError](err); !ok {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestResolveFLUXComponentSelectionAndMissingGuidance(t *testing.T) {
	request := generation.Request{SchemaVersion: 1, Model: fluxDev.Key, PositivePrompt: "test", Seed: new(uint32(1))}
	for _, tc := range []struct {
		name      string
		inventory []generation.ModelIdentifier
		change    func(*generation.Request)
		wantCode  string
	}{
		{"missing VAE", []generation.ModelIdentifier{fluxDev, fluxT5, fluxCLIP}, nil, "missing_component"},
		{"missing T5", []generation.ModelIdentifier{fluxDev, fluxVAE, fluxCLIP}, nil, "missing_component"},
		{"missing CLIP", []generation.ModelIdentifier{fluxDev, fluxVAE, fluxT5}, nil, "missing_component"},
		{"ambiguous VAE", append(fluxInventory(fluxDev), generation.ModelIdentifier{Key: "vae-2", Hash: "hash-2", Name: "VAE 2", Base: "flux", Type: "vae"}), nil, "selection_required"},
		{"ambiguous T5", append(fluxInventory(fluxDev), generation.ModelIdentifier{Key: "t5-2", Hash: "hash-2", Name: "T5 2", Base: "any", Type: "t5_encoder"}), nil, "selection_required"},
		{"ambiguous CLIP", append(fluxInventory(fluxDev), generation.ModelIdentifier{Key: "clip-2", Hash: "hash-2", Name: "CLIP 2", Base: "any", Type: "clip_embed"}), nil, "selection_required"},
		{"explicit components", append(fluxInventory(fluxDev), generation.ModelIdentifier{Key: "vae-2", Hash: "hash-2", Name: "VAE 2", Base: "flux", Type: "vae"}), func(r *generation.Request) {
			r.Components = &generation.Components{VAE: new(fluxVAE.Key), T5Encoder: new(fluxT5.Key), CLIPEmbed: new(fluxCLIP.Key)}
		}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := request
			if tc.change != nil {
				tc.change(&r)
			}
			got, err := generation.ResolveFLUX(r, tc.inventory, bytes.NewReader(nil))
			if tc.wantCode == "" {
				if err != nil || got.Models.VAE.Key != fluxVAE.Key {
					t.Fatalf("resolution = %#v; error = %v", got, err)
				}
				return
			}
			switch tc.wantCode {
			case "missing_component":
				if _, ok := errors.AsType[*operation.MissingComponentError](err); !ok {
					t.Fatalf("error = %v", err)
				}
			case "selection_required":
				if _, ok := errors.AsType[*operation.SelectionRequiredError](err); !ok {
					t.Fatalf("error = %v", err)
				}
			}
		})
	}
}
