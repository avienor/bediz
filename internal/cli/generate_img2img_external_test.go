package cli_test

import (
	"bytes"
	json "encoding/json/v2"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

func sourcePNG(t *testing.T, width, height int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	canvas.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(file, canvas); err != nil {
		t.Fatal(err)
	}
	return path
}

func sourceEncodedFile(t *testing.T, format string, width, height int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source."+format)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	var encodeErr error
	switch format {
	case "jpg":
		encodeErr = jpeg.Encode(file, canvas, nil)
	case "gif":
		encodeErr = gif.Encode(file, canvas, nil)
	}
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	return path
}

func img2imgServer(t *testing.T, sourceWidth, sourceHeight int, document map[string]any, inventory []map[string]any) (*httptest.Server, *[]string, *map[string]any) {
	t.Helper()
	requests := &[]string{}
	graph := &map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*requests = append(*requests, r.Method+" "+r.URL.Path)
		if serveAnimaPreflight(w, r, "6.14.1", document, inventory) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/images/i/source.png":
			_ = json.MarshalWrite(w, upscaleImage("source.png", sourceWidth, sourceHeight))
		case "/api/v1/images/upload":
			w.WriteHeader(http.StatusCreated)
			_ = json.MarshalWrite(w, upscaleImage("uploaded.png", sourceWidth, sourceHeight))
		case "/api/v1/queue/default/enqueue_batch":
			if err := json.UnmarshalRead(r.Body, graph); err != nil {
				t.Error(err)
			}
			_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "batch": map[string]any{"batch_id": "img-batch"}, "item_ids": []int{19}, "enqueued": 1, "requested": 1})
		case "/api/v1/recall/default":
			_, _ = w.Write([]byte(`{"status":"success"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, requests, graph
}

func runImg2Img(t *testing.T, args ...string) (int, map[string]any) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.New(&stdout, &stderr).Run(t.Context(), append([]string{"generate", "--json"}, args...))
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%s", stderr.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout=%s: %v", stdout.String(), err)
	}
	return code, envelope
}

func TestGenerateSDXLImageSourceResolvesSizeAndReceipt(t *testing.T) {
	isolateUserConfigDir(t)
	server, requests, graph := img2imgServer(t, 1001, 750, sdxlOpenAPIFixture(t), sdxlInventory())
	code, envelope := runImg2Img(t, "--no-wait", "--model", "sdxl-main", "--prompt", "lighthouse", "--image", "source.png", "--seed", "41", "--url", server.URL)
	if code != 0 || envelope["ok"] != true {
		t.Fatalf("code=%d envelope=%#v", code, envelope)
	}
	data := envelope["data"].(map[string]any)
	settings := data["resolved_settings"].(map[string]any)
	if settings["width"] != float64(1000) || settings["height"] != float64(744) || settings["strength"] != 0.75 || data["source_uploaded"] != false || data["source_image"].(map[string]any)["image_name"] != "source.png" {
		t.Fatalf("receipt=%#v", data)
	}
	nodes := (*graph)["batch"].(map[string]any)["graph"].(map[string]any)["nodes"].(map[string]any)
	if nodes["resize"].(map[string]any)["width"] != float64(1000) || nodes["metadata"].(map[string]any)["generation_mode"] != "sdxl_img2img" {
		t.Fatalf("graph=%#v", nodes)
	}
	if slices.Contains(*requests, uploadRequest) || !slices.Contains(*requests, enqueueRequest) {
		t.Fatalf("requests=%q", *requests)
	}
	warnings := envelope["warnings"].([]any)
	fields := warnings[0].(map[string]any)["details"].(map[string]any)["not_restored"].([]any)
	if !slices.Equal(fields, []any{"scheduler", "vae", "output_count", "board_id", "source_image", "strength"}) {
		t.Fatalf("not_restored=%#v", fields)
	}
}

func TestGenerateSDXLPathUploadsOnceAfterPreflight(t *testing.T) {
	isolateUserConfigDir(t)
	path := sourcePNG(t, 17, 10)
	server, requests, graph := img2imgServer(t, 17, 10, sdxlOpenAPIFixture(t), sdxlInventory())
	code, envelope := runImg2Img(t, "--no-wait", "--model", "sdxl-main", "--prompt", "lighthouse", "--image-path", path, "--seed", "41", "--url", server.URL)
	if code != 0 || envelope["ok"] != true {
		t.Fatalf("code=%d envelope=%#v", code, envelope)
	}
	data := envelope["data"].(map[string]any)
	settings := data["resolved_settings"].(map[string]any)
	if settings["width"] != float64(16) || settings["height"] != float64(8) || data["source_uploaded"] != true || data["source_image"].(map[string]any)["image_name"] != "uploaded.png" {
		t.Fatalf("receipt=%#v", data)
	}
	if countRequest(*requests, uploadRequest) != 1 || countRequest(*requests, enqueueRequest) != 1 || slices.Index(*requests, uploadRequest) > slices.Index(*requests, enqueueRequest) {
		t.Fatalf("requests=%q", *requests)
	}
	nodes := (*graph)["batch"].(map[string]any)["graph"].(map[string]any)["nodes"].(map[string]any)
	if nodes["noise"].(map[string]any)["width"] != float64(16) || nodes["i2l"].(map[string]any)["image"] != nil {
		t.Fatalf("graph=%#v", nodes)
	}
}

func TestGenerateSDXLJPEGAndGIFPathsReadHeaderDimensions(t *testing.T) {
	for _, format := range []string{"jpg", "gif"} {
		t.Run(format, func(t *testing.T) {
			isolateUserConfigDir(t)
			path := sourceEncodedFile(t, format, 17, 10)
			server, requests, _ := img2imgServer(t, 17, 10, sdxlOpenAPIFixture(t), sdxlInventory())
			code, envelope := runImg2Img(t, "--no-wait", "--model", "sdxl-main", "--prompt", "lighthouse", "--image-path", path, "--seed", "41", "--url", server.URL)
			if code != 0 || envelope["ok"] != true {
				t.Fatalf("code=%d envelope=%#v", code, envelope)
			}
			data := envelope["data"].(map[string]any)
			if data["source_image"].(map[string]any)["width"] != float64(17) || data["source_image"].(map[string]any)["height"] != float64(10) || data["resolved_settings"].(map[string]any)["width"] != float64(16) || countRequest(*requests, uploadRequest) != 1 {
				t.Fatalf("data=%#v requests=%q", data, *requests)
			}
		})
	}
}

func TestGenerateSDXLFlagsAndDocumentResolveSameSourceAndStrength(t *testing.T) {
	isolateUserConfigDir(t)
	server, _, _ := img2imgServer(t, 512, 512, sdxlOpenAPIFixture(t), sdxlInventory())
	code, flagged := runImg2Img(t, "--no-wait", "--model", "sdxl-main", "--prompt", "lighthouse", "--image", "source.png", "--strength", "0.6", "--seed", "41", "--url", server.URL)
	if code != 0 {
		t.Fatalf("flags: %#v", flagged)
	}
	document := `{"schema_version":1,"model":"sdxl-main","positive_prompt":"lighthouse","source":{"type":"image","reference":"source.png"},"strength":0.6,"seed":41}`
	var stdout, stderr bytes.Buffer
	code = cli.NewWithIO(strings.NewReader(document), &stdout, &stderr).Run(t.Context(), []string{"generate", "--no-wait", "--request", "-", "--url", server.URL, "--json"})
	var fromDocument map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &fromDocument); err != nil {
		t.Fatal(err)
	}
	if code != 0 || stderr.Len() != 0 || !reflect.DeepEqual(flagged["data"].(map[string]any)["submitted_request"], fromDocument["data"].(map[string]any)["submitted_request"]) || !reflect.DeepEqual(flagged["data"].(map[string]any)["resolved_settings"], fromDocument["data"].(map[string]any)["resolved_settings"]) {
		t.Fatalf("flags=%#v document=%#v stderr=%s", flagged, fromDocument, stderr.String())
	}
}

func TestGenerateSDXLSourceDimensionsOverrideProfileDimensions(t *testing.T) {
	isolateUserConfigDir(t)
	createGenerateProfile(t, `{"schema_version":1,"name":"preset","generate":{"model":"sdxl-main","width":1024,"height":1024}}`)
	server, _, _ := img2imgServer(t, 513, 257, sdxlOpenAPIFixture(t), sdxlInventory())
	code, envelope := runImg2Img(t, "--no-wait", "--profile", "preset", "--prompt", "lighthouse", "--image", "source.png", "--seed", "41", "--url", server.URL)
	if code != 0 || envelope["ok"] != true {
		t.Fatalf("code=%d envelope=%#v", code, envelope)
	}
	settings := envelope["data"].(map[string]any)["resolved_settings"].(map[string]any)
	if settings["width"] != float64(512) || settings["height"] != float64(256) {
		t.Fatalf("settings=%#v", settings)
	}
}

func TestGenerateImageToImageLocalValidationMakesNoRequests(t *testing.T) {
	isolateUserConfigDir(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); http.NotFound(w, r) }))
	t.Cleanup(server.Close)
	tooSmall := sourcePNG(t, 7, 8)
	nonImage := filepath.Join(t.TempDir(), "text.txt")
	if err := os.WriteFile(nonImage, []byte("plain text"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		args  []string
		field string
	}{
		{"zero strength", []string{"--image", "source.png", "--strength", "0"}, "strength"},
		{"high strength", []string{"--image", "source.png", "--strength", "1.1"}, "strength"},
		{"malformed strength", []string{"--image", "source.png", "--strength", "banana"}, "strength"},
		{"missing strength value", []string{"--image", "source.png", "--strength"}, "strength"},
		{"strength without source", []string{"--strength", "0.5"}, "strength"},
		{"both source flags", []string{"--image", "source.png", "--image-path", tooSmall}, ""},
		{"relative path", []string{"--image-path", "source.png"}, ""},
		{"non-image", []string{"--image-path", nonImage}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"--model", "sdxl-main", "--prompt", "lighthouse", "--url", server.URL}, tc.args...)
			code, envelope := runImg2Img(t, args...)
			if code != result.ExitInvalidRequest || envelope["error"].(map[string]any)["code"] != "invalid_request" {
				t.Fatalf("code=%d envelope=%#v", code, envelope)
			}
			if tc.field != "" && envelope["error"].(map[string]any)["details"].(map[string]any)["field"] != tc.field {
				t.Fatalf("error=%#v", envelope["error"])
			}
			if requests.Load() != 0 {
				t.Fatalf("requests=%d", requests.Load())
			}
		})
	}
}

func TestGenerateSDXLTooSmallPathNeverUploads(t *testing.T) {
	isolateUserConfigDir(t)
	path := sourcePNG(t, 7, 8)
	server, requests, _ := img2imgServer(t, 7, 8, sdxlOpenAPIFixture(t), sdxlInventory())
	code, envelope := runImg2Img(t, "--no-wait", "--model", "sdxl-main", "--prompt", "lighthouse", "--image-path", path, "--seed", "41", "--url", server.URL)
	if code != result.ExitInvalidRequest || envelope["error"].(map[string]any)["code"] != "invalid_request" || slices.Contains(*requests, uploadRequest) || slices.Contains(*requests, enqueueRequest) {
		t.Fatalf("code=%d envelope=%#v requests=%q", code, envelope, *requests)
	}
}

func TestGenerateSDXLTooSmallExistingImageNeverEnqueues(t *testing.T) {
	isolateUserConfigDir(t)
	server, requests, _ := img2imgServer(t, 7, 8, sdxlOpenAPIFixture(t), sdxlInventory())
	code, envelope := runImg2Img(t, "--no-wait", "--model", "sdxl-main", "--prompt", "lighthouse", "--image", "source.png", "--seed", "41", "--url", server.URL)
	if code != result.ExitInvalidRequest || envelope["error"].(map[string]any)["code"] != "invalid_request" || !slices.Contains(*requests, "GET /api/v1/images/i/source.png") || slices.Contains(*requests, enqueueRequest) {
		t.Fatalf("code=%d envelope=%#v requests=%q", code, envelope, *requests)
	}
}

func TestGenerateImageToImageUnsupportedPathFormatsAreLocalErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		header []byte
	}{
		{"webp", []byte("RIFF\x10\x00\x00\x00WEBPVP8 ")},
		{"bmp", append([]byte("BM"), make([]byte, 60)...)},
		{"ico", append([]byte{0, 0, 1, 0}, make([]byte, 60)...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			if !strings.HasPrefix(http.DetectContentType(tc.header), "image/") {
				t.Fatalf("fixture is not detected as an image: %s", http.DetectContentType(tc.header))
			}
			path := filepath.Join(t.TempDir(), "source."+tc.name)
			if err := os.WriteFile(path, tc.header, 0o600); err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); http.NotFound(w, r) }))
			t.Cleanup(server.Close)
			code, envelope := runImg2Img(t, "--model", "sdxl-main", "--prompt", "lighthouse", "--image-path", path, "--url", server.URL)
			failure := envelope["error"].(map[string]any)
			if code != result.ExitInvalidRequest || failure["code"] != "invalid_request" || !strings.Contains(failure["message"].(string), "images upload") || requests.Load() != 0 {
				t.Fatalf("code=%d envelope=%#v requests=%d", code, envelope, requests.Load())
			}
		})
	}
}

func TestGenerateSDXLUploadedDimensionMismatchStopsEnqueue(t *testing.T) {
	isolateUserConfigDir(t)
	path := sourcePNG(t, 17, 10)
	server, requests, _ := img2imgServer(t, 18, 10, sdxlOpenAPIFixture(t), sdxlInventory())
	code, envelope := runImg2Img(t, "--no-wait", "--model", "sdxl-main", "--prompt", "lighthouse", "--image-path", path, "--seed", "41", "--url", server.URL)
	if code != result.ExitInvokeAIFailure || envelope["error"].(map[string]any)["code"] != "invalid_invokeai_response" || slices.Contains(*requests, enqueueRequest) {
		t.Fatalf("code=%d envelope=%#v requests=%q", code, envelope, *requests)
	}
	details := envelope["error"].(map[string]any)["details"].(map[string]any)
	if details["source_uploaded"] != true || details["source_image"].(map[string]any)["image_name"] != "uploaded.png" {
		t.Fatalf("details=%#v", details)
	}
}

func TestGenerateImageToImageRejectsOtherFamiliesBeforeUpload(t *testing.T) {
	isolateUserConfigDir(t)
	for _, inventory := range [][]map[string]any{fluxCLIInventory()} {
		server, requests, _ := img2imgServer(t, 512, 512, sdxlOpenAPIFixture(t), inventory)
		model := inventory[0]["key"].(string)
		code, envelope := runImg2Img(t, "--no-wait", "--model", model, "--prompt", "lighthouse", "--image", "source.png", "--seed", "41", "--url", server.URL)
		if code != result.ExitUnsupportedCapability || envelope["error"].(map[string]any)["code"] != "unsupported_capability" || slices.Contains(*requests, uploadRequest) || slices.Contains(*requests, enqueueRequest) {
			t.Fatalf("code=%d envelope=%#v requests=%q", code, envelope, *requests)
		}
	}
}

func TestGenerateAnimaImageSourceResolvesSizeAndReceipt(t *testing.T) {
	isolateUserConfigDir(t)
	server, requests, graph := img2imgServer(t, 1001, 750, sdxlOpenAPIFixture(t), animaModelInventory())
	code, envelope := runImg2Img(t, "--no-wait", "--model", "main-key", "--prompt", "lighthouse", "--image", "source.png", "--strength", "0.6", "--seed", "41", "--url", server.URL)
	if code != 0 || envelope["ok"] != true {
		t.Fatalf("code=%d envelope=%#v", code, envelope)
	}
	data := envelope["data"].(map[string]any)
	settings := data["resolved_settings"].(map[string]any)
	if settings["width"] != float64(1000) || settings["height"] != float64(744) || settings["strength"] != 0.6 || data["source_uploaded"] != false || data["source_image"].(map[string]any)["image_name"] != "source.png" {
		t.Fatalf("receipt=%#v", data)
	}
	components := settings["component_keys"].(map[string]any)
	if components["vae"] != "vae-key" || components["qwen3_encoder"] != "encoder-key" {
		t.Fatalf("components=%#v", components)
	}
	nodes := (*graph)["batch"].(map[string]any)["graph"].(map[string]any)["nodes"].(map[string]any)
	if nodes["resize"].(map[string]any)["width"] != float64(1000) || nodes["metadata"].(map[string]any)["generation_mode"] != "anima_img2img" || nodes["denoise"].(map[string]any)["denoising_start"] != 0.4 {
		t.Fatalf("graph=%#v", nodes)
	}
	if slices.Contains(*requests, uploadRequest) || !slices.Contains(*requests, enqueueRequest) {
		t.Fatalf("requests=%q", *requests)
	}
	warnings := envelope["warnings"].([]any)
	fields := warnings[0].(map[string]any)["details"].(map[string]any)["not_restored"].([]any)
	if !slices.Equal(fields, []any{"scheduler", "guidance", "vae", "qwen3_encoder", "output_count", "board_id", "source_image", "strength"}) {
		t.Fatalf("not_restored=%#v", fields)
	}
}

func TestGenerateAnimaImageSourceUsesExplicitComponentsWithoutResize(t *testing.T) {
	isolateUserConfigDir(t)
	inventory := append(animaModelInventory(),
		map[string]any{"key": "other-vae", "hash": "blake3:other-vae", "name": "Other Anima VAE", "base": "anima", "type": "vae"},
		map[string]any{"key": "other-encoder", "hash": "blake3:other-encoder", "name": "Other Qwen3", "base": "any", "type": "qwen3_encoder"},
	)
	server, _, graph := img2imgServer(t, 768, 768, sdxlOpenAPIFixture(t), inventory)
	code, envelope := runImg2Img(t, "--no-wait", "--model", "main-key", "--prompt", "lighthouse", "--image", "source.png", "--vae", "vae-key", "--qwen3-encoder", "encoder-key", "--seed", "41", "--url", server.URL)
	if code != 0 || envelope["ok"] != true {
		t.Fatalf("code=%d envelope=%#v", code, envelope)
	}
	settings := envelope["data"].(map[string]any)["resolved_settings"].(map[string]any)
	components := settings["component_keys"].(map[string]any)
	if components["vae"] != "vae-key" || components["qwen3_encoder"] != "encoder-key" || settings["strength"] != 0.75 {
		t.Fatalf("resolved settings=%#v", settings)
	}
	nodes := (*graph)["batch"].(map[string]any)["graph"].(map[string]any)["nodes"].(map[string]any)
	if _, resized := nodes["resize"]; resized {
		t.Fatalf("aligned source unexpectedly resized: %#v", nodes["resize"])
	}
	if nodes["i2l"].(map[string]any)["image"].(map[string]any)["image_name"] != "source.png" || nodes["model_loader"].(map[string]any)["vae_model"].(map[string]any)["key"] != "vae-key" || nodes["model_loader"].(map[string]any)["qwen3_encoder_model"].(map[string]any)["key"] != "encoder-key" || nodes["denoise"].(map[string]any)["denoising_start"] != 0.25 {
		t.Fatalf("graph=%#v", nodes)
	}
}

func TestGenerateAnimaTooSmallSourceNeverUploadsOrEnqueues(t *testing.T) {
	for _, source := range []string{"path", "image"} {
		t.Run(source, func(t *testing.T) {
			isolateUserConfigDir(t)
			server, requests, _ := img2imgServer(t, 7, 8, sdxlOpenAPIFixture(t), animaModelInventory())
			args := []string{"--no-wait", "--model", "main-key", "--prompt", "lighthouse", "--seed", "41", "--url", server.URL}
			if source == "path" {
				args = append(args, "--image-path", sourcePNG(t, 7, 8))
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

func TestDoctorAnimaImageToImageMatchesTextModelRequirements(t *testing.T) {
	isolateUserConfigDir(t)
	server, _, _ := img2imgServer(t, 768, 768, sdxlOpenAPIFixture(t), animaModelInventory())
	var stdout, stderr bytes.Buffer
	code := cli.New(&stdout, &stderr).Run(t.Context(), []string{"doctor", "--url", server.URL, "--json"})
	if code != result.ExitUnsupportedCapability || stderr.Len() != 0 {
		t.Fatalf("code=%d stderr=%q stdout=%s", code, stderr.String(), stdout.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	report := envelope["error"].(map[string]any)["details"].(map[string]any)["report"].(map[string]any)
	modes := map[string]map[string]any{}
	for _, raw := range report["capabilities"].([]any) {
		entry := raw.(map[string]any)
		if entry["operation"] == "generate" && entry["family"] == "anima" {
			modes[entry["mode"].(string)] = entry
		}
	}
	for _, mode := range []string{"txt2img", "img2img"} {
		entry := modes[mode]
		if entry == nil || entry["compatible"] != true || entry["ui_sync"] != "partial" || len(entry["failures"].([]any)) != 0 {
			t.Fatalf("Anima %s capability=%#v", mode, entry)
		}
	}
}

func TestAnimaImageToImageVocabularyGuardsGenerateAndDoctor(t *testing.T) {
	type requirement struct{ schema, property, endpoint string }
	var cases []requirement
	for _, schema := range []string{"ImageResizeInvocation", "AnimaImageToLatentsInvocation"} {
		cases = append(cases, requirement{schema: schema})
		fields := "id is_intermediate use_cache type image vae"
		if schema == "ImageResizeInvocation" {
			fields = "id is_intermediate use_cache type image width height resample_mode"
		}
		for field := range strings.FieldsSeq(fields) {
			cases = append(cases, requirement{schema: schema, property: field})
		}
	}
	for _, item := range []requirement{
		{schema: "AnimaDenoiseInvocation", property: "latents"},
		{schema: "CoreMetadataInvocation", property: "strength"},
		{schema: "CoreMetadataInvocation", property: "init_image"},
		{endpoint: "/api/v1/images/upload"},
	} {
		cases = append(cases, item)
	}
	for _, tc := range cases {
		name := tc.schema + "/" + tc.property + tc.endpoint
		t.Run(name, func(t *testing.T) {
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
			server, requests, _ := img2imgServer(t, 17, 10, document, animaModelInventory())
			code, envelope := runImg2Img(t, "--no-wait", "--model", "main-key", "--prompt", "lighthouse", "--image-path", sourcePNG(t, 17, 10), "--seed", "41", "--url", server.URL)
			failure := envelope["error"].(map[string]any)
			if code != result.ExitUnsupportedCapability || failure["code"] != "unsupported_capability" || slices.Contains(*requests, uploadRequest) || slices.Contains(*requests, enqueueRequest) {
				t.Fatalf("code=%d envelope=%#v requests=%q", code, envelope, *requests)
			}
			if tc.property != "" && !strings.Contains(failure["message"].(string), tc.property) {
				t.Fatalf("missing property absent from generate error: %#v", failure)
			}
			var stdout, stderr bytes.Buffer
			code = cli.New(&stdout, &stderr).Run(t.Context(), []string{"doctor", "--url", server.URL, "--json"})
			if code != result.ExitUnsupportedCapability || stderr.Len() != 0 {
				t.Fatalf("doctor code=%d stderr=%q stdout=%s", code, stderr.String(), stdout.String())
			}
			var doctorEnvelope map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &doctorEnvelope); err != nil {
				t.Fatal(err)
			}
			report := doctorEnvelope["error"].(map[string]any)["details"].(map[string]any)["report"].(map[string]any)
			var imageMode, textMode map[string]any
			for _, raw := range report["capabilities"].([]any) {
				entry := raw.(map[string]any)
				if entry["operation"] == "generate" && entry["family"] == "anima" {
					if entry["mode"] == "img2img" {
						imageMode = entry
					} else if entry["mode"] == "txt2img" {
						textMode = entry
					}
				}
			}
			if imageMode == nil || textMode == nil || imageMode["compatible"] != false || textMode["compatible"] != true {
				t.Fatalf("Anima doctor modes: image=%#v text=%#v", imageMode, textMode)
			}
			failures := imageMode["failures"].([]any)
			wantFailure := "incompatible_invocation:"
			if tc.endpoint != "" {
				wantFailure = "missing_endpoint:POST " + tc.endpoint
			} else {
				wantFailure += map[string]string{"ImageResizeInvocation": "img_resize", "AnimaImageToLatentsInvocation": "anima_i2l", "AnimaDenoiseInvocation": "anima_denoise", "CoreMetadataInvocation": "core_metadata"}[tc.schema]
			}
			if !slices.Contains(failures, any(wantFailure)) {
				t.Fatalf("failures=%#v, want %q", failures, wantFailure)
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

func countRequest(requests []string, target string) int {
	count := 0
	for _, request := range requests {
		if request == target {
			count++
		}
	}
	return count
}

func TestGenerateSDXLMissingImageReturnsNotFound(t *testing.T) {
	isolateUserConfigDir(t)
	server, requests, _ := img2imgServer(t, 512, 512, sdxlOpenAPIFixture(t), sdxlInventory())
	code, envelope := runImg2Img(t, "--no-wait", "--model", "sdxl-main", "--prompt", "lighthouse", "--image", "missing.png", "--seed", "41", "--url", server.URL)
	if code != result.ExitInvokeAIFailure || envelope["error"].(map[string]any)["code"] != "not_found" || slices.Contains(*requests, enqueueRequest) {
		t.Fatalf("code=%d envelope=%#v requests=%q", code, envelope, *requests)
	}
	if !strings.Contains(strings.Join(*requests, " "), "missing.png") {
		t.Fatalf("requests=%q", *requests)
	}
}

func TestGenerateSDXLSourcePreflightRejectsMissingVocabularyBeforeUpload(t *testing.T) {
	for _, tc := range []struct{ name, schema, property, endpoint string }{
		{"resize schema", "ImageResizeInvocation", "", ""},
		{"encoder field", "ImageToLatentsInvocation", "color_compensation", ""},
		{"denoiser latents", "DenoiseLatentsInvocation", "latents", ""},
		{"metadata strength", "CoreMetadataInvocation", "strength", ""},
		{"upload endpoint", "", "", "/api/v1/images/upload"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			path := sourcePNG(t, 17, 10)
			document := sdxlOpenAPIFixture(t)
			if tc.endpoint != "" {
				delete(document["paths"].(map[string]any), tc.endpoint)
			}
			if tc.schema != "" {
				schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
				if tc.property == "" {
					delete(schemas, tc.schema)
				} else {
					delete(schemas[tc.schema].(map[string]any)["properties"].(map[string]any), tc.property)
				}
			}
			server, requests, _ := img2imgServer(t, 17, 10, document, sdxlInventory())
			code, envelope := runImg2Img(t, "--no-wait", "--model", "sdxl-main", "--prompt", "lighthouse", "--image-path", path, "--seed", "41", "--url", server.URL)
			if code != result.ExitUnsupportedCapability || envelope["error"].(map[string]any)["code"] != "unsupported_capability" || slices.Contains(*requests, uploadRequest) || slices.Contains(*requests, enqueueRequest) {
				t.Fatalf("code=%d envelope=%#v requests=%q", code, envelope, *requests)
			}
		})
	}
}

func TestGenerateSDXLPathMutationFailureKeepsSingleUploadSemantics(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		uploadStatus, enqueueStatus int
		wantCode                    string
		wantUploaded                bool
	}{
		{"uncertain upload", http.StatusBadGateway, 0, "outcome_unknown", false},
		{"rejected enqueue", http.StatusCreated, http.StatusUnprocessableEntity, "invokeai_operation_failed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			path := sourcePNG(t, 17, 10)
			var uploads, enqueues atomic.Int32
			openAPI := sdxlOpenAPIFixture(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveAnimaPreflight(w, r, "6.14.1", openAPI, sdxlInventory()) {
					return
				}
				switch r.URL.Path {
				case "/api/v1/images/upload":
					uploads.Add(1)
					w.WriteHeader(tc.uploadStatus)
					if tc.uploadStatus == http.StatusCreated {
						_ = json.MarshalWrite(w, upscaleImage("uploaded.png", 17, 10))
					}
				case "/api/v1/queue/default/enqueue_batch":
					enqueues.Add(1)
					w.WriteHeader(tc.enqueueStatus)
					_, _ = w.Write([]byte(`{"detail":"rejected"}`))
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			code, envelope := runImg2Img(t, "--no-wait", "--model", "sdxl-main", "--prompt", "lighthouse", "--image-path", path, "--seed", "41", "--url", server.URL)
			failure := envelope["error"].(map[string]any)
			if code != result.ExitInvokeAIFailure || failure["code"] != tc.wantCode || uploads.Load() != 1 {
				t.Fatalf("code=%d envelope=%#v uploads=%d", code, envelope, uploads.Load())
			}
			if tc.wantUploaded {
				if enqueues.Load() != 1 || failure["details"].(map[string]any)["source_uploaded"] != true {
					t.Fatalf("enqueues=%d failure=%#v", enqueues.Load(), failure)
				}
			} else if enqueues.Load() != 0 {
				t.Fatalf("enqueue after uncertain upload: %d", enqueues.Load())
			}
		})
	}
}

func TestGenerateSDXLPathInvalidUploadReferenceRetainsSource(t *testing.T) {
	isolateUserConfigDir(t)
	path := sourcePNG(t, 17, 10)
	var uploads, enqueues atomic.Int32
	openAPI := sdxlOpenAPIFixture(t)
	inventory := sdxlInventory()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", openAPI, inventory) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/images/upload":
			uploads.Add(1)
			w.WriteHeader(http.StatusCreated)
			response := upscaleImage("uploaded.png", 17, 10)
			response["image_url"] = "://bad-url"
			_ = json.MarshalWrite(w, response)
		case "/api/v1/queue/default/enqueue_batch":
			enqueues.Add(1)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	code, envelope := runImg2Img(t, "--no-wait", "--model", "sdxl-main", "--prompt", "lighthouse", "--image-path", path, "--seed", "41", "--url", server.URL)
	failure := envelope["error"].(map[string]any)
	details, _ := failure["details"].(map[string]any)
	if code != result.ExitInvokeAIFailure || failure["code"] != "invalid_invokeai_response" || details["source_uploaded"] != true || details["source_image"].(map[string]any)["image_name"] != "uploaded.png" || uploads.Load() != 1 || enqueues.Load() != 0 {
		t.Fatalf("code=%d failure=%#v uploads=%d enqueues=%d", code, failure, uploads.Load(), enqueues.Load())
	}
}
