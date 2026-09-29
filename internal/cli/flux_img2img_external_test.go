package cli_test

import (
	"bytes"
	json "encoding/json/v2"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

func TestGenerateFLUXImageSourceResolvesSizeAndReceipt(t *testing.T) {
	for _, model := range []string{"flux-dev", "flux-schnell"} {
		t.Run(model, func(t *testing.T) {
			isolateUserConfigDir(t)
			server, requests, graph := img2imgServer(t, 1001, 750, sdxlOpenAPIFixture(t), fluxCLIInventory())
			code, envelope := runImg2Img(t, "--no-wait", "--model", model, "--prompt", "lighthouse", "--image", "source.png", "--seed", "41", "--url", server.URL)
			if code != 0 || envelope["ok"] != true {
				t.Fatalf("code=%d envelope=%#v", code, envelope)
			}
			data := envelope["data"].(map[string]any)
			settings := data["resolved_settings"].(map[string]any)
			if settings["width"] != float64(992) || settings["height"] != float64(736) || settings["strength"] != 0.75 || data["source_uploaded"] != false || data["source_image"].(map[string]any)["image_name"] != "source.png" {
				t.Fatalf("receipt=%#v", data)
			}
			components := settings["component_keys"].(map[string]any)
			if components["vae"] != "flux-vae" || components["t5_encoder"] != "flux-t5" || components["clip_embed"] != "flux-clip" {
				t.Fatalf("components=%#v", components)
			}
			nodes := (*graph)["batch"].(map[string]any)["graph"].(map[string]any)["nodes"].(map[string]any)
			if nodes["resize"].(map[string]any)["width"] != float64(992) || nodes["resize"].(map[string]any)["height"] != float64(736) || nodes["metadata"].(map[string]any)["generation_mode"] != "flux_img2img" || nodes["denoise"].(map[string]any)["denoising_start"] != 0.05591248870509802 {
				t.Fatalf("graph=%#v", nodes)
			}
			if slices.Contains(*requests, uploadRequest) || countRequest(*requests, enqueueRequest) != 1 {
				t.Fatalf("requests=%q", *requests)
			}
			fields := []any{"scheduler"}
			if model == "flux-dev" {
				fields = append(fields, "guidance")
			}
			fields = append(fields, "vae", "t5_encoder", "clip_embed", "output_count", "board_id", "source_image", "strength")
			warnings := envelope["warnings"].([]any)
			if len(warnings) != 1 || warnings[0].(map[string]any)["code"] != "ui_sync_partial" || !slices.Equal(warnings[0].(map[string]any)["details"].(map[string]any)["not_restored"].([]any), fields) {
				t.Fatalf("warnings=%#v, want not_restored=%#v", warnings, fields)
			}
		})
	}
}

func fluxDoctorReport(t *testing.T, url string) map[string]any {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.New(&stdout, &stderr).Run(t.Context(), []string{"doctor", "--url", url, "--json"})
	if code != result.ExitUnsupportedCapability || stderr.Len() != 0 {
		t.Fatalf("doctor code=%d stderr=%q stdout=%s", code, stderr.String(), stdout.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope["error"].(map[string]any)["details"].(map[string]any)["report"].(map[string]any)
}

func fluxDoctorModes(report map[string]any) map[string]map[string]any {
	modes := map[string]map[string]any{}
	for _, raw := range report["capabilities"].([]any) {
		entry := raw.(map[string]any)
		if entry["operation"] == "generate" && entry["family"] == "flux" && entry["setting"] == nil {
			modes[entry["mode"].(string)] = entry
		}
	}
	return modes
}

func TestFLUXImageToImageVocabularyGuardsGenerateAndDoctor(t *testing.T) {
	type requirement struct {
		schema, property, endpoint string
		textRequired               bool
	}
	var cases []requirement
	for _, schema := range []string{"ImageResizeInvocation", "FluxVaeEncodeInvocation"} {
		cases = append(cases, requirement{schema: schema})
		fields := "id is_intermediate use_cache type image vae"
		if schema == "ImageResizeInvocation" {
			fields = "id is_intermediate use_cache type image width height resample_mode"
		}
		for field := range strings.FieldsSeq(fields) {
			cases = append(cases, requirement{schema: schema, property: field})
		}
	}
	for _, field := range []string{"latents", "denoising_start", "denoising_end", "add_noise"} {
		cases = append(cases, requirement{schema: "FluxDenoiseInvocation", property: field})
	}
	cases = append(cases, requirement{schema: "CoreMetadataInvocation", property: "strength"}, requirement{schema: "CoreMetadataInvocation", property: "init_image"}, requirement{endpoint: "/api/v1/images/upload"})
	for _, schema := range []struct{ name, fields string }{
		{"FluxModelLoaderInvocation", "model vae_model t5_encoder_model clip_embed_model"},
		{"FluxTextEncoderInvocation", "clip t5_encoder t5_max_seq_len prompt"},
		{"FluxDenoiseInvocation", "transformer positive_text_conditioning cfg_scale width height num_steps scheduler guidance seed"},
		{"FluxVaeDecodeInvocation", "latents vae metadata board"},
		{"CoreMetadataInvocation", "generation_mode positive_prompt negative_prompt seed width height steps scheduler cfg_scale model vae"},
		{"StringInvocation", "value"},
		{"IntegerInvocation", "value"},
	} {
		cases = append(cases, requirement{schema: schema.name, textRequired: true})
		for field := range strings.FieldsSeq("id is_intermediate use_cache type " + schema.fields) {
			cases = append(cases, requirement{schema: schema.name, property: field, textRequired: true})
		}
	}
	cases = append(cases, requirement{endpoint: "/api/v1/images/i/{image_name}", textRequired: true})
	for _, tc := range cases {
		t.Run(tc.schema+"/"+tc.property+tc.endpoint, func(t *testing.T) {
			isolateUserConfigDir(t)
			document := sdxlOpenAPIFixture(t)
			if tc.endpoint != "" {
				delete(document["paths"].(map[string]any), tc.endpoint)
			} else {
				schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
				if tc.property == "" {
					delete(schemas, tc.schema)
				} else {
					delete(schemas[tc.schema].(map[string]any)["properties"].(map[string]any), tc.property)
				}
			}
			server, requests, _ := img2imgServer(t, 32, 32, document, fluxCLIInventory())
			code, envelope := runImg2Img(t, "--no-wait", "--model", "flux-dev", "--prompt", "lighthouse", "--image-path", sourcePNG(t, 32, 32), "--seed", "41", "--url", server.URL)
			if code != result.ExitUnsupportedCapability || envelope["error"].(map[string]any)["code"] != "unsupported_capability" || slices.Contains(*requests, uploadRequest) || slices.Contains(*requests, enqueueRequest) {
				t.Fatalf("code=%d envelope=%#v requests=%q", code, envelope, *requests)
			}
			if tc.property != "" && !strings.Contains(envelope["error"].(map[string]any)["message"].(string), tc.property) {
				t.Fatalf("generate did not name missing property: %#v", envelope)
			}
			if tc.schema != "" && tc.property == "" && !strings.Contains(envelope["error"].(map[string]any)["message"].(string), tc.schema) {
				t.Fatalf("generate did not name missing schema: %#v", envelope)
			}
			report := fluxDoctorReport(t, server.URL)
			modes := fluxDoctorModes(report)
			if modes["img2img"] == nil || modes["img2img"]["compatible"] != false || modes["txt2img"]["compatible"] != !tc.textRequired {
				t.Fatalf("FLUX.1 doctor modes=%#v", modes)
			}
			failure := "missing_endpoint:POST " + tc.endpoint
			if tc.endpoint == "/api/v1/images/i/{image_name}" {
				failure = "missing_endpoint:GET " + tc.endpoint
			}
			if tc.schema != "" {
				failure = "incompatible_invocation:" + map[string]string{"ImageResizeInvocation": "img_resize", "FluxVaeEncodeInvocation": "flux_vae_encode", "FluxDenoiseInvocation": "flux_denoise", "CoreMetadataInvocation": "core_metadata", "FluxModelLoaderInvocation": "flux_model_loader", "FluxTextEncoderInvocation": "flux_text_encoder", "FluxVaeDecodeInvocation": "flux_vae_decode", "StringInvocation": "string", "IntegerInvocation": "integer"}[tc.schema]
			}
			if !slices.Contains(modes["img2img"]["failures"].([]any), any(failure)) {
				t.Fatalf("doctor failures=%#v, want %q", modes["img2img"], failure)
			}
			if tc.property != "" {
				found := false
				for _, raw := range report["openapi"].(map[string]any)["required_invocations"].([]any) {
					check := raw.(map[string]any)
					if check["schema"] == tc.schema && slices.Contains(check["missing_properties"].([]any), any(tc.property)) {
						found = true
					}
				}
				if !found {
					t.Fatalf("doctor did not name missing %s.%s", tc.schema, tc.property)
				}
			}
		})
	}
}

func TestGenerateFLUXTooSmallSourceNeverUploadsOrEnqueues(t *testing.T) {
	for _, source := range []string{"path", "image"} {
		for _, size := range [][2]int{{15, 16}, {16, 15}} {
			t.Run(fmt.Sprintf("%s/%dx%d", source, size[0], size[1]), func(t *testing.T) {
				isolateUserConfigDir(t)
				server, requests, _ := img2imgServer(t, size[0], size[1], sdxlOpenAPIFixture(t), fluxCLIInventory())
				args := []string{"--no-wait", "--model", "flux-dev", "--prompt", "lighthouse", "--width", "768", "--height", "768", "--seed", "41", "--url", server.URL}
				if source == "path" {
					args = append(args, "--image-path", sourcePNG(t, size[0], size[1]))
				} else {
					args = append(args, "--image", "source.png")
				}
				code, envelope := runImg2Img(t, args...)
				if code != result.ExitInvalidRequest || envelope["error"].(map[string]any)["code"] != "invalid_request" || slices.Contains(*requests, uploadRequest) || slices.Contains(*requests, enqueueRequest) {
					t.Fatalf("code=%d envelope=%#v requests=%q", code, envelope, *requests)
				}
			})
		}
	}
}

func TestFLUXImageToImageModelContractGuardsGenerateAndDoctor(t *testing.T) {
	type modelCase struct {
		name, variant, format, displayName, wantCode string
		flags                                        []string
	}
	var cases []modelCase
	for _, variant := range []string{"dev", "schnell"} {
		for _, format := range []string{"checkpoint", "bnb_quantized_nf4b", "gguf_quantized"} {
			cases = append(cases, modelCase{name: variant + "/" + format, variant: variant, format: format, displayName: "FLUX.1-Kontext-dev"})
		}
	}
	cases = append(cases,
		modelCase{name: "Krea", variant: "dev", format: "checkpoint", displayName: "FLUX.1-Krea-dev"},
		modelCase{name: "fill", variant: "dev_fill", format: "checkpoint", displayName: "FLUX dev", wantCode: "unsupported_capability"},
		modelCase{name: "unknown variant", variant: "unknown", format: "checkpoint", displayName: "FLUX dev", wantCode: "unsupported_capability"},
		modelCase{name: "SDNQ", variant: "dev", format: "sdnq_quantized", displayName: "FLUX dev", wantCode: "unsupported_capability"},
		modelCase{name: "diffusers", variant: "dev", format: "diffusers", displayName: "FLUX dev", wantCode: "unsupported_capability"},
		modelCase{name: "unknown format", variant: "dev", format: "unknown", displayName: "FLUX dev", wantCode: "unsupported_capability"},
		modelCase{name: "negative prompt", variant: "dev", format: "checkpoint", displayName: "FLUX dev", wantCode: "invalid_request", flags: []string{"--negative-prompt", "bad"}},
		modelCase{name: "schnell guidance", variant: "schnell", format: "checkpoint", displayName: "FLUX schnell", wantCode: "invalid_request", flags: []string{"--guidance", "4"}},
	)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			inventory := fluxCLIInventory()
			main := inventory[0]
			main["variant"], main["format"], main["name"] = tc.variant, tc.format, tc.displayName
			inventory = append([]map[string]any{main}, inventory[2:]...)
			server, requests, _ := img2imgServer(t, 32, 32, sdxlOpenAPIFixture(t), inventory)
			args := []string{"--no-wait", "--model", "flux-dev", "--prompt", "lighthouse", "--image-path", sourcePNG(t, 32, 32), "--seed", "41", "--url", server.URL}
			code, envelope := runImg2Img(t, append(args, tc.flags...)...)
			if tc.wantCode == "" {
				if code != 0 || envelope["ok"] != true || countRequest(*requests, uploadRequest) != 1 || countRequest(*requests, enqueueRequest) != 1 {
					t.Fatalf("code=%d envelope=%#v requests=%q", code, envelope, *requests)
				}
			} else {
				wantExit := result.ExitUnsupportedCapability
				if tc.wantCode == "invalid_request" {
					wantExit = result.ExitInvalidRequest
				}
				if code != wantExit || envelope["error"].(map[string]any)["code"] != tc.wantCode || slices.Contains(*requests, uploadRequest) || slices.Contains(*requests, enqueueRequest) {
					t.Fatalf("code=%d envelope=%#v requests=%q", code, envelope, *requests)
				}
			}
			modes := fluxDoctorModes(fluxDoctorReport(t, server.URL))
			for _, mode := range []string{"txt2img", "img2img"} {
				entry := modes[mode]
				compatible := tc.wantCode != "unsupported_capability"
				if entry == nil || entry["compatible"] != compatible {
					t.Fatalf("%s doctor capability=%#v, want compatible=%v", mode, entry, compatible)
				}
				if compatible && (entry["ui_sync"] != "partial" || len(entry["failures"].([]any)) != 0) {
					t.Fatalf("%s doctor capability=%#v", mode, entry)
				} else if !compatible && !slices.Contains(entry["failures"].([]any), any("missing_component:FLUX.1 main model")) {
					t.Fatalf("%s doctor did not name missing supported main model: %#v", mode, entry)
				}
			}
		})
	}
}

func TestGenerateFLUXPathFlagsAndDocumentResolveSameDimensions(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
		explicit      bool
	}{
		{"source rounding", 992, 736, false},
		{"explicit pair", 768, 512, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			path := sourcePNG(t, 1001, 750)
			server, requests, graph := img2imgServer(t, 1001, 750, sdxlOpenAPIFixture(t), fluxCLIInventory())
			args := []string{"--no-wait", "--model", "flux-dev", "--prompt", "lighthouse", "--image-path", path, "--strength", "0.5", "--seed", "41", "--url", server.URL}
			document := map[string]any{"schema_version": 1, "model": "flux-dev", "positive_prompt": "lighthouse", "source": map[string]any{"type": "path", "reference": path}, "strength": 0.5, "seed": 41}
			if tc.explicit {
				args = append(args, "--width", "768", "--height", "512")
				document["width"], document["height"] = 768, 512
			}
			code, flags := runImg2Img(t, args...)
			if code != 0 {
				t.Fatalf("flags code=%d envelope=%#v", code, flags)
			}
			encoded, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			app := cli.NewWithIO(bytes.NewReader(encoded), &stdout, &stderr)
			code = app.Run(t.Context(), []string{"generate", "--request", "-", "--no-wait", "--url", server.URL, "--json"})
			var fromDocument map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &fromDocument); err != nil {
				t.Fatal(err)
			}
			if code != 0 || stderr.Len() != 0 || !reflect.DeepEqual(flags, fromDocument) {
				t.Fatalf("document code=%d stderr=%s flags=%#v document=%#v", code, stderr.String(), flags, fromDocument)
			}
			data := flags["data"].(map[string]any)
			settings := data["resolved_settings"].(map[string]any)
			if settings["width"] != float64(tc.width) || settings["height"] != float64(tc.height) || settings["strength"] != 0.5 || data["source_uploaded"] != true || data["source_image"].(map[string]any)["image_name"] != "uploaded.png" {
				t.Fatalf("receipt=%#v", data)
			}
			if _, internal := settings["denoising_start"]; internal {
				t.Fatalf("receipt exposes denoising_start: %#v", settings)
			}
			nodes := (*graph)["batch"].(map[string]any)["graph"].(map[string]any)["nodes"].(map[string]any)
			if nodes["resize"].(map[string]any)["width"] != float64(tc.width) || nodes["resize"].(map[string]any)["height"] != float64(tc.height) || nodes["metadata"].(map[string]any)["init_image"] != "uploaded.png" {
				t.Fatalf("graph=%#v", nodes)
			}
			if countRequest(*requests, uploadRequest) != 2 || countRequest(*requests, enqueueRequest) != 2 {
				t.Fatalf("requests=%q", *requests)
			}
		})
	}
}

func TestGenerateFLUXSourceDimensionsOverrideProfileWithoutResize(t *testing.T) {
	isolateUserConfigDir(t)
	createGenerateProfile(t, `{"schema_version":1,"name":"preset","generate":{"model":"flux-dev","width":1024,"height":1024}}`)
	inventory := append(fluxCLIInventory(), map[string]any{"key": "other-vae", "hash": "other-vae-hash", "name": "Other VAE", "base": "flux", "type": "vae"})
	server, _, graph := img2imgServer(t, 768, 512, sdxlOpenAPIFixture(t), inventory)
	code, envelope := runImg2Img(t, "--no-wait", "--profile", "preset", "--vae", "flux-vae", "--prompt", "lighthouse", "--image", "source.png", "--seed", "41", "--url", server.URL)
	if code != 0 || envelope["ok"] != true {
		t.Fatalf("code=%d envelope=%#v", code, envelope)
	}
	settings := envelope["data"].(map[string]any)["resolved_settings"].(map[string]any)
	if settings["width"] != float64(768) || settings["height"] != float64(512) || settings["component_keys"].(map[string]any)["vae"] != "flux-vae" {
		t.Fatalf("settings=%#v", settings)
	}
	nodes := (*graph)["batch"].(map[string]any)["graph"].(map[string]any)["nodes"].(map[string]any)
	if _, resized := nodes["resize"]; resized {
		t.Fatal("aligned source was resized")
	}
	if nodes["i2l"].(map[string]any)["image"].(map[string]any)["image_name"] != "source.png" {
		t.Fatalf("encoder=%#v", nodes["i2l"])
	}
}

func TestDoctorFLUXImageToImageKeepsExecutionCompatibleWithoutRecall(t *testing.T) {
	isolateUserConfigDir(t)
	document := sdxlOpenAPIFixture(t)
	delete(document["paths"].(map[string]any), "/api/v1/recall/{queue_id}")
	server, _, _ := img2imgServer(t, 768, 768, document, fluxCLIInventory())
	modes := fluxDoctorModes(fluxDoctorReport(t, server.URL))
	for _, mode := range []string{"txt2img", "img2img"} {
		entry := modes[mode]
		if entry == nil || entry["compatible"] != true {
			t.Fatalf("%s capability=%#v", mode, entry)
		}
		if _, restored := entry["ui_sync"]; restored {
			t.Fatalf("%s advertises synchronization without Recall: %#v", mode, entry)
		}
	}
}
