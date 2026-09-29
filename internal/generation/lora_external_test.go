package generation_test

import (
	"bytes"
	json "encoding/json/v2"
	"errors"
	"math"
	"os"
	"reflect"
	"testing"

	"github.com/avienor/bediz/internal/generation"
	"github.com/avienor/bediz/internal/images"
	"github.com/avienor/bediz/internal/operation"
	"github.com/avienor/bediz/internal/sourceimage"
)

func TestResolveSDXLLoRAUsesRecordedAndFallbackWeightsInRequestOrder(t *testing.T) {
	first := generation.ModelIdentifier{Key: "lora-a", Hash: "hash-a", Name: "Alien Style", Base: "sdxl", Type: "lora", DefaultSettings: &generation.ModelDefaultSettings{Weight: new(1.25)}}
	second := generation.ModelIdentifier{Key: "lora-b", Hash: "hash-b", Name: "Noodles Style", Base: "sdxl", Type: "lora"}
	request := generation.Request{SchemaVersion: 1, Model: sdxlMain.Key, PositivePrompt: "alienzkin", Loras: []generation.LoRA{{Model: first.Name}, {Model: second.Key}}}
	resolved, err := generation.Resolve(request, []generation.ModelIdentifier{sdxlMain, first, second}, bytes.NewReader([]byte{1, 0, 0, 0}))
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Loras) != 2 || resolved.Loras[0].Model.Key != first.Key || resolved.Loras[0].Weight != 1.25 || resolved.Loras[1].Model.Key != second.Key || resolved.Loras[1].Weight != 0.75 {
		t.Fatalf("resolved LoRAs = %#v", resolved.Loras)
	}
	if request.Loras[0].Weight != nil || request.Loras[1].Weight != nil {
		t.Fatalf("submitted weights changed: %#v", request.Loras)
	}
}

func TestResolveSDXLLoRARejectsDuplicateModelKeys(t *testing.T) {
	lora := generation.ModelIdentifier{Key: "lora-a", Hash: "hash-a", Name: "Alien Style", Base: "sdxl", Type: "lora"}
	request := generation.Request{SchemaVersion: 1, Model: sdxlMain.Key, PositivePrompt: "test", Seed: new(uint32(1)), Loras: []generation.LoRA{{Model: lora.Key}, {Model: lora.Name}}}
	_, err := generation.Resolve(request, []generation.ModelIdentifier{sdxlMain, lora}, bytes.NewReader(nil))
	invalid, ok := errors.AsType[*operation.InvalidRequestError](err)
	if !ok || invalid.Field != "loras" {
		t.Fatalf("duplicate LoRA error = %v", err)
	}
}

func TestCompileSDXLAppliesTwoLoRAsInOrder(t *testing.T) {
	first := generation.ModelIdentifier{Key: "lora-a", Hash: "hash-a", Name: "Alien Style", Base: "sdxl", Type: "lora"}
	second := generation.ModelIdentifier{Key: "lora-b", Hash: "hash-b", Name: "Noodles Style", Base: "sdxl", Type: "lora"}
	request := generation.Request{SchemaVersion: 1, Model: sdxlMain.Key, PositivePrompt: "alienzkin", Width: new(768), Height: new(768), Steps: new(24), Scheduler: new("heun"), Guidance: new(6.5), Seed: new(uint32(41)), OutputCount: new(1), Loras: []generation.LoRA{{Model: first.Key, Weight: new(1.0)}, {Model: second.Key, Weight: new(0.5)}}}
	resolved, err := generation.Resolve(request, []generation.ModelIdentifier{sdxlMain, first, second}, bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := generation.Compile(resolved)
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
	if nodes["lora_0"].(map[string]any)["lora"].(map[string]any)["key"] != first.Key || nodes["lora_1"].(map[string]any)["lora"].(map[string]any)["key"] != second.Key {
		t.Fatalf("LoRA nodes = %#v", nodes)
	}
	metadata := nodes["metadata"].(map[string]any)["loras"].([]any)
	if len(metadata) != 2 || metadata[0].(map[string]any)["weight"] != 1.0 || metadata[1].(map[string]any)["weight"] != 0.5 {
		t.Fatalf("LoRA metadata = %#v", metadata)
	}
	wantEdges := map[string]bool{
		"model_loader:unet>lora_0:unet": false, "model_loader:clip>lora_0:clip": false, "model_loader:clip2>lora_0:clip2": false,
		"lora_0:unet>lora_1:unet": false, "lora_0:clip>lora_1:clip": false, "lora_0:clip2>lora_1:clip2": false,
		"lora_1:unet>denoise:unet": false, "lora_1:clip>positive_conditioning:clip": false, "lora_1:clip>negative_conditioning:clip": false,
		"lora_1:clip2>positive_conditioning:clip2": false, "lora_1:clip2>negative_conditioning:clip2": false,
	}
	for _, raw := range graph["edges"].([]any) {
		edge := raw.(map[string]any)
		from := edge["source"].(map[string]any)
		to := edge["destination"].(map[string]any)
		key := from["node_id"].(string) + ":" + from["field"].(string) + ">" + to["node_id"].(string) + ":" + to["field"].(string)
		if _, ok := wantEdges[key]; ok {
			wantEdges[key] = true
		}
		if from["node_id"] == "model_loader" && (from["field"] == "unet" || from["field"] == "clip" || from["field"] == "clip2") && to["node_id"] != "lora_0" {
			t.Fatalf("unrouted model output: %s", key)
		}
	}
	for key, found := range wantEdges {
		if !found {
			t.Errorf("missing edge %s", key)
		}
	}
}

func TestResolveLoRARejectsInvalidLocalAndModelSelections(t *testing.T) {
	valid := generation.ModelIdentifier{Key: "lora-a", Hash: "hash-a", Name: "Alien Style", Base: "sdxl", Type: "lora"}
	other := valid
	other.Key, other.Hash = "lora-b", "hash-b"
	wrongBase := valid
	wrongBase.Key, wrongBase.Name, wrongBase.Base = "lora-flux", "FLUX LoRA", "flux"
	badDefault := valid
	badDefault.Key, badDefault.Name, badDefault.DefaultSettings = "bad-default", "Bad Default", &generation.ModelDefaultSettings{Weight: new(11.0)}
	for _, test := range []struct {
		name                string
		loras               []generation.LoRA
		wantField, wantKind string
	}{
		{"empty list", []generation.LoRA{}, "loras", "invalid"},
		{"empty selector", []generation.LoRA{{Model: ""}}, "loras.0.model", "invalid"},
		{"low weight", []generation.LoRA{{Model: valid.Key, Weight: new(-10.1)}}, "loras.0.weight", "invalid"},
		{"high weight", []generation.LoRA{{Model: valid.Key, Weight: new(10.1)}}, "loras.0.weight", "invalid"},
		{"nonfinite weight", []generation.LoRA{{Model: valid.Key, Weight: new(math.Inf(1))}}, "loras.0.weight", "invalid"},
		{"unknown selector", []generation.LoRA{{Model: "missing"}}, "loras.0.model", "invalid"},
		{"ambiguous name", []generation.LoRA{{Model: valid.Name}}, "", "selection"},
		{"wrong type", []generation.LoRA{{Model: sdxlVAE.Key}}, "", "unsupported"},
		{"wrong base", []generation.LoRA{{Model: wrongBase.Key}}, "", "unsupported"},
		{"recorded default outside bounds", []generation.LoRA{{Model: badDefault.Key}}, "loras.0.weight", "invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := generation.Request{SchemaVersion: 1, Model: sdxlMain.Key, PositivePrompt: "test", Seed: new(uint32(1)), Loras: test.loras}
			_, err := generation.Resolve(request, []generation.ModelIdentifier{sdxlMain, sdxlVAE, valid, other, wrongBase, badDefault}, bytes.NewReader(nil))
			switch test.wantKind {
			case "invalid":
				invalid, ok := errors.AsType[*operation.InvalidRequestError](err)
				if !ok || invalid.Field != test.wantField {
					t.Fatalf("error = %v, want invalid field %s", err, test.wantField)
				}
			case "selection":
				selection, ok := errors.AsType[*operation.SelectionRequiredError](err)
				if !ok || selection.Kind != "lora" || len(selection.Candidates) != 2 || selection.Candidates[0].Key != valid.Key || selection.Candidates[1].Key != other.Key {
					t.Fatalf("selection = %#v", selection)
				}
			case "unsupported":
				if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok {
					t.Fatalf("error = %v, want unsupported capability", err)
				}
			}
		})
	}
}

func TestResolveLoRARejectsUnregisteredFamilies(t *testing.T) {
	for _, main := range []generation.ModelIdentifier{{Key: "flux-main", Hash: "hash", Name: "FLUX", Base: "flux", Type: "main", Variant: "dev", Format: "checkpoint"}} {
		request := generation.Request{SchemaVersion: 1, Model: main.Key, PositivePrompt: "test", Seed: new(uint32(1)), Loras: []generation.LoRA{{Model: "anything"}}}
		_, err := generation.Resolve(request, []generation.ModelIdentifier{main}, bytes.NewReader(nil))
		if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok {
			t.Fatalf("%s: error = %v", main.Base, err)
		}
	}
}

func TestResolveAnimaLoRAUsesMatchingInstalledModel(t *testing.T) {
	main := generation.ModelIdentifier{Key: "anima-main", Hash: "main-hash", Name: "Anima", Base: "anima", Type: "main"}
	vae := generation.ModelIdentifier{Key: "anima-vae", Hash: "vae-hash", Name: "Anima VAE", Base: "anima", Type: "vae"}
	encoder := generation.ModelIdentifier{Key: "qwen3", Hash: "encoder-hash", Name: "Qwen3", Base: "any", Type: "qwen3_encoder"}
	otherVAE := generation.ModelIdentifier{Key: "other-vae", Hash: "other-vae-hash", Name: "Other Anima VAE", Base: "anima", Type: "vae"}
	otherEncoder := generation.ModelIdentifier{Key: "other-qwen3", Hash: "other-encoder-hash", Name: "Other Qwen3", Base: "any", Type: "qwen3_encoder"}
	lora := generation.ModelIdentifier{Key: "anima-lora", Hash: "lora-hash", Name: "Detail Tweaker", Base: "anima", Type: "lora", DefaultSettings: &generation.ModelDefaultSettings{Weight: new(1.25)}}
	request := generation.Request{SchemaVersion: 1, Model: main.Key, PositivePrompt: "test", Seed: new(uint32(41)), Components: &generation.Components{VAE: new(vae.Key), Qwen3Encoder: new(encoder.Key)}, Loras: []generation.LoRA{{Model: lora.Name}}}
	inventory := []generation.ModelIdentifier{main, vae, otherVAE, encoder, otherEncoder, lora}
	resolved, err := generation.Resolve(request, inventory, bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Models.VAE.Key != vae.Key || resolved.Models.Qwen3Encoder.Key != encoder.Key {
		t.Fatalf("resolved components = %#v", resolved.Models)
	}
	withoutLoRA := request
	withoutLoRA.Loras = nil
	baseline, err := generation.Resolve(withoutLoRA, inventory, bytes.NewReader(nil))
	if err != nil || !reflect.DeepEqual(resolved.Models, baseline.Models) {
		t.Fatalf("LoRA changed component resolution: with=%#v without=%#v err=%v", resolved.Models, baseline.Models, err)
	}
	if len(resolved.Loras) != 1 || resolved.Loras[0].Model.Key != lora.Key || resolved.Loras[0].Weight != 1.25 {
		t.Fatalf("resolved LoRAs = %#v", resolved.Loras)
	}
}

func TestCompileAnimaChainsLoRAsThroughBothTextEncoders(t *testing.T) {
	main := generation.ModelIdentifier{Key: "main-key", Hash: "blake3:main", Name: "Anima Main", Base: "anima", Type: "main"}
	vae := generation.ModelIdentifier{Key: "vae-key", Hash: "blake3:vae", Name: "Anima VAE", Base: "anima", Type: "vae"}
	encoder := generation.ModelIdentifier{Key: "encoder-key", Hash: "blake3:encoder", Name: "Qwen3 Encoder", Base: "any", Type: "qwen3_encoder"}
	first := generation.ModelIdentifier{Key: "lora-a", Hash: "hash-a", Name: "Detail Tweaker", Base: "anima", Type: "lora"}
	second := generation.ModelIdentifier{Key: "lora-b", Hash: "hash-b", Name: "Second LoRA", Base: "anima", Type: "lora"}
	request := generation.Request{SchemaVersion: 1, Model: main.Key, PositivePrompt: "a lighthouse", NegativePrompt: "text", Width: new(768), Height: new(768), Steps: new(24), Scheduler: new("heun"), Guidance: new(4.25), Seed: new(uint32(42)), OutputCount: new(1), Loras: []generation.LoRA{{Model: first.Key, Weight: new(1.0)}, {Model: second.Key, Weight: new(0.5)}}}
	resolved, err := generation.Resolve(request, []generation.ModelIdentifier{main, vae, encoder, first, second}, bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := generation.Compile(resolved)
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
	for index, model := range []generation.ModelIdentifier{first, second} {
		id := []string{"lora_0", "lora_1"}[index]
		node, ok := nodes[id].(map[string]any)
		if !ok || node["type"] != "anima_lora_loader" || node["lora"].(map[string]any)["key"] != model.Key {
			t.Fatalf("LoRA node %s = %#v", id, nodes[id])
		}
	}
	metadata := nodes["metadata"].(map[string]any)["loras"].([]any)
	if len(metadata) != 2 || metadata[0].(map[string]any)["weight"] != 1.0 || metadata[1].(map[string]any)["weight"] != 0.5 {
		t.Fatalf("LoRA metadata = %#v", metadata)
	}
	want := map[string]bool{}
	for _, connection := range []string{
		"model_loader:transformer>lora_0:transformer", "model_loader:qwen3_encoder>lora_0:qwen3_encoder",
		"lora_0:transformer>lora_1:transformer", "lora_0:qwen3_encoder>lora_1:qwen3_encoder",
		"lora_1:transformer>denoise:transformer", "lora_1:qwen3_encoder>positive_conditioning:qwen3_encoder",
		"lora_1:qwen3_encoder>negative_conditioning:qwen3_encoder",
	} {
		want[connection] = false
	}
	for _, raw := range graph["edges"].([]any) {
		connection := raw.(map[string]any)
		from := connection["source"].(map[string]any)
		to := connection["destination"].(map[string]any)
		key := from["node_id"].(string) + ":" + from["field"].(string) + ">" + to["node_id"].(string) + ":" + to["field"].(string)
		if _, ok := want[key]; ok {
			want[key] = true
		}
		if from["node_id"] == "model_loader" && to["node_id"] != "lora_0" && (from["field"] == "transformer" || from["field"] == "qwen3_encoder") {
			t.Errorf("unrouted model output: %s", key)
		}
	}
	for connection, found := range want {
		if !found {
			t.Errorf("missing edge %s", connection)
		}
	}
}

func TestCompileAnimaLoRAMatchesInvokeAI614EnqueueFixtures(t *testing.T) {
	main := generation.ModelIdentifier{Key: "main-key", Hash: "blake3:main", Name: "Anima Main", Base: "anima", Type: "main"}
	vae := generation.ModelIdentifier{Key: "vae-key", Hash: "blake3:vae", Name: "Anima VAE", Base: "anima", Type: "vae"}
	encoder := generation.ModelIdentifier{Key: "encoder-key", Hash: "blake3:encoder", Name: "Qwen3 Encoder", Base: "any", Type: "qwen3_encoder"}
	first := generation.ModelIdentifier{Key: "lora-a", Hash: "hash-a", Name: "Detail Tweaker", Base: "anima", Type: "lora"}
	second := generation.ModelIdentifier{Key: "lora-b", Hash: "hash-b", Name: "Second LoRA", Base: "anima", Type: "lora"}
	for _, test := range []struct {
		name, fixture string
		two, img2img  bool
	}{
		{"one", "testdata/anima_6_14_lora_one_enqueue.json", false, false},
		{"two", "testdata/anima_6_14_lora_two_enqueue.json", true, false},
		{"image-to-image", "testdata/anima_6_14_lora_img2img_enqueue.json", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := generation.Request{SchemaVersion: 1, Model: main.Key, PositivePrompt: "a lighthouse in a storm", NegativePrompt: "text", Width: new(768), Height: new(1024), Steps: new(24), Scheduler: new("heun"), Guidance: new(4.25), Seed: new(uint32(42)), OutputCount: new(1), BoardID: "board-1", Loras: []generation.LoRA{{Model: first.Key, Weight: new(1.0)}}}
			if test.two {
				request.Loras = append(request.Loras, generation.LoRA{Model: second.Key, Weight: new(0.5)})
			}
			if test.img2img {
				request.Source = &sourceimage.Source{Type: "image", Reference: "source.png"}
				request.Strength = new(0.6)
			}
			resolved, err := generation.Resolve(request, []generation.ModelIdentifier{main, vae, encoder, first, second}, bytes.NewReader(nil))
			if err != nil {
				t.Fatal(err)
			}
			if test.img2img {
				resolved.SourceImage = images.Reference{ImageName: "source.png", Width: 768, Height: 1024}
			}
			compiled, err := generation.Compile(resolved)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(compiled)
			if err != nil {
				t.Fatal(err)
			}
			fixture, err := os.ReadFile(test.fixture)
			if err != nil {
				t.Fatal(err)
			}
			var got, want any
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(fixture, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("enqueue differs from %s\ngot: %s\nwant: %s", test.fixture, encoded, fixture)
			}
		})
	}
}

func TestCompileSDXLLoRAMatchesInvokeAI614EnqueueFixtures(t *testing.T) {
	first := generation.ModelIdentifier{Key: "lora-a", Hash: "hash-a", Name: "Alien Style", Base: "sdxl", Type: "lora"}
	second := generation.ModelIdentifier{Key: "lora-b", Hash: "hash-b", Name: "Noodles Style", Base: "sdxl", Type: "lora"}
	for _, test := range []struct {
		name, fixture     string
		two, vae, img2img bool
	}{
		{"one", "testdata/sdxl_6_14_lora_one_enqueue.json", false, false, false},
		{"two", "testdata/sdxl_6_14_lora_two_enqueue.json", true, false, false},
		{"vae override", "testdata/sdxl_6_14_lora_vae_enqueue.json", false, true, false},
		{"image-to-image", "testdata/sdxl_6_14_lora_img2img_enqueue.json", false, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := generation.Request{SchemaVersion: 1, Model: sdxlMain.Key, PositivePrompt: "a lighthouse", NegativePrompt: "text", Width: new(768), Height: new(512), Steps: new(24), Scheduler: new("heun"), Guidance: new(6.5), Seed: new(uint32(41)), OutputCount: new(1), Loras: []generation.LoRA{{Model: first.Key, Weight: new(1.0)}}}
			if test.two {
				request.Loras = append(request.Loras, generation.LoRA{Model: second.Key, Weight: new(0.5)})
			}
			if test.vae {
				request.Components = &generation.Components{VAE: new(sdxlVAE.Key)}
			}
			if test.img2img {
				request.Source = &sourceimage.Source{Type: "image", Reference: "source.png"}
				request.Strength = new(0.6)
			}
			resolved, err := generation.Resolve(request, []generation.ModelIdentifier{sdxlMain, sdxlVAE, first, second}, bytes.NewReader(nil))
			if err != nil {
				t.Fatal(err)
			}
			if test.img2img {
				resolved.SourceImage = images.Reference{ImageName: "source.png", Width: 768, Height: 512}
			}
			compiled, err := generation.Compile(resolved)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(compiled)
			if err != nil {
				t.Fatal(err)
			}
			fixture, err := os.ReadFile(test.fixture)
			if err != nil {
				t.Fatal(err)
			}
			var got, want map[string]any
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(fixture, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				gotGraph := got["batch"].(map[string]any)["graph"].(map[string]any)
				wantGraph := want["batch"].(map[string]any)["graph"].(map[string]any)
				for key, value := range gotGraph["nodes"].(map[string]any) {
					if !reflect.DeepEqual(value, wantGraph["nodes"].(map[string]any)[key]) {
						t.Errorf("node %s differs: got=%#v want=%#v", key, value, wantGraph["nodes"].(map[string]any)[key])
					}
				}
				gotEdges := gotGraph["edges"].([]any)
				wantEdges := wantGraph["edges"].([]any)
				for index := range min(len(gotEdges), len(wantEdges)) {
					if !reflect.DeepEqual(gotEdges[index], wantEdges[index]) {
						t.Errorf("edge %d differs: got=%#v want=%#v", index, gotEdges[index], wantEdges[index])
						break
					}
				}
				t.Fatalf("enqueue differs from %s: got edges=%d want edges=%d", test.fixture, len(gotEdges), len(wantEdges))
			}
		})
	}
}
