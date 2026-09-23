package upscale_test

import (
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/images"
	"github.com/avienor/bediz/internal/upscale"
)

// These payloads record the no-LoRA SDXL branches of the installed stock
// InvokeAI 6.14.1 Upscale builder and its optional VAE and board branches.
func TestSDXLGraphMatchesVersionedEnqueueFixtures(t *testing.T) {
	models := upscale.Models{
		Main:           graphops.ModelIdentifier{Key: "main", Hash: "main-hash", Name: "SDXL", Base: "sdxl", Type: "main", Variant: "normal"},
		UpscaleModel:   graphops.ModelIdentifier{Key: "spandrel", Hash: "spandrel-hash", Name: "RealESRGAN", Base: "any", Type: "spandrel_image_to_image"},
		TileControlNet: graphops.ModelIdentifier{Key: "controlnet", Hash: "controlnet-hash", Name: "Tile", Base: "sdxl", Type: "controlnet"},
	}
	request := upscale.Request{
		SchemaVersion: 1, Source: upscale.Source{Type: "image", Reference: "source.png"}, Model: "main",
		PositivePrompt: "mountain landscape", NegativePrompt: "text", Scale: new(2), Creativity: new(0), Structure: new(0),
		Steps: new(30), Scheduler: new("kdpm_2"), Guidance: new(2.0), Seed: new(uint32(42)), TileSize: new(1024), TileOverlap: new(128),
	}
	for _, test := range []struct {
		name  string
		vae   bool
		board string
	}{
		{name: "sdxl_6_14_enqueue.json"},
		{name: "sdxl_6_14_vae_board_enqueue.json", vae: true, board: "board-7"},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolvedModels := models
			if test.vae {
				resolvedModels.VAE = graphops.ModelIdentifier{Key: "vae", Hash: "vae-hash", Name: "Override", Base: "sdxl", Type: "vae"}
			}
			resolvedRequest := request
			resolvedRequest.BoardID = test.board
			actual, err := upscale.Compile(upscale.Resolution{Request: resolvedRequest, Models: resolvedModels, Seed: 42}, images.Reference{ImageName: "source.png", Width: 513, Height: 513})
			if err != nil {
				t.Fatal(err)
			}
			assertMatchesFixture(t, actual, test.name)
		})
	}
}

// These payloads record the no-LoRA SD1.5 branch of the installed stock
// InvokeAI 6.14.1 Upscale builder with the bundled VAE and a VAE override.
func TestSD1GraphMatchesVersionedEnqueueFixtures(t *testing.T) {
	models := upscale.Models{
		Main:           graphops.ModelIdentifier{Key: "main", Hash: "main-hash", Name: "SD1.5", Base: "sd-1", Type: "main", Variant: "normal"},
		UpscaleModel:   graphops.ModelIdentifier{Key: "spandrel", Hash: "spandrel-hash", Name: "RealESRGAN", Base: "any", Type: "spandrel_image_to_image"},
		TileControlNet: graphops.ModelIdentifier{Key: "controlnet", Hash: "controlnet-hash", Name: "Tile", Base: "sd-1", Type: "controlnet"},
	}
	request := upscale.Request{
		SchemaVersion: 1, Source: upscale.Source{Type: "image", Reference: "source.png"}, Model: "main",
		PositivePrompt: "mountain landscape", NegativePrompt: "text", Scale: new(2), Creativity: new(0), Structure: new(0),
		Steps: new(30), Scheduler: new("kdpm_2"), Guidance: new(2.0), Seed: new(uint32(42)), TileSize: new(1024), TileOverlap: new(128),
	}
	for _, test := range []struct {
		name string
		vae  bool
	}{
		{name: "sd1_6_14_enqueue.json"},
		{name: "sd1_6_14_vae_enqueue.json", vae: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolvedModels := models
			if test.vae {
				resolvedModels.VAE = graphops.ModelIdentifier{Key: "vae", Hash: "vae-hash", Name: "Override", Base: "sd-1", Type: "vae"}
			}
			actual, err := upscale.Compile(upscale.Resolution{Request: request, Models: resolvedModels, Seed: 42}, images.Reference{ImageName: "source.png", Width: 513, Height: 513})
			if err != nil {
				t.Fatal(err)
			}
			assertMatchesFixture(t, actual, test.name)
		})
	}
}

func assertMatchesFixture(t *testing.T, actual graphops.EnqueueRequest, name string) {
	t.Helper()
	encoded, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(golden, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("compiled enqueue differs from %s", name)
	}
}

func TestUpscaleInvocationVocabulariesMatchInvokeAI614Fixture(t *testing.T) {
	document, err := os.ReadFile("../doctor/testdata/invokeai_6_14_anima_openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openapi.json" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(document)
	}))
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []capability.Entry{capability.SDXLUpscaleEntry(), capability.SD1UpscaleEntry()} {
		if err := graphops.CheckInvocations(t.Context(), client, entry.Invocations); err != nil {
			t.Fatalf("%s: %v", entry.Family, err)
		}
	}
	var vocabulary struct {
		Components struct {
			Schemas map[string]struct {
				AdditionalProperties bool `json:"additionalProperties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(document, &vocabulary); err != nil {
		t.Fatal(err)
	}
	if !vocabulary.Components.Schemas["CoreMetadataInvocation"].AdditionalProperties {
		t.Fatal("CoreMetadataInvocation must allow stock upscale metadata fields")
	}
}
