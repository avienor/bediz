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

func TestResolveAnimaAppliesFamilyDefaultsAndUniqueComponents(t *testing.T) {
	request := generation.Request{
		SchemaVersion:  1,
		Model:          "main-key",
		PositivePrompt: "a lighthouse in a storm",
	}
	inventory := []generation.ModelIdentifier{
		{Key: "encoder-key", Hash: "blake3:encoder", Name: "Qwen3 Encoder", Base: "any", Type: "qwen3_encoder"},
		{Key: "main-key", Hash: "blake3:main", Name: "Anima Main", Base: "anima", Type: "main"},
		{Key: "vae-key", Hash: "blake3:vae", Name: "Anima VAE", Base: "anima", Type: "vae"},
	}

	resolved, err := generation.ResolveAnima(request, inventory, bytes.NewReader([]byte{0x78, 0x56, 0x34, 0x12}))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Request.Width == nil || *resolved.Request.Width != 1024 ||
		resolved.Request.Height == nil || *resolved.Request.Height != 1024 ||
		resolved.Request.Steps == nil || *resolved.Request.Steps != 30 ||
		resolved.Request.Scheduler == nil || *resolved.Request.Scheduler != "euler" ||
		resolved.Request.Guidance == nil || *resolved.Request.Guidance != 4.5 ||
		resolved.Request.OutputCount == nil || *resolved.Request.OutputCount != 1 ||
		resolved.Request.Seed == nil || *resolved.Request.Seed != 0x12345678 ||
		resolved.Request.NegativePrompt != "" {
		t.Fatalf("unexpected resolved defaults: %#v", resolved.Request)
	}
	if !reflect.DeepEqual(resolved.Seeds, []uint32{0x12345678}) {
		t.Fatalf("resolved seeds = %v, want [305419896]", resolved.Seeds)
	}
	if resolved.Models.Main.Key != "main-key" || resolved.Models.VAE.Key != "vae-key" || resolved.Models.Qwen3Encoder.Key != "encoder-key" {
		t.Fatalf("unexpected resolved models: %#v", resolved.Models)
	}
	if request.Width != nil || request.Components != nil || request.Seed != nil {
		t.Fatalf("resolution mutated the submitted request: %#v", request)
	}
}

func TestResolveAnimaReturnsStableSelectionCandidates(t *testing.T) {
	main := generation.ModelIdentifier{Key: "main-key", Hash: "blake3:main", Name: "Anima Main", Base: "anima", Type: "main"}
	vae := generation.ModelIdentifier{Key: "vae-key", Hash: "blake3:vae", Name: "Anima VAE", Base: "anima", Type: "vae"}
	encoder := generation.ModelIdentifier{Key: "encoder-key", Hash: "blake3:encoder", Name: "Qwen3 Encoder", Base: "any", Type: "qwen3_encoder"}
	request := generation.Request{SchemaVersion: 1, Model: main.Key, PositivePrompt: "test", Seed: new(uint32(1))}
	tests := []struct {
		name       string
		request    generation.Request
		inventory  []generation.ModelIdentifier
		kind       string
		selector   string
		candidates []string
	}{
		{
			name:      "ambiguous main model name",
			request:   generation.Request{SchemaVersion: 1, Model: "Same Anima", PositivePrompt: "test", Seed: new(uint32(1))},
			inventory: []generation.ModelIdentifier{{Key: "main-b", Hash: "b", Name: "Same Anima", Base: "anima", Type: "main"}, encoder, {Key: "main-a", Hash: "a", Name: "Same Anima", Base: "anima", Type: "main"}, vae},
			kind:      "main_model", selector: "Same Anima", candidates: []string{"main-a", "main-b"},
		},
		{
			name:      "multiple compatible VAEs",
			request:   request,
			inventory: []generation.ModelIdentifier{main, {Key: "vae-b", Hash: "b", Name: "VAE B", Base: "anima", Type: "vae"}, encoder, {Key: "vae-a", Hash: "a", Name: "VAE A", Base: "anima", Type: "vae"}},
			kind:      "vae", selector: "", candidates: []string{"vae-a", "vae-b"},
		},
		{
			name: "ambiguous encoder name",
			request: generation.Request{
				SchemaVersion: 1, Model: main.Key, PositivePrompt: "test", Seed: new(uint32(1)),
				Components: &generation.Components{VAE: new(vae.Key), Qwen3Encoder: new("Same Encoder")},
			},
			inventory: []generation.ModelIdentifier{main, {Key: "encoder-b", Hash: "b", Name: "Same Encoder", Base: "any", Type: "qwen3_encoder"}, vae, {Key: "encoder-a", Hash: "a", Name: "Same Encoder", Base: "any", Type: "qwen3_encoder"}},
			kind:      "qwen3_encoder", selector: "Same Encoder", candidates: []string{"encoder-a", "encoder-b"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := generation.ResolveAnima(test.request, test.inventory, &errorReader{err: errors.New("unexpected random read")})
			selection, ok := errors.AsType[*operation.SelectionRequiredError](err)
			if !ok {
				t.Fatalf("error = %v, want selection_required", err)
			}
			keys := make([]string, len(selection.Candidates))
			for index, candidate := range selection.Candidates {
				keys[index] = candidate.Key
			}
			if selection.Kind != test.kind || selection.Selector != test.selector || !reflect.DeepEqual(keys, test.candidates) {
				t.Fatalf("selection = %#v, candidate keys = %q", selection, keys)
			}
		})
	}
}

func TestResolveAnimaExplicitSettingsAndCompatibleSelectorsWin(t *testing.T) {
	random := &errorReader{err: errors.New("random source must not be read")}
	request := generation.Request{
		SchemaVersion:  1,
		Model:          "Anima Main",
		PositivePrompt: "a lighthouse in a storm",
		NegativePrompt: "text",
		Width:          new(768),
		Height:         new(1024),
		Steps:          new(24),
		Scheduler:      new("heun"),
		Guidance:       new(4.25),
		Seed:           new(uint32(42)),
		OutputCount:    new(1),
		Components: &generation.Components{
			VAE:          new("vae-b"),
			Qwen3Encoder: new("Encoder B"),
		},
	}
	inventory := []generation.ModelIdentifier{
		{Key: "main-key", Hash: "blake3:main", Name: "Anima Main", Base: "anima", Type: "main"},
		{Key: "vae-a", Hash: "blake3:vae-a", Name: "VAE A", Base: "anima", Type: "vae"},
		{Key: "vae-b", Hash: "blake3:vae-b", Name: "VAE B", Base: "anima", Type: "vae"},
		{Key: "encoder-a", Hash: "blake3:encoder-a", Name: "Encoder A", Base: "any", Type: "qwen3_encoder"},
		{Key: "encoder-b", Hash: "blake3:encoder-b", Name: "Encoder B", Base: "any", Type: "qwen3_encoder"},
	}

	resolved, err := generation.ResolveAnima(request, inventory, random)
	if err != nil {
		t.Fatal(err)
	}
	if *resolved.Request.Width != 768 || *resolved.Request.Height != 1024 || *resolved.Request.Steps != 24 ||
		*resolved.Request.Scheduler != "heun" || *resolved.Request.Guidance != 4.25 || *resolved.Request.Seed != 42 ||
		*resolved.Request.OutputCount != 1 || resolved.Request.NegativePrompt != "text" {
		t.Fatalf("explicit settings did not win: %#v", resolved.Request)
	}
	if resolved.Models.Main.Key != "main-key" || resolved.Models.VAE.Key != "vae-b" || resolved.Models.Qwen3Encoder.Key != "encoder-b" {
		t.Fatalf("explicit selectors did not win: %#v", resolved.Models)
	}
}

func TestResolveAnimaAssignsIndependentRandomSeeds(t *testing.T) {
	request := generation.Request{
		SchemaVersion: 1, Model: "main-key", PositivePrompt: "test", OutputCount: new(3),
	}
	inventory := []generation.ModelIdentifier{
		{Key: "main-key", Hash: "blake3:main", Name: "Anima Main", Base: "anima", Type: "main"},
		{Key: "vae-key", Hash: "blake3:vae", Name: "Anima VAE", Base: "anima", Type: "vae"},
		{Key: "encoder-key", Hash: "blake3:encoder", Name: "Qwen3 Encoder", Base: "any", Type: "qwen3_encoder"},
	}
	random := bytes.NewReader([]byte{
		0x01, 0x00, 0x00, 0x00,
		0xef, 0xbe, 0xad, 0xde,
		0xff, 0xff, 0xff, 0xff,
	})

	resolved, err := generation.ResolveAnima(request, inventory, random)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resolved.Seeds, []uint32{1, 0xdeadbeef, math.MaxUint32}) {
		t.Fatalf("resolved seeds = %v", resolved.Seeds)
	}
	if resolved.Request.Seed == nil || *resolved.Request.Seed != 1 {
		t.Fatalf("graph seed = %v, want the first resolved seed", resolved.Request.Seed)
	}
}

func TestResolveAnimaIncrementsExplicitSeedWithUnsignedWraparound(t *testing.T) {
	request := generation.Request{
		SchemaVersion: 1, Model: "main-key", PositivePrompt: "test",
		Seed: new(uint32(math.MaxUint32 - 1)), OutputCount: new(4),
	}
	inventory := []generation.ModelIdentifier{
		{Key: "main-key", Hash: "blake3:main", Name: "Anima Main", Base: "anima", Type: "main"},
		{Key: "vae-key", Hash: "blake3:vae", Name: "Anima VAE", Base: "anima", Type: "vae"},
		{Key: "encoder-key", Hash: "blake3:encoder", Name: "Qwen3 Encoder", Base: "any", Type: "qwen3_encoder"},
	}

	resolved, err := generation.ResolveAnima(request, inventory, &errorReader{err: errors.New("random source must not be read")})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resolved.Seeds, []uint32{math.MaxUint32 - 1, math.MaxUint32, 0, 1}) {
		t.Fatalf("resolved seeds = %v", resolved.Seeds)
	}
}

type errorReader struct {
	err error
}

func (r *errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

func TestResolveAnimaRejectsInvalidSettings(t *testing.T) {
	valid := generation.Request{
		SchemaVersion:  1,
		Model:          "main-key",
		PositivePrompt: "test",
		Width:          new(1024),
		Height:         new(1024),
		Steps:          new(30),
		Scheduler:      new("euler"),
		Guidance:       new(4.5),
		Seed:           new(uint32(1)),
		OutputCount:    new(1),
	}
	inventory := []generation.ModelIdentifier{
		{Key: "main-key", Hash: "blake3:main", Name: "Anima Main", Base: "anima", Type: "main"},
		{Key: "vae-key", Hash: "blake3:vae", Name: "Anima VAE", Base: "anima", Type: "vae"},
		{Key: "encoder-key", Hash: "blake3:encoder", Name: "Qwen3 Encoder", Base: "any", Type: "qwen3_encoder"},
	}
	tests := []struct {
		name    string
		change  func(*generation.Request)
		message string
	}{
		{name: "schema version", change: func(r *generation.Request) { r.SchemaVersion = 2 }, message: "schema version"},
		{name: "model", change: func(r *generation.Request) { r.Model = "" }, message: "model is required"},
		{name: "positive prompt", change: func(r *generation.Request) { r.PositivePrompt = "" }, message: "positive prompt is required"},
		{name: "width without height", change: func(r *generation.Request) { r.Height = nil }, message: "width and height"},
		{name: "height without width", change: func(r *generation.Request) { r.Width = nil }, message: "width and height"},
		{name: "zero width", change: func(r *generation.Request) { r.Width = new(0) }, message: "positive multiples of 8"},
		{name: "unaligned height", change: func(r *generation.Request) { r.Height = new(1023) }, message: "positive multiples of 8"},
		{name: "zero steps", change: func(r *generation.Request) { r.Steps = new(0) }, message: "steps must be positive"},
		{name: "scheduler", change: func(r *generation.Request) { r.Scheduler = new("ddim") }, message: "scheduler is not supported"},
		{name: "low guidance", change: func(r *generation.Request) { r.Guidance = new(0.99) }, message: "guidance must be finite and at least 1"},
		{name: "NaN guidance", change: func(r *generation.Request) { r.Guidance = new(math.NaN()) }, message: "guidance must be finite and at least 1"},
		{name: "infinite guidance", change: func(r *generation.Request) { r.Guidance = new(math.Inf(1)) }, message: "guidance must be finite and at least 1"},
		{name: "zero output count", change: func(r *generation.Request) { r.OutputCount = new(0) }, message: "output count must be positive"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			test.change(&request)

			_, err := generation.ResolveAnima(request, inventory, &errorReader{err: errors.New("unexpected random read")})
			invalid, ok := errors.AsType[*operation.InvalidRequestError](err)
			if !ok || !strings.Contains(invalid.Error(), test.message) {
				t.Fatalf("error = %v, want invalid_request containing %q", err, test.message)
			}
		})
	}
}

func TestResolveAnimaAcceptsEveryApprovedScheduler(t *testing.T) {
	inventory := []generation.ModelIdentifier{
		{Key: "main-key", Hash: "blake3:main", Name: "Anima Main", Base: "anima", Type: "main"},
		{Key: "vae-key", Hash: "blake3:vae", Name: "Anima VAE", Base: "anima", Type: "vae"},
		{Key: "encoder-key", Hash: "blake3:encoder", Name: "Qwen3 Encoder", Base: "any", Type: "qwen3_encoder"},
	}
	for _, scheduler := range []string{"euler", "heun", "dpmpp_2m", "dpmpp_2m_sde", "er_sde", "lcm"} {
		t.Run(scheduler, func(t *testing.T) {
			request := generation.Request{
				SchemaVersion: 1, Model: "main-key", PositivePrompt: "test",
				Scheduler: new(scheduler), Seed: new(uint32(1)),
			}

			resolved, err := generation.ResolveAnima(request, inventory, &errorReader{err: errors.New("unexpected random read")})
			if err != nil {
				t.Fatal(err)
			}
			if *resolved.Request.Scheduler != scheduler {
				t.Fatalf("scheduler = %q, want %q", *resolved.Request.Scheduler, scheduler)
			}
		})
	}
}

func TestResolveAnimaReportsMissingComponents(t *testing.T) {
	main := generation.ModelIdentifier{Key: "main-key", Hash: "blake3:main", Name: "Anima Main", Base: "anima", Type: "main"}
	vae := generation.ModelIdentifier{Key: "vae-key", Hash: "blake3:vae", Name: "Anima VAE", Base: "anima", Type: "vae"}
	encoder := generation.ModelIdentifier{Key: "encoder-key", Hash: "blake3:encoder", Name: "Qwen3 Encoder", Base: "any", Type: "qwen3_encoder"}
	request := generation.Request{SchemaVersion: 1, Model: main.Key, PositivePrompt: "test", Seed: new(uint32(1))}
	tests := []struct {
		name          string
		inventory     []generation.ModelIdentifier
		componentType string
		base          string
		modelType     string
	}{
		{name: "Anima VAE", inventory: []generation.ModelIdentifier{main, encoder}, componentType: "vae", base: "anima", modelType: "vae"},
		{name: "Qwen3 encoder", inventory: []generation.ModelIdentifier{main, vae}, componentType: "qwen3_encoder", base: "any", modelType: "qwen3_encoder"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := generation.ResolveAnima(request, test.inventory, &errorReader{err: errors.New("unexpected random read")})
			missing, ok := errors.AsType[*operation.MissingComponentError](err)
			if !ok {
				t.Fatalf("error = %v, want missing_component", err)
			}
			if missing.ComponentType != test.componentType || missing.RequiredBase != test.base ||
				missing.RequiredType != test.modelType || missing.InstallationGuidance == "" {
				t.Fatalf("unexpected missing-component details: %#v", missing)
			}
		})
	}
}

func TestResolveAnimaRejectsIncompatibleOrIncompleteExactModels(t *testing.T) {
	main := generation.ModelIdentifier{Key: "main-key", Hash: "blake3:main", Name: "Anima Main", Base: "anima", Type: "main"}
	vae := generation.ModelIdentifier{Key: "vae-key", Hash: "blake3:vae", Name: "Anima VAE", Base: "anima", Type: "vae"}
	encoder := generation.ModelIdentifier{Key: "encoder-key", Hash: "blake3:encoder", Name: "Qwen3 Encoder", Base: "any", Type: "qwen3_encoder"}
	baseRequest := generation.Request{SchemaVersion: 1, Model: main.Key, PositivePrompt: "test", Seed: new(uint32(1))}
	tests := []struct {
		name      string
		request   generation.Request
		inventory []generation.ModelIdentifier
	}{
		{
			name:      "main model family",
			request:   generation.Request{SchemaVersion: 1, Model: "wrong-main", PositivePrompt: "test", Seed: new(uint32(1))},
			inventory: []generation.ModelIdentifier{{Key: "wrong-main", Hash: "wrong", Name: "Wrong Main", Base: "flux", Type: "main"}, vae, encoder},
		},
		{
			name: "FLUX VAE fallback",
			request: generation.Request{
				SchemaVersion: 1, Model: main.Key, PositivePrompt: "test", Seed: new(uint32(1)),
				Components: &generation.Components{VAE: new("flux-vae"), Qwen3Encoder: new(encoder.Key)},
			},
			inventory: []generation.ModelIdentifier{main, {Key: "flux-vae", Hash: "flux", Name: "FLUX VAE", Base: "flux", Type: "vae"}, encoder},
		},
		{
			name: "FLUX VAE fallback selected by unique name",
			request: generation.Request{
				SchemaVersion: 1, Model: main.Key, PositivePrompt: "test", Seed: new(uint32(1)),
				Components: &generation.Components{VAE: new("FLUX VAE"), Qwen3Encoder: new(encoder.Key)},
			},
			inventory: []generation.ModelIdentifier{main, {Key: "flux-vae", Hash: "flux", Name: "FLUX VAE", Base: "flux", Type: "vae"}, encoder},
		},
		{
			name: "encoder type",
			request: generation.Request{
				SchemaVersion: 1, Model: main.Key, PositivePrompt: "test", Seed: new(uint32(1)),
				Components: &generation.Components{VAE: new(vae.Key), Qwen3Encoder: new("clip-key")},
			},
			inventory: []generation.ModelIdentifier{main, vae, {Key: "clip-key", Hash: "clip", Name: "CLIP", Base: "any", Type: "clip_embed"}},
		},
		{
			name:      "incomplete component identifier",
			request:   baseRequest,
			inventory: []generation.ModelIdentifier{main, {Key: "vae-key", Name: "Anima VAE", Base: "anima", Type: "vae"}, encoder},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := generation.ResolveAnima(test.request, test.inventory, &errorReader{err: errors.New("unexpected random read")})
			if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok {
				t.Fatalf("error = %v, want unsupported_capability", err)
			}
		})
	}
}

func TestResolveAnimaRejectsIncompleteAmbiguityCandidates(t *testing.T) {
	request := generation.Request{SchemaVersion: 1, Model: "main-key", PositivePrompt: "test", Seed: new(uint32(1))}
	inventory := []generation.ModelIdentifier{
		{Key: "main-key", Hash: "blake3:main", Name: "Anima Main", Base: "anima", Type: "main"},
		{Key: "", Hash: "blake3:vae-a", Name: "VAE A", Base: "anima", Type: "vae"},
		{Key: "vae-b", Hash: "blake3:vae-b", Name: "VAE B", Base: "anima", Type: "vae"},
		{Key: "encoder-key", Hash: "blake3:encoder", Name: "Qwen3 Encoder", Base: "any", Type: "qwen3_encoder"},
	}

	_, err := generation.ResolveAnima(request, inventory, &errorReader{err: errors.New("unexpected random read")})
	if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok {
		t.Fatalf("error = %v, want unsupported_capability", err)
	}
}
