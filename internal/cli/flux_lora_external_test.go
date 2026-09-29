package cli_test

import (
	"reflect"
	"testing"
)

func TestGenerateFLUXLoRARecordsReceiptAndVariantSynchronization(t *testing.T) {
	for _, main := range []string{"flux-dev", "flux-schnell"} {
		for _, imageToImage := range []bool{false, true} {
			t.Run(main+map[bool]string{false: "/txt2img", true: "/img2img"}[imageToImage], func(t *testing.T) {
				isolateUserConfigDir(t)
				inventory := append(fluxCLIInventory(), map[string]any{"key": "flux-lora", "hash": "lora-hash", "name": "Realism", "base": "flux", "type": "lora", "default_settings": map[string]any{"weight": 1.25}})
				server, requests, graphs := img2imgServer(t, 768, 768, sdxlOpenAPIFixture(t), inventory)
				args := []string{"--no-wait", "--model", main, "--prompt", "a lighthouse", "--seed", "41", "--lora", "Realism", "--url", server.URL}
				wantFields := []any{"scheduler"}
				if main == "flux-dev" {
					wantFields = append(wantFields, "guidance")
				}
				wantFields = append(wantFields, "vae", "t5_encoder", "clip_embed", "output_count", "board_id")
				if imageToImage {
					args = append(args, "--image-path", sourcePNG(t, 768, 768), "--strength", "0.6")
					wantFields = append(wantFields, "source_image", "strength")
				}
				code, envelope := runImg2Img(t, args...)
				if code != 0 || countRequest(*requests, "POST /api/v1/queue/default/enqueue_batch") != 1 {
					t.Fatalf("code=%d envelope=%#v requests=%#v", code, envelope, *requests)
				}
				data := envelope["data"].(map[string]any)
				submitted := data["submitted_request"].(map[string]any)["loras"].([]any)
				resolved := data["resolved_settings"].(map[string]any)["loras"].([]any)
				if !reflect.DeepEqual(submitted, []any{map[string]any{"model": "Realism"}}) || !reflect.DeepEqual(resolved, []any{map[string]any{"model_key": "flux-lora", "weight": 1.25}}) {
					t.Fatalf("receipt LoRAs: submitted=%#v resolved=%#v", submitted, resolved)
				}
				fields := envelope["warnings"].([]any)[0].(map[string]any)["details"].(map[string]any)["not_restored"]
				if !reflect.DeepEqual(fields, wantFields) || !reflect.DeepEqual(data["warnings"], envelope["warnings"]) {
					t.Fatalf("not_restored=%#v want=%#v", fields, wantFields)
				}
				nodes := (*graphs)["batch"].(map[string]any)["graph"].(map[string]any)["nodes"].(map[string]any)
				if nodes["lora_0"].(map[string]any)["type"] != "flux_lora_loader" || nodes["lora_0"].(map[string]any)["weight"] != 1.25 {
					t.Fatalf("LoRA graph = %#v", nodes)
				}
				// A LoRA-less receipt and graph must omit all LoRA members.
				argsWithoutLoRA := append(append([]string{}, args[:7]...), args[9:]...)
				clear(*graphs)
				code, envelope = runImg2Img(t, argsWithoutLoRA...)
				if code != 0 {
					t.Fatalf("LoRA-less generation: code=%d envelope=%#v", code, envelope)
				}
				data = envelope["data"].(map[string]any)
				for _, key := range []string{"submitted_request", "resolved_settings"} {
					if _, found := data[key].(map[string]any)["loras"]; found {
						t.Fatalf("LoRA-less receipt contains %s.loras", key)
					}
				}
				fields = envelope["warnings"].([]any)[0].(map[string]any)["details"].(map[string]any)["not_restored"]
				if !reflect.DeepEqual(fields, wantFields) {
					t.Fatalf("LoRA-less not_restored = %#v", fields)
				}
				nodes = (*graphs)["batch"].(map[string]any)["graph"].(map[string]any)["nodes"].(map[string]any)
				if nodes["lora_0"] != nil || nodes["metadata"].(map[string]any)["loras"] != nil {
					t.Fatalf("LoRA-less graph contains LoRAs: %#v", nodes)
				}
			})
		}
	}
}

func TestGenerateFLUXLoRARejectsIncompatibleModelsAndSettingsBeforeUpload(t *testing.T) {
	for _, test := range []struct {
		name, main, base, modelType, variant, format, negative, guidance string
		code                                                             int
	}{
		{name: "flux2 LoRA", main: "flux-dev", base: "flux2", modelType: "lora", code: 4},
		{name: "other base", main: "flux-dev", base: "sdxl", modelType: "lora", code: 4},
		{name: "control LoRA", main: "flux-dev", base: "flux", modelType: "control_lora", code: 4},
		{name: "other type", main: "flux-dev", base: "flux", modelType: "main", code: 4},
		{name: "unsupported variant", main: "flux-dev", base: "flux", modelType: "lora", variant: "dev_fill", code: 4},
		{name: "unsupported format", main: "flux-dev", base: "flux", modelType: "lora", format: "diffusers", code: 4},
		{name: "dev negative prompt", main: "flux-dev", base: "flux", modelType: "lora", negative: "blurry", code: 2},
		{name: "schnell negative prompt", main: "flux-schnell", base: "flux", modelType: "lora", negative: "blurry", code: 2},
		{name: "schnell guidance", main: "flux-schnell", base: "flux", modelType: "lora", guidance: "4", code: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			inventory := fluxCLIInventory()
			if test.variant != "" {
				inventory[0]["variant"] = test.variant
			}
			if test.format != "" {
				inventory[0]["format"] = test.format
			}
			inventory = append(inventory, map[string]any{"key": "selected-lora", "hash": "hash", "name": "Selected LoRA", "base": test.base, "type": test.modelType})
			server, requests, _ := img2imgServer(t, 768, 768, sdxlOpenAPIFixture(t), inventory)
			args := []string{"--no-wait", "--model", test.main, "--prompt", "test", "--image-path", sourcePNG(t, 768, 768), "--lora", "selected-lora", "--url", server.URL}
			if test.negative != "" {
				args = append(args, "--negative-prompt", test.negative)
			}
			if test.guidance != "" {
				args = append(args, "--guidance", test.guidance)
			}
			code, envelope := runImg2Img(t, args...)
			wantError := "unsupported_capability"
			if test.code == 2 {
				wantError = "invalid_request"
			}
			if code != test.code || envelope["error"].(map[string]any)["code"] != wantError || countRequest(*requests, "POST /api/v1/images/upload") != 0 || countRequest(*requests, "POST /api/v1/queue/default/enqueue_batch") != 0 {
				t.Fatalf("code=%d envelope=%#v requests=%#v", code, envelope, *requests)
			}
		})
	}
}

func TestGenerateFLUXLoRAPreflightRejectsMissingVocabularyBeforeUploadOrEnqueue(t *testing.T) {
	for _, property := range []string{"", "lora", "weight", "transformer", "clip", "t5_encoder", "id", "is_intermediate", "use_cache", "type", "metadata.loras"} {
		t.Run(property, func(t *testing.T) {
			isolateUserConfigDir(t)
			openAPI := sdxlOpenAPIFixture(t)
			schemas := openAPI["components"].(map[string]any)["schemas"].(map[string]any)
			if property == "" {
				delete(schemas, "FluxLoRALoaderInvocation")
			} else if property == "metadata.loras" {
				delete(schemas["CoreMetadataInvocation"].(map[string]any)["properties"].(map[string]any), "loras")
			} else {
				delete(schemas["FluxLoRALoaderInvocation"].(map[string]any)["properties"].(map[string]any), property)
			}
			inventory := append(fluxCLIInventory(), map[string]any{"key": "flux-lora", "hash": "lora-hash", "name": "Realism", "base": "flux", "type": "lora"})
			server, requests, _ := img2imgServer(t, 768, 768, openAPI, inventory)
			code, envelope := runImg2Img(t, "--no-wait", "--model", "flux-dev", "--prompt", "test", "--image-path", sourcePNG(t, 768, 768), "--lora", "flux-lora", "--url", server.URL)
			if code != 4 || envelope["error"].(map[string]any)["code"] != "unsupported_capability" || countRequest(*requests, "POST /api/v1/images/upload") != 0 || countRequest(*requests, "POST /api/v1/queue/default/enqueue_batch") != 0 {
				t.Fatalf("code=%d envelope=%#v requests=%#v", code, envelope, *requests)
			}
			code, envelope = runImg2Img(t, "--no-wait", "--model", "flux-dev", "--prompt", "test", "--image-path", sourcePNG(t, 768, 768), "--url", server.URL)
			if code != 0 || countRequest(*requests, "POST /api/v1/images/upload") != 1 || countRequest(*requests, "POST /api/v1/queue/default/enqueue_batch") != 1 {
				t.Fatalf("LoRA-less request affected: code=%d envelope=%#v requests=%#v", code, envelope, *requests)
			}
		})
	}
}
