package generation_test

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/avienor/bediz/internal/generation"
	"github.com/avienor/bediz/internal/operation"
)

var sdxlMain = generation.ModelIdentifier{Key: "sdxl-main", Hash: "main-hash", Name: "SDXL Main", Base: "sdxl", Type: "main"}
var sdxlVAE = generation.ModelIdentifier{Key: "sdxl-vae", Hash: "vae-hash", Name: "SDXL VAE", Base: "sdxl", Type: "vae"}

func TestResolveSDXLUsesBundledVAEAndFamilyDefaults(t *testing.T) {
	request := generation.Request{SchemaVersion: 1, Model: sdxlMain.Key, PositivePrompt: "a lighthouse"}
	resolved, err := generation.ResolveSDXL(request, []generation.ModelIdentifier{sdxlMain, sdxlVAE}, bytes.NewReader([]byte{1, 0, 0, 0}))
	if err != nil {
		t.Fatal(err)
	}
	settings := resolved.Request
	if *settings.Width != 1024 || *settings.Height != 1024 || *settings.Steps != 30 || *settings.Scheduler != "dpmpp_3m_k" || *settings.Guidance != 7 || *settings.OutputCount != 1 || *settings.Seed != 1 || settings.NegativePrompt != "" || resolved.Models.VAE.Key != "" || !reflect.DeepEqual(resolved.Seeds, []uint32{1}) || request.Width != nil {
		t.Fatalf("resolved = %#v, submitted = %#v", resolved, request)
	}
}

func TestResolveSDXLExplicitSettingsAndVAE(t *testing.T) {
	request := generation.Request{SchemaVersion: 1, Model: sdxlMain.Name, PositivePrompt: "a lighthouse", NegativePrompt: "text", Width: new(768), Height: new(512), Steps: new(2), Scheduler: new("ddim"), Guidance: new(2.5), Seed: new(uint32(41)), OutputCount: new(3), Components: &generation.Components{VAE: new(sdxlVAE.Name)}}
	resolved, err := generation.ResolveSDXL(request, []generation.ModelIdentifier{sdxlMain, sdxlVAE}, bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Models.VAE != sdxlVAE || *resolved.Request.Width != 768 || *resolved.Request.Height != 512 || *resolved.Request.Steps != 2 || *resolved.Request.Scheduler != "ddim" || *resolved.Request.Guidance != 2.5 || resolved.Request.NegativePrompt != "text" || !reflect.DeepEqual(resolved.Seeds, []uint32{41, 42, 43}) {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestResolveSDXLSchedulerSet(t *testing.T) {
	approved := []string{"ddim", "ddpm", "deis", "deis_k", "lms", "lms_k", "pndm", "heun", "heun_k", "euler", "euler_k", "euler_a", "kdpm_2", "kdpm_2_k", "kdpm_2_a", "kdpm_2_a_k", "dpmpp_2s", "dpmpp_2s_k", "dpmpp_2m", "dpmpp_2m_k", "dpmpp_2m_sde", "dpmpp_2m_sde_k", "dpmpp_3m", "dpmpp_3m_k", "dpmpp_sde", "dpmpp_sde_k", "er_sde", "unipc", "unipc_k", "lcm", "tcd"}
	for _, scheduler := range approved {
		t.Run(scheduler, func(t *testing.T) {
			request := generation.Request{SchemaVersion: 1, Model: sdxlMain.Key, PositivePrompt: "test", Scheduler: new(scheduler), Seed: new(uint32(1))}
			if _, err := generation.ResolveSDXL(request, []generation.ModelIdentifier{sdxlMain}, bytes.NewReader(nil)); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, scheduler := range []string{"anima_only", "unknown"} {
		request := generation.Request{SchemaVersion: 1, Model: sdxlMain.Key, PositivePrompt: "test", Scheduler: new(scheduler), Seed: new(uint32(1))}
		if _, err := generation.ResolveSDXL(request, []generation.ModelIdentifier{sdxlMain}, bytes.NewReader(nil)); err == nil || !strings.Contains(err.Error(), "scheduler") {
			t.Fatalf("scheduler %q: %v", scheduler, err)
		}
	}
}

func TestResolveSDXLRejectsInvalidSettingsAndComponents(t *testing.T) {
	cases := []struct {
		name string
		edit func(*generation.Request)
	}{
		{"width only", func(r *generation.Request) { r.Width = new(768) }},
		{"height only", func(r *generation.Request) { r.Height = new(768) }},
		{"unaligned width", func(r *generation.Request) { r.Width, r.Height = new(769), new(768) }},
		{"zero height", func(r *generation.Request) { r.Width, r.Height = new(768), new(0) }},
		{"zero steps", func(r *generation.Request) { r.Steps = new(0) }},
		{"low guidance", func(r *generation.Request) { r.Guidance = new(0.5) }},
		{"NaN guidance", func(r *generation.Request) { r.Guidance = new(math.NaN()) }},
		{"infinite guidance", func(r *generation.Request) { r.Guidance = new(math.Inf(1)) }},
		{"Anima component", func(r *generation.Request) { r.Components = &generation.Components{Qwen3Encoder: new("encoder")} }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := generation.Request{SchemaVersion: 1, Model: sdxlMain.Key, PositivePrompt: "test", Seed: new(uint32(1))}
			test.edit(&request)
			_, err := generation.ResolveSDXL(request, []generation.ModelIdentifier{sdxlMain}, bytes.NewReader(nil))
			if _, ok := errors.AsType[*operation.InvalidRequestError](err); !ok {
				t.Fatalf("error = %v, want invalid_request", err)
			}
		})
	}
}

func TestResolveSDXLVAEErrors(t *testing.T) {
	request := generation.Request{SchemaVersion: 1, Model: sdxlMain.Key, PositivePrompt: "test", Seed: new(uint32(1)), Components: &generation.Components{VAE: new("same")}}
	duplicate := sdxlVAE
	duplicate.Name = "same"
	other := duplicate
	other.Key = "other-vae"
	_, err := generation.ResolveSDXL(request, []generation.ModelIdentifier{sdxlMain, duplicate, other}, bytes.NewReader(nil))
	if _, ok := errors.AsType[*operation.SelectionRequiredError](err); !ok {
		t.Fatalf("ambiguous VAE error = %v", err)
	}
	request.Components.VAE = new("wrong-vae")
	wrong := sdxlVAE
	wrong.Key, wrong.Base = "wrong-vae", "anima"
	_, err = generation.ResolveSDXL(request, []generation.ModelIdentifier{sdxlMain, wrong}, bytes.NewReader(nil))
	if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok {
		t.Fatalf("incompatible VAE error = %v", err)
	}
}
