package generation_test

import (
	json "encoding/json/v2"
	"os"
	"reflect"
	"testing"

	"github.com/avienor/bediz/internal/generation"
)

func TestCompileAnimaProducesInvokeAI614Graph(t *testing.T) {
	request := generation.Request{
		SchemaVersion:  1,
		Model:          "main-key",
		PositivePrompt: "a lighthouse in a storm",
		NegativePrompt: "text",
		Width:          new(768),
		Height:         new(1024),
		Steps:          new(24),
		Scheduler:      new("heun"),
		Guidance:       new(4.25),
		Seed:           new(uint32(42)),
		OutputCount:    new(1),
		BoardID:        "board-1",
		Components: &generation.Components{
			VAE:          "vae-key",
			Qwen3Encoder: "encoder-key",
		},
	}
	models := generation.ResolvedModels{
		Main:         generation.ModelIdentifier{Key: "main-key", Hash: "blake3:main", Name: "Anima Main", Base: "anima", Type: "main"},
		VAE:          generation.ModelIdentifier{Key: "vae-key", Hash: "blake3:vae", Name: "Anima VAE", Base: "anima", Type: "vae"},
		Qwen3Encoder: generation.ModelIdentifier{Key: "encoder-key", Hash: "blake3:encoder", Name: "Qwen3 Encoder", Base: "any", Type: "qwen3_encoder"},
	}

	got, err := generation.CompileAnima(request, models)
	if err != nil {
		t.Fatal(err)
	}
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := os.ReadFile("testdata/anima_6_14_enqueue.json")
	if err != nil {
		t.Fatal(err)
	}
	var gotValue any
	var wantValue any
	if err := json.Unmarshal(gotJSON, &gotValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(wantJSON, &wantValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("compiled enqueue request does not match the InvokeAI 6.14 fixture\ngot:  %s\nwant: %s", gotJSON, wantJSON)
	}
}
