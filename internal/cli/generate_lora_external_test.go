package cli_test

import (
	"bytes"
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/cli"
)

func TestGenerateSDXLLoRAFlagSubmitsOneGraphAndResolvedReceipt(t *testing.T) {
	isolateUserConfigDir(t)
	inventory := append(sdxlInventory(), map[string]any{"key": "lora-a", "hash": "hash-a", "name": "Alien Style", "base": "sdxl", "type": "lora", "default_settings": map[string]any{"weight": 1.25}})
	openAPI := sdxlOpenAPIFixture(t)
	var enqueues atomic.Int32
	var graph map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", openAPI, inventory) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/queue/default/enqueue_batch":
			enqueues.Add(1)
			if err := json.UnmarshalRead(r.Body, &graph); err != nil {
				t.Error(err)
			}
			_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "enqueued": 1, "requested": 1, "item_ids": []int{17}, "batch": map[string]any{"batch_id": "lora-batch"}})
		case "/api/v1/recall/default":
			var patch map[string]any
			if err := json.UnmarshalRead(r.Body, &patch); err != nil {
				t.Error(err)
			}
			want := map[string]any{
				"model": "SDXL Main", "positive_prompt": "alienzkin", "negative_prompt": "",
				"width": float64(1024), "height": float64(1024), "steps": float64(30), "seed": float64(41), "cfg_scale": float64(7),
				"loras": []any{map[string]any{"model_name": "Alien Style", "weight": 1.25}},
			}
			if !reflect.DeepEqual(patch, want) {
				t.Errorf("Recall patch = %#v, want %#v", patch, want)
			}
			_, _ = w.Write([]byte(`{"status":"success"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := cli.New(&stdout, &stderr).Run(t.Context(), []string{"generate", "--no-wait", "--model", "sdxl-main", "--prompt", "alienzkin", "--seed", "41", "--lora", "Alien Style", "--url", server.URL, "--json"})
	if code != 0 || stderr.Len() != 0 || enqueues.Load() != 1 {
		t.Fatalf("code=%d enqueues=%d stderr=%s stdout=%s", code, enqueues.Load(), stderr.String(), stdout.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	data := envelope["data"].(map[string]any)
	submitted := data["submitted_request"].(map[string]any)["loras"].([]any)[0].(map[string]any)
	resolved := data["resolved_settings"].(map[string]any)["loras"].([]any)[0].(map[string]any)
	if submitted["model"] != "Alien Style" || submitted["weight"] != nil || resolved["model_key"] != "lora-a" || resolved["weight"] != 1.25 {
		t.Fatalf("receipt = %#v", data)
	}
	nodes := graph["batch"].(map[string]any)["graph"].(map[string]any)["nodes"].(map[string]any)
	if nodes["lora_0"].(map[string]any)["weight"] != 1.25 {
		t.Fatalf("LoRA graph = %#v", graph)
	}
	warnings := envelope["warnings"].([]any)
	fields := warnings[0].(map[string]any)["details"].(map[string]any)["not_restored"].([]any)
	if !reflect.DeepEqual(fields, []any{"scheduler", "vae", "output_count", "board_id"}) {
		t.Fatalf("not_restored = %#v", fields)
	}
}

func TestGenerateSDXLLoRAFlagRejectsInvalidWeightBeforeNetwork(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := cli.New(&stdout, &stderr).Run(t.Context(), []string{"generate", "--model", "sdxl-main", "--prompt", "test", "--lora", "Alien Style=nope", "--url", server.URL, "--json"})
	if code != 2 || requests.Load() != 0 || !strings.Contains(stdout.String(), `"field":"loras.0.weight"`) {
		t.Fatalf("code=%d requests=%d stdout=%s stderr=%s", code, requests.Load(), stdout.String(), stderr.String())
	}
}

func TestGenerateLoRARejectsLocalDocumentAndFlagErrorsWithoutNetwork(t *testing.T) {
	for _, test := range []struct {
		name, document, flag, field string
	}{
		{"empty list", `{"schema_version":1,"model":"sdxl-main","positive_prompt":"test","loras":[]}`, "", "loras"},
		{"null list", `{"schema_version":1,"model":"sdxl-main","positive_prompt":"test","loras":null}`, "", "loras"},
		{"null element", `{"schema_version":1,"model":"sdxl-main","positive_prompt":"test","loras":[null]}`, "", "loras.0"},
		{"unknown element member", `{"schema_version":1,"model":"sdxl-main","positive_prompt":"test","loras":[{"model":"lora-a","extra":1}]}`, "", "loras.0.extra"},
		{"empty document selector", `{"schema_version":1,"model":"sdxl-main","positive_prompt":"test","loras":[{"model":""}]}`, "", "loras.0.model"},
		{"document weight bounds", `{"schema_version":1,"model":"sdxl-main","positive_prompt":"test","loras":[{"model":"lora-a","weight":11}]}`, "", "loras.0.weight"},
		{"flag empty selector", "", "=0.5", "loras.0.model"},
		{"flag nonnumeric suffix", "", "Alien Style=nope", "loras.0.weight"},
		{"flag infinite weight", "", "Alien Style=Inf", "loras.0.weight"},
		{"flag hexadecimal weight", "", "Alien Style=0x1p0", "loras.0.weight"},
		{"flag underscored weight", "", "Alien Style=1_0", "loras.0.weight"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer server.Close()
			args := []string{"generate", "--json", "--url", server.URL}
			var stdin *strings.Reader
			if test.document != "" {
				args = append(args, "--request", "-")
				stdin = strings.NewReader(test.document)
			} else {
				args = append(args, "--model", "sdxl-main", "--prompt", "test", "--lora", test.flag)
				stdin = strings.NewReader("")
			}
			var stdout, stderr bytes.Buffer
			code := cli.NewWithIO(stdin, &stdout, &stderr).Run(t.Context(), args)
			if code != 2 || requests.Load() != 0 || !strings.Contains(stdout.String(), `"field":"`+test.field+`"`) {
				t.Fatalf("code=%d requests=%d stdout=%s stderr=%s", code, requests.Load(), stdout.String(), stderr.String())
			}
		})
	}
}

func TestGenerateLoRAFlagAndRequestCannotBeCombined(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1) }))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := cli.New(&stdout, &stderr).Run(t.Context(), []string{"generate", "--request", "-", "--lora", "Alien Style", "--url", server.URL, "--json"})
	if code != 2 || requests.Load() != 0 || !strings.Contains(stdout.String(), "operation flags cannot be combined") {
		t.Fatalf("code=%d requests=%d stdout=%s", code, requests.Load(), stdout.String())
	}
}

func TestGenerateSDXLLoRAPreflightFailsBeforeSourceUploadAndEnqueue(t *testing.T) {
	for _, test := range []struct{ name, schema, property string }{
		{"missing loader", "SDXLLoRALoaderInvocation", ""},
		{"missing loader property", "SDXLLoRALoaderInvocation", "clip2"},
		{"missing metadata loras", "CoreMetadataInvocation", "loras"},
	} {
		t.Run(test.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			document := sdxlOpenAPIFixture(t)
			schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
			if test.property == "" {
				delete(schemas, test.schema)
			} else {
				delete(schemas[test.schema].(map[string]any)["properties"].(map[string]any), test.property)
			}
			inventory := append(sdxlInventory(), map[string]any{"key": "lora-a", "hash": "hash-a", "name": "Alien Style", "base": "sdxl", "type": "lora"})
			server, requests, _ := img2imgServer(t, 768, 768, document, inventory)
			path := sourcePNG(t, 768, 768)
			code, envelope := runImg2Img(t, "--no-wait", "--model", "sdxl-main", "--prompt", "test", "--image-path", path, "--lora", "lora-a", "--url", server.URL)
			if code != 4 || envelope["error"].(map[string]any)["code"] != "unsupported_capability" || countRequest(*requests, "POST /api/v1/images/upload") != 0 || countRequest(*requests, "POST /api/v1/queue/default/enqueue_batch") != 0 {
				t.Fatalf("code=%d envelope=%#v requests=%#v", code, envelope, *requests)
			}
			code, envelope = runImg2Img(t, "--no-wait", "--model", "sdxl-main", "--prompt", "test", "--image-path", path, "--url", server.URL)
			if code != 0 || countRequest(*requests, "POST /api/v1/images/upload") != 1 || countRequest(*requests, "POST /api/v1/queue/default/enqueue_batch") != 1 {
				t.Fatalf("LoRA-less request affected: code=%d envelope=%#v requests=%#v", code, envelope, *requests)
			}
		})
	}
}

func TestGenerateLoRAFlagsPreserveCommasSplitAtLastEqualsAndMatchDocument(t *testing.T) {
	isolateUserConfigDir(t)
	inventory := append(sdxlInventory(), map[string]any{"key": "lora-a", "hash": "hash-a", "name": "Alien, Style=Special", "base": "sdxl", "type": "lora"})
	server, requests, _ := img2imgServer(t, 768, 768, sdxlOpenAPIFixture(t), inventory)
	run := func(stdin string, args ...string) map[string]any {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := cli.NewWithIO(strings.NewReader(stdin), &stdout, &stderr).Run(t.Context(), append([]string{"generate", "--no-wait", "--url", server.URL, "--json"}, args...))
		if code != 0 || stderr.Len() != 0 {
			t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
		}
		var envelope map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope["data"].(map[string]any)
	}
	flagged := run("", "--model", "sdxl-main", "--prompt", "test", "--seed", "41", "--lora", "Alien, Style=Special=0.5")
	fromDocument := run(`{"schema_version":1,"model":"sdxl-main","positive_prompt":"test","seed":41,"loras":[{"model":"Alien, Style=Special","weight":0.5}]}`, "--request", "-")
	if countRequest(*requests, "POST /api/v1/queue/default/enqueue_batch") != 2 || !reflect.DeepEqual(flagged["submitted_request"], fromDocument["submitted_request"]) || !reflect.DeepEqual(flagged["resolved_settings"], fromDocument["resolved_settings"]) {
		t.Fatalf("flags and document differ: flagged=%#v document=%#v requests=%#v", flagged, fromDocument, *requests)
	}
}

func TestGenerateOtherFamiliesRejectLoRAsBeforeSourceUpload(t *testing.T) {
	for _, main := range []map[string]any{
		{"key": "flux2-main", "hash": "hash", "name": "FLUX.2", "base": "flux2", "type": "main", "variant": "dev", "format": "checkpoint"},
	} {
		t.Run(main["base"].(string), func(t *testing.T) {
			isolateUserConfigDir(t)
			server, requests, _ := img2imgServer(t, 768, 768, sdxlOpenAPIFixture(t), []map[string]any{main})
			code, envelope := runImg2Img(t, "--no-wait", "--model", main["key"].(string), "--prompt", "test", "--image-path", sourcePNG(t, 768, 768), "--lora", "anything", "--url", server.URL)
			if code != 4 || envelope["error"].(map[string]any)["code"] != "unsupported_capability" || countRequest(*requests, "POST /api/v1/images/upload") != 0 || countRequest(*requests, "POST /api/v1/queue/default/enqueue_batch") != 0 {
				t.Fatalf("code=%d envelope=%#v requests=%#v", code, envelope, *requests)
			}
		})
	}
}

func TestGenerateAnimaLoRARecordsReceiptAndSynchronization(t *testing.T) {
	isolateUserConfigDir(t)
	inventory := append(animaModelInventory(), map[string]any{"key": "anima-lora", "hash": "lora-hash", "name": "Detail Tweaker", "base": "anima", "type": "lora", "default_settings": map[string]any{"weight": 1.25}})
	server, requests, graphs := img2imgServer(t, 768, 768, sdxlOpenAPIFixture(t), inventory)
	code, envelope := runImg2Img(t, "--no-wait", "--model", "main-key", "--prompt", "a lighthouse", "--seed", "41", "--lora", "Detail Tweaker", "--url", server.URL)
	if code != 0 || countRequest(*requests, "POST /api/v1/queue/default/enqueue_batch") != 1 || len(*graphs) != 1 {
		t.Fatalf("code=%d envelope=%#v requests=%#v", code, envelope, *requests)
	}
	data := envelope["data"].(map[string]any)
	submitted := data["submitted_request"].(map[string]any)["loras"].([]any)[0].(map[string]any)
	resolved := data["resolved_settings"].(map[string]any)["loras"].([]any)[0].(map[string]any)
	if submitted["model"] != "Detail Tweaker" || submitted["weight"] != nil || resolved["model_key"] != "anima-lora" || resolved["weight"] != 1.25 {
		t.Fatalf("receipt LoRAs: submitted=%#v resolved=%#v", submitted, resolved)
	}
	fields := envelope["warnings"].([]any)[0].(map[string]any)["details"].(map[string]any)["not_restored"].([]any)
	if !reflect.DeepEqual(fields, []any{"scheduler", "guidance", "vae", "qwen3_encoder", "output_count", "board_id"}) {
		t.Fatalf("not_restored = %#v", fields)
	}
	code, envelope = runImg2Img(t, "--no-wait", "--model", "main-key", "--prompt", "a lighthouse", "--seed", "41", "--image-path", sourcePNG(t, 768, 768), "--strength", "0.6", "--lora", "anima-lora=1", "--url", server.URL)
	if code != 0 || countRequest(*requests, "POST /api/v1/images/upload") != 1 || countRequest(*requests, "POST /api/v1/queue/default/enqueue_batch") != 2 {
		t.Fatalf("image-to-image: code=%d envelope=%#v requests=%#v", code, envelope, *requests)
	}
	nodes := (*graphs)["batch"].(map[string]any)["graph"].(map[string]any)["nodes"].(map[string]any)
	if nodes["metadata"].(map[string]any)["generation_mode"] != "anima_img2img" || nodes["lora_0"].(map[string]any)["weight"] != float64(1) {
		t.Fatalf("image-to-image LoRA graph = %#v", nodes)
	}
	fields = envelope["warnings"].([]any)[0].(map[string]any)["details"].(map[string]any)["not_restored"].([]any)
	if !reflect.DeepEqual(fields[len(fields)-2:], []any{"source_image", "strength"}) {
		t.Fatalf("image-to-image not_restored = %#v", fields)
	}
}

func TestGenerateAnimaLoRARejectsWrongBaseAndMissingVocabularyBeforeUpload(t *testing.T) {
	for _, test := range []struct{ name, schema, property, base, modelType string }{
		{"wrong base", "", "", "sdxl", "lora"},
		{"wrong type", "", "", "anima", "main"},
		{"missing loader", "AnimaLoRALoaderInvocation", "", "anima", "lora"},
		{"missing loader property", "AnimaLoRALoaderInvocation", "qwen3_encoder", "anima", "lora"},
	} {
		t.Run(test.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			openAPI := sdxlOpenAPIFixture(t)
			if test.schema != "" {
				schemas := openAPI["components"].(map[string]any)["schemas"].(map[string]any)
				if test.property == "" {
					delete(schemas, test.schema)
				} else {
					delete(schemas[test.schema].(map[string]any)["properties"].(map[string]any), test.property)
				}
			}
			inventory := append(animaModelInventory(), map[string]any{"key": "anima-lora", "hash": "lora-hash", "name": "Detail Tweaker", "base": test.base, "type": test.modelType})
			server, requests, _ := img2imgServer(t, 768, 768, openAPI, inventory)
			code, envelope := runImg2Img(t, "--no-wait", "--model", "main-key", "--prompt", "test", "--image-path", sourcePNG(t, 768, 768), "--lora", "anima-lora", "--url", server.URL)
			if code != 4 || envelope["error"].(map[string]any)["code"] != "unsupported_capability" || countRequest(*requests, "POST /api/v1/images/upload") != 0 || countRequest(*requests, "POST /api/v1/queue/default/enqueue_batch") != 0 {
				t.Fatalf("code=%d envelope=%#v requests=%#v", code, envelope, *requests)
			}
		})
	}
}
