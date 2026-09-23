package cli_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/version"
)

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("test write failure")
}

func writeTestPNG(t *testing.T, path string) []byte {
	t.Helper()
	content, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return content
}

func animaOpenAPIFixture(missingSchema, missingProperty string) map[string]any {
	requirements := map[string]struct {
		invocationType string
		properties     string
	}{
		"AnimaDenoiseInvocation": {
			invocationType: "anima_denoise",
			properties:     "id is_intermediate use_cache type denoising_start denoising_end add_noise guidance_scale width height steps seed scheduler transformer positive_conditioning negative_conditioning",
		},
		"AnimaLatentsToImageInvocation": {
			invocationType: "anima_l2i",
			properties:     "id is_intermediate use_cache type board latents metadata vae",
		},
		"AnimaModelLoaderInvocation": {
			invocationType: "anima_model_loader",
			properties:     "id is_intermediate use_cache type model vae_model qwen3_encoder_model",
		},
		"AnimaTextEncoderInvocation": {
			invocationType: "anima_text_encoder",
			properties:     "id is_intermediate use_cache type prompt qwen3_encoder",
		},
		"CollectInvocation": {
			invocationType: "collect",
			properties:     "id is_intermediate use_cache type collection item",
		},
		"CoreMetadataInvocation": {
			invocationType: "core_metadata",
			properties:     "id is_intermediate use_cache type generation_mode negative_prompt width height cfg_scale steps scheduler model vae qwen3_encoder seed positive_prompt",
		},
		"IntegerInvocation": {
			invocationType: "integer",
			properties:     "id is_intermediate use_cache type value",
		},
		"StringInvocation": {
			invocationType: "string",
			properties:     "id is_intermediate use_cache type value",
		},
	}
	schemas := make(map[string]any, len(requirements))
	for schema, requirement := range requirements {
		properties := make(map[string]any)
		for property := range strings.FieldsSeq(requirement.properties) {
			if schema == missingSchema && property == missingProperty {
				continue
			}
			properties[property] = map[string]any{}
		}
		properties["type"] = map[string]any{"const": requirement.invocationType}
		schemas[schema] = map[string]any{"properties": properties}
	}
	if missingSchema != "" && missingProperty == "" {
		delete(schemas, missingSchema)
	}
	recallSchema := recallOpenAPI()
	schemas["RecallParameter"] = recallSchema["components"].(map[string]any)["schemas"].(map[string]any)["RecallParameter"]
	return map[string]any{
		"paths":      recallSchema["paths"],
		"components": map[string]any{"schemas": schemas},
	}
}

func newAnimaGenerationServer(openAPI map[string]any, models []map[string]any, enqueue http.HandlerFunc) *httptest.Server {
	if openAPI == nil {
		openAPI = animaOpenAPIFixture("", "")
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", openAPI, models) {
			return
		}
		if r.URL.Path == "/api/v1/queue/default/enqueue_batch" {
			enqueue(w, r)
			return
		}
		if r.URL.Path == "/api/v1/recall/default" {
			_, _ = w.Write([]byte(`{"status":"success"}`))
			return
		}
		http.NotFound(w, r)
	}))
}

func animaModelInventory() []map[string]any {
	return []map[string]any{
		{"key": "main-key", "hash": "blake3:main", "name": "Anima Main", "base": "anima", "type": "main"},
		{"key": "vae-key", "hash": "blake3:vae", "name": "Anima VAE", "base": "anima", "type": "vae"},
		{"key": "encoder-key", "hash": "blake3:encoder", "name": "Qwen3 Encoder", "base": "any", "type": "qwen3_encoder"},
	}
}

func serveAnimaPreflight(w http.ResponseWriter, r *http.Request, version string, openAPI map[string]any, models []map[string]any) bool {
	switch r.URL.Path {
	case "/api/v1/app/version":
		_ = jsonv2.MarshalWrite(w, map[string]any{"version": version})
	case "/openapi.json":
		_ = jsonv2.MarshalWrite(w, openAPI)
	case "/api/v2/models/":
		_ = jsonv2.MarshalWrite(w, map[string]any{"models": models})
	default:
		return false
	}
	return true
}

func TestGenerateNoWaitFlagsSubmitExactAnimaRequestOnce(t *testing.T) {
	isolateUserConfigDir(t)
	fixture, err := os.ReadFile("../generation/testdata/anima_6_14_enqueue.json")
	if err != nil {
		t.Fatal(err)
	}
	var wantEnqueue any
	if err := jsonv2.Unmarshal(fixture, &wantEnqueue); err != nil {
		t.Fatal(err)
	}
	var enqueueRequests atomic.Int32
	server := newAnimaGenerationServer(nil, []map[string]any{
		{"key": "main-key", "hash": "blake3:main", "name": "Anima Main", "base": "anima", "type": "main", "format": "checkpoint"},
		{"key": "vae-key", "hash": "blake3:vae", "name": "Anima VAE", "base": "anima", "type": "vae", "format": "checkpoint"},
		{"key": "encoder-key", "hash": "blake3:encoder", "name": "Qwen3 Encoder", "base": "any", "type": "qwen3_encoder", "format": "checkpoint"},
	}, func(w http.ResponseWriter, r *http.Request) {
		enqueueRequests.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("enqueue method = %s, want POST", r.Method)
		}
		var gotEnqueue any
		if err := jsonv2.UnmarshalRead(r.Body, &gotEnqueue); err != nil {
			t.Errorf("decode enqueue request: %v", err)
			return
		}
		if !reflect.DeepEqual(gotEnqueue, wantEnqueue) {
			t.Errorf("enqueue request does not match fixture\ngot:  %#v\nwant: %#v", gotEnqueue, wantEnqueue)
		}
		_ = jsonv2.MarshalWrite(w, map[string]any{
			"queue_id": "default", "enqueued": 1, "requested": 1, "priority": 0, "item_ids": []int{17},
			"batch": map[string]any{"batch_id": "batch-1", "origin": "generate", "destination": "generate", "graph": map[string]any{}, "runs": 1},
		})
	})
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{
		"generate", "--no-wait",
		"--model", "main-key", "--prompt", "a lighthouse in a storm", "--negative-prompt", "text",
		"--width", "768", "--height", "1024", "--steps", "24", "--scheduler", "heun", "--guidance", "4.25",
		"--seed", "42", "--output-count", "1", "--board", "board-1", "--vae", "vae-key", "--qwen3-encoder", "encoder-key",
		"--url", server.URL, "--json",
	})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 || enqueueRequests.Load() != 1 {
		t.Fatalf("exit code = %d, enqueue requests = %d, stderr = %q, stdout = %q", exitCode, enqueueRequests.Load(), stderr.String(), stdout.String())
	}
	var envelope receiptEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not a JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.SchemaVersion != 1 || !envelope.OK || envelope.Operation != "generate" ||
		envelope.Data.SubmittedRequest["model"] != "main-key" ||
		envelope.Data.ResolvedSettings.ModelKey != "main-key" ||
		envelope.Data.ResolvedSettings.BoardID != "board-1" ||
		!reflect.DeepEqual(envelope.Data.ResolvedSettings.ComponentKeys, map[string]string{"vae": "vae-key", "qwen3_encoder": "encoder-key"}) ||
		!reflect.DeepEqual(envelope.Data.ResolvedSettings.Seeds, []uint32{42}) ||
		envelope.Data.Queue.QueueID != "default" || envelope.Data.Queue.BatchID != "batch-1" ||
		!reflect.DeepEqual(envelope.Data.Queue.ItemIDs, []int{17}) || len(envelope.Data.Outputs) != 0 {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
	assertPartialSyncWarning(t, envelope.Data.Warnings, envelope.Warnings)
}

func TestGenerateNoWaitSubmitsOneOrderedBatch(t *testing.T) {
	isolateUserConfigDir(t)
	var enqueueRequests atomic.Int32
	server := newAnimaGenerationServer(nil, animaModelInventory(), func(w http.ResponseWriter, r *http.Request) {
		enqueueRequests.Add(1)
		var payload struct {
			Batch struct {
				Data [][]struct {
					NodePath  string   `json:"node_path"`
					FieldName string   `json:"field_name"`
					Items     []uint32 `json:"items"`
				} `json:"data"`
				Graph struct {
					Nodes map[string]json.RawMessage `json:"nodes"`
				} `json:"graph"`
				Runs int `json:"runs"`
			} `json:"batch"`
		}
		if err := jsonv2.UnmarshalRead(r.Body, &payload); err != nil {
			t.Errorf("decode enqueue request: %v", err)
			return
		}
		if payload.Batch.Runs != 1 || len(payload.Batch.Data) != 1 || len(payload.Batch.Data[0]) != 1 {
			t.Errorf("unexpected batch shape: %#v", payload.Batch)
		} else {
			seedData := payload.Batch.Data[0][0]
			if seedData.NodePath != "seed" || seedData.FieldName != "value" || !slices.Equal(seedData.Items, []uint32{9, 8, 7}) {
				t.Errorf("seed batch data = %#v", seedData)
			}
		}
		var decode map[string]any
		if err := json.Unmarshal(payload.Batch.Graph.Nodes["decode"], &decode); err != nil {
			t.Errorf("decode output node: %v", err)
		} else if !reflect.DeepEqual(decode["board"], map[string]any{"board_id": "board-1"}) {
			t.Errorf("decode board = %#v", decode["board"])
		}
		_ = jsonv2.MarshalWrite(w, map[string]any{
			"queue_id": "default", "enqueued": 3, "requested": 3, "item_ids": []int{23, 22, 21},
			"batch": map[string]any{"batch_id": "batch-3"},
		})
	})
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{
		"generate", "--no-wait", "--model", "main-key", "--prompt", "test",
		"--seed", "7", "--output-count", "3", "--board", "board-1",
		"--vae", "vae-key", "--qwen3-encoder", "encoder-key", "--url", server.URL, "--json",
	})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 || enqueueRequests.Load() != 1 {
		t.Fatalf("exit code = %d, enqueue requests = %d, stderr = %q, stdout = %q", exitCode, enqueueRequests.Load(), stderr.String(), stdout.String())
	}
	var envelope receiptEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.ResolvedSettings.OutputCount != 3 ||
		envelope.Data.ResolvedSettings.BoardID != "board-1" ||
		!slices.Equal(envelope.Data.ResolvedSettings.Seeds, []uint32{7, 8, 9}) ||
		!slices.Equal(envelope.Data.Queue.ItemIDs, []int{23, 22, 21}) || len(envelope.Data.Outputs) != 0 {
		t.Fatalf("unexpected receipt: %#v", envelope.Data)
	}
}

func TestGenerateNoWaitResolvesOmittedAnimaDefaultsAndComponents(t *testing.T) {
	isolateUserConfigDir(t)
	var enqueueRequests atomic.Int32
	var enqueuedSeed uint32
	server := newAnimaGenerationServer(nil, []map[string]any{
		{"key": "main-key", "hash": "blake3:main", "name": "Anima Main", "base": "anima", "type": "main"},
		{"key": "vae-key", "hash": "blake3:vae", "name": "Anima VAE", "base": "anima", "type": "vae"},
		{"key": "encoder-key", "hash": "blake3:encoder", "name": "Qwen3 Encoder", "base": "any", "type": "qwen3_encoder"},
	}, func(w http.ResponseWriter, r *http.Request) {
		enqueueRequests.Add(1)
		var payload struct {
			Batch struct {
				Graph struct {
					Nodes map[string]json.RawMessage `json:"nodes"`
				} `json:"graph"`
			} `json:"batch"`
		}
		if err := jsonv2.UnmarshalRead(r.Body, &payload); err != nil {
			t.Errorf("decode enqueue request: %v", err)
			return
		}
		var denoise struct {
			Width         int     `json:"width"`
			Height        int     `json:"height"`
			Steps         int     `json:"steps"`
			Scheduler     string  `json:"scheduler"`
			GuidanceScale float64 `json:"guidance_scale"`
		}
		if err := json.Unmarshal(payload.Batch.Graph.Nodes["denoise"], &denoise); err != nil {
			t.Errorf("decode denoise node: %v", err)
		}
		if denoise.Width != 1024 || denoise.Height != 1024 || denoise.Steps != 30 || denoise.Scheduler != "euler" || denoise.GuidanceScale != 4.5 {
			t.Errorf("unexpected resolved denoise settings: %#v", denoise)
		}
		var seed struct {
			Value uint32 `json:"value"`
		}
		if err := json.Unmarshal(payload.Batch.Graph.Nodes["seed"], &seed); err != nil {
			t.Errorf("decode seed node: %v", err)
		}
		enqueuedSeed = seed.Value
		_ = jsonv2.MarshalWrite(w, map[string]any{
			"queue_id": "default", "enqueued": 1, "requested": 1, "item_ids": []int{19},
			"batch": map[string]any{"batch_id": "batch-defaults"},
		})
	})
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{
		"generate", "--no-wait", "--model", "main-key", "--prompt", "test", "--url", server.URL, "--json",
	})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 || enqueueRequests.Load() != 1 {
		t.Fatalf("exit code = %d, enqueue requests = %d, stderr = %q, stdout = %q", exitCode, enqueueRequests.Load(), stderr.String(), stdout.String())
	}
	var envelope receiptEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	settings := envelope.Data.ResolvedSettings
	if settings.NegativePrompt != "" || settings.Width != 1024 || settings.Height != 1024 || settings.Steps != 30 ||
		settings.Scheduler != "euler" || settings.Guidance != 4.5 || settings.OutputCount != 1 ||
		settings.ModelKey != "main-key" ||
		!reflect.DeepEqual(settings.ComponentKeys, map[string]string{"vae": "vae-key", "qwen3_encoder": "encoder-key"}) ||
		len(settings.Seeds) != 1 || settings.Seeds[0] != enqueuedSeed {
		t.Fatalf("unexpected resolved settings: %#v", settings)
	}
	if _, exists := envelope.Data.SubmittedRequest["width"]; exists {
		t.Fatalf("submitted request contains resolved defaults: %#v", envelope.Data.SubmittedRequest)
	}
}

func TestGenerateRejectsMissingInvokeAIInvocationFieldBeforeEnqueue(t *testing.T) {
	isolateUserConfigDir(t)
	var enqueueRequests atomic.Int32
	server := newAnimaGenerationServer(
		animaOpenAPIFixture("AnimaDenoiseInvocation", "guidance_scale"),
		[]map[string]any{
			{"key": "main-key", "hash": "blake3:main", "name": "Anima Main", "base": "anima", "type": "main"},
			{"key": "vae-key", "hash": "blake3:vae", "name": "Anima VAE", "base": "anima", "type": "vae"},
			{"key": "encoder-key", "hash": "blake3:encoder", "name": "Qwen3 Encoder", "base": "any", "type": "qwen3_encoder"},
		},
		func(w http.ResponseWriter, _ *http.Request) {
			enqueueRequests.Add(1)
			http.Error(w, "must not enqueue", http.StatusInternalServerError)
		},
	)
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{
		"generate", "--no-wait", "--model", "main-key", "--prompt", "test", "--url", server.URL, "--json",
	})

	if exitCode != result.ExitUnsupportedCapability || stderr.Len() != 0 || enqueueRequests.Load() != 0 {
		t.Fatalf("exit code = %d, enqueue requests = %d, stderr = %q, stdout = %q", exitCode, enqueueRequests.Load(), stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := jsonv2.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error == nil || envelope.Error.Code != result.CodeUnsupportedCapability ||
		!strings.Contains(envelope.Error.Message, "AnimaDenoiseInvocation") ||
		!strings.Contains(envelope.Error.Message, "guidance_scale") {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestGenerateRejectsUnsupportedCapabilitiesBeforeEnqueue(t *testing.T) {
	tests := []struct {
		name   string
		server func(*atomic.Int32) *httptest.Server
		args   []string
	}{
		{
			name: "unsupported InvokeAI version",
			server: func(enqueueRequests *atomic.Int32) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if serveAnimaPreflight(w, r, "6.15.0", nil, nil) {
						return
					}
					if r.URL.Path == "/api/v1/queue/default/enqueue_batch" {
						enqueueRequests.Add(1)
					}
					http.NotFound(w, r)
				}))
			},
			args: []string{"generate", "--no-wait", "--model", "main-key", "--prompt", "test"},
		},
		{
			name: "untested FLUX VAE combination selected by name",
			server: func(enqueueRequests *atomic.Int32) *httptest.Server {
				return newAnimaGenerationServer(nil, []map[string]any{
					{"key": "main-key", "hash": "blake3:main", "name": "Anima Main", "base": "anima", "type": "main"},
					{"key": "flux-vae", "hash": "blake3:flux-vae", "name": "FLUX VAE", "base": "flux", "type": "vae"},
					{"key": "encoder-key", "hash": "blake3:encoder", "name": "Qwen3 Encoder", "base": "any", "type": "qwen3_encoder"},
				}, func(w http.ResponseWriter, _ *http.Request) {
					enqueueRequests.Add(1)
					http.Error(w, "must not enqueue", http.StatusInternalServerError)
				})
			},
			args: []string{
				"generate", "--no-wait", "--model", "main-key", "--prompt", "test",
				"--vae", "FLUX VAE", "--qwen3-encoder", "encoder-key",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			var enqueueRequests atomic.Int32
			server := test.server(&enqueueRequests)
			defer server.Close()
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app := cli.New(&stdout, &stderr)
			args := append(slices.Clone(test.args), "--url", server.URL, "--json")

			exitCode := app.Run(t.Context(), args)

			if exitCode != result.ExitUnsupportedCapability || stderr.Len() != 0 || enqueueRequests.Load() != 0 {
				t.Fatalf("exit code = %d, enqueue requests = %d, stderr = %q, stdout = %q", exitCode, enqueueRequests.Load(), stderr.String(), stdout.String())
			}
			var envelope result.Envelope
			if err := jsonv2.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Error == nil || envelope.Error.Code != result.CodeUnsupportedCapability {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
		})
	}
}

func TestGenerateReturnsSelectionRequiredBeforeEnqueueForAmbiguousModelName(t *testing.T) {
	isolateUserConfigDir(t)
	var enqueueRequests atomic.Int32
	server := newAnimaGenerationServer(nil, []map[string]any{
		{"key": "main-b", "hash": "blake3:main-b", "name": "Same Anima", "base": "anima", "type": "main"},
		{"key": "main-a", "hash": "blake3:main-a", "name": "Same Anima", "base": "anima", "type": "main"},
		{"key": "vae-key", "hash": "blake3:vae", "name": "Anima VAE", "base": "anima", "type": "vae"},
		{"key": "encoder-key", "hash": "blake3:encoder", "name": "Qwen3 Encoder", "base": "any", "type": "qwen3_encoder"},
	}, func(w http.ResponseWriter, _ *http.Request) {
		enqueueRequests.Add(1)
		http.Error(w, "must not enqueue", http.StatusInternalServerError)
	})
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{
		"generate", "--no-wait", "--model", "Same Anima", "--prompt", "test", "--negative-prompt", "",
		"--width", "768", "--height", "768", "--steps", "20", "--scheduler", "euler", "--guidance", "4.5",
		"--seed", "7", "--output-count", "1", "--vae", "vae-key", "--qwen3-encoder", "encoder-key",
		"--url", server.URL, "--json",
	})

	if exitCode != result.ExitSelectionRequired || stderr.Len() != 0 || enqueueRequests.Load() != 0 {
		t.Fatalf("exit code = %d, enqueue requests = %d, stderr = %q, stdout = %q", exitCode, enqueueRequests.Load(), stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := jsonv2.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "selection_required" || envelope.Error.Details["selector"] != "Same Anima" || envelope.Error.Details["kind"] != "main_model" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
	candidates, ok := envelope.Error.Details["candidates"].([]any)
	if !ok || len(candidates) != 2 || candidates[0].(map[string]any)["key"] != "main-a" || candidates[1].(map[string]any)["key"] != "main-b" {
		t.Fatalf("unexpected candidates: %#v", envelope.Error.Details["candidates"])
	}
}

func TestGenerateReturnsSelectionRequiredBeforeEnqueueForMultipleCompatibleComponents(t *testing.T) {
	isolateUserConfigDir(t)
	var enqueueRequests atomic.Int32
	server := newAnimaGenerationServer(nil, []map[string]any{
		{"key": "main-key", "hash": "blake3:main", "name": "Anima Main", "base": "anima", "type": "main"},
		{"key": "vae-b", "hash": "blake3:vae-b", "name": "VAE B", "base": "anima", "type": "vae"},
		{"key": "encoder-key", "hash": "blake3:encoder", "name": "Qwen3 Encoder", "base": "any", "type": "qwen3_encoder"},
		{"key": "vae-a", "hash": "blake3:vae-a", "name": "VAE A", "base": "anima", "type": "vae"},
	}, func(w http.ResponseWriter, _ *http.Request) {
		enqueueRequests.Add(1)
		http.Error(w, "must not enqueue", http.StatusInternalServerError)
	})
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{
		"generate", "--no-wait", "--model", "main-key", "--prompt", "test", "--url", server.URL, "--json",
	})

	if exitCode != result.ExitSelectionRequired || stderr.Len() != 0 || enqueueRequests.Load() != 0 {
		t.Fatalf("exit code = %d, enqueue requests = %d, stderr = %q, stdout = %q", exitCode, enqueueRequests.Load(), stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := jsonv2.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error == nil || envelope.Error.Code != result.CodeSelectionRequired ||
		envelope.Error.Details["kind"] != "vae" || envelope.Error.Details["selector"] != "" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
	candidates, ok := envelope.Error.Details["candidates"].([]any)
	if !ok || len(candidates) != 2 || candidates[0].(map[string]any)["key"] != "vae-a" || candidates[1].(map[string]any)["key"] != "vae-b" {
		t.Fatalf("unexpected candidates: %#v", envelope.Error.Details["candidates"])
	}
}

func TestGenerateReportsMissingComponentsBeforeEnqueue(t *testing.T) {
	tests := []struct {
		name          string
		models        []map[string]any
		componentType string
		requiredBase  string
		requiredType  string
	}{
		{
			name: "Anima VAE",
			models: []map[string]any{
				{"key": "main-key", "hash": "blake3:main", "name": "Anima Main", "base": "anima", "type": "main"},
				{"key": "encoder-key", "hash": "blake3:encoder", "name": "Qwen3 Encoder", "base": "any", "type": "qwen3_encoder"},
			},
			componentType: "vae", requiredBase: "anima", requiredType: "vae",
		},
		{
			name: "Qwen3 encoder",
			models: []map[string]any{
				{"key": "main-key", "hash": "blake3:main", "name": "Anima Main", "base": "anima", "type": "main"},
				{"key": "vae-key", "hash": "blake3:vae", "name": "Anima VAE", "base": "anima", "type": "vae"},
			},
			componentType: "qwen3_encoder", requiredBase: "any", requiredType: "qwen3_encoder",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			var enqueueRequests atomic.Int32
			server := newAnimaGenerationServer(nil, test.models, func(w http.ResponseWriter, _ *http.Request) {
				enqueueRequests.Add(1)
				http.Error(w, "must not enqueue", http.StatusInternalServerError)
			})
			defer server.Close()
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app := cli.New(&stdout, &stderr)

			exitCode := app.Run(t.Context(), []string{
				"generate", "--no-wait", "--model", "main-key", "--prompt", "test", "--url", server.URL, "--json",
			})

			if exitCode != result.ExitUnsupportedCapability || stderr.Len() != 0 || enqueueRequests.Load() != 0 {
				t.Fatalf("exit code = %d, enqueue requests = %d, stderr = %q, stdout = %q", exitCode, enqueueRequests.Load(), stderr.String(), stdout.String())
			}
			var envelope result.Envelope
			if err := jsonv2.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Error == nil || envelope.Error.Code != "missing_component" ||
				envelope.Error.Details["component_type"] != test.componentType ||
				envelope.Error.Details["required_base"] != test.requiredBase ||
				envelope.Error.Details["required_type"] != test.requiredType ||
				envelope.Error.Details["installation_guidance"] == "" {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
		})
	}
}

func TestGenerateRejectsIncompleteModelIdentifiersBeforeEnqueue(t *testing.T) {
	isolateUserConfigDir(t)
	var enqueueRequests atomic.Int32
	server := newAnimaGenerationServer(nil, []map[string]any{
		{"key": "main-key", "name": "Anima Main", "base": "anima", "type": "main"},
		{"key": "vae-key", "hash": "blake3:vae", "name": "Anima VAE", "base": "anima", "type": "vae"},
		{"key": "encoder-key", "hash": "blake3:encoder", "name": "Qwen3 Encoder", "base": "any", "type": "qwen3_encoder"},
	}, func(w http.ResponseWriter, _ *http.Request) {
		enqueueRequests.Add(1)
		http.Error(w, "must not enqueue", http.StatusInternalServerError)
	})
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{
		"generate", "--no-wait", "--model", "main-key", "--prompt", "test",
		"--width", "768", "--height", "768", "--steps", "20", "--scheduler", "euler", "--guidance", "4.5",
		"--seed", "7", "--output-count", "1", "--vae", "vae-key", "--qwen3-encoder", "encoder-key",
		"--url", server.URL, "--json",
	})

	if exitCode != result.ExitUnsupportedCapability || stderr.Len() != 0 || enqueueRequests.Load() != 0 {
		t.Fatalf("exit code = %d, enqueue requests = %d, stderr = %q, stdout = %q", exitCode, enqueueRequests.Load(), stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := jsonv2.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error == nil || envelope.Error.Code != "unsupported_capability" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestGenerateRequestDocumentCompilesToExactAnimaOperation(t *testing.T) {
	isolateUserConfigDir(t)
	fixture, err := os.ReadFile("../generation/testdata/anima_6_14_enqueue.json")
	if err != nil {
		t.Fatal(err)
	}
	var wantEnqueue any
	if err := jsonv2.Unmarshal(fixture, &wantEnqueue); err != nil {
		t.Fatal(err)
	}
	server := newAnimaGenerationServer(nil, []map[string]any{
		{"key": "main-key", "hash": "blake3:main", "name": "Anima Main", "base": "anima", "type": "main"},
		{"key": "vae-key", "hash": "blake3:vae", "name": "Anima VAE", "base": "anima", "type": "vae"},
		{"key": "encoder-key", "hash": "blake3:encoder", "name": "Qwen3 Encoder", "base": "any", "type": "qwen3_encoder"},
	}, func(w http.ResponseWriter, r *http.Request) {
		var gotEnqueue any
		if err := jsonv2.UnmarshalRead(r.Body, &gotEnqueue); err != nil {
			t.Errorf("decode enqueue request: %v", err)
			return
		}
		if !reflect.DeepEqual(gotEnqueue, wantEnqueue) {
			t.Errorf("request document and flags compiled differently\ngot:  %#v\nwant: %#v", gotEnqueue, wantEnqueue)
		}
		_ = jsonv2.MarshalWrite(w, map[string]any{
			"queue_id": "default", "enqueued": 1, "requested": 1, "item_ids": []int{18},
			"batch": map[string]any{"batch_id": "batch-2"},
		})
	})
	defer server.Close()
	request := `{"schema_version":1,"model":"main-key","positive_prompt":"a lighthouse in a storm","negative_prompt":"text","width":768,"height":1024,"steps":24,"scheduler":"heun","guidance":4.25,"seed":42,"output_count":1,"board_id":"board-1","components":{"vae":"vae-key","qwen3_encoder":"encoder-key"}}`
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(request), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"generate", "--no-wait", "--request", "-", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
}

func TestGenerateRejectsUnknownOrMixedRequestDocumentFieldsBeforeNetwork(t *testing.T) {
	for _, test := range []struct {
		name    string
		request string
		args    []string
	}{
		{
			name:    "unknown request field",
			request: `{"schema_version":1,"model":"main-key","positive_prompt":"test","typo":true}`,
			args:    []string{"generate", "--no-wait", "--request", "-"},
		},
		{
			name:    "missing schema version",
			request: `{"model":"main-key","positive_prompt":"test"}`,
			args:    []string{"generate", "--no-wait", "--request", "-"},
		},
		{
			name:    "request mixed with operation flag",
			request: `{"schema_version":1,"model":"main-key","positive_prompt":"test"}`,
			args:    []string{"generate", "--no-wait", "--request", "-", "--model", "main-key"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				requests.Add(1)
			}))
			defer server.Close()
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app := cli.NewWithIO(strings.NewReader(test.request), &stdout, &stderr)
			args := append(slices.Clone(test.args), "--url", server.URL, "--json")

			exitCode := app.Run(t.Context(), args)

			if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 || requests.Load() != 0 {
				t.Fatalf("exit code = %d, requests = %d, stderr = %q, stdout = %q", exitCode, requests.Load(), stderr.String(), stdout.String())
			}
			var envelope result.Envelope
			if err := jsonv2.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Error == nil || envelope.Error.Code != "invalid_request" {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
		})
	}
}

func TestGenerateRejectsSeedsOutsideUnsigned32BitRangeBeforeNetwork(t *testing.T) {
	for _, seed := range []string{"-1", "4294967296"} {
		t.Run(seed, func(t *testing.T) {
			isolateUserConfigDir(t)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				requests.Add(1)
			}))
			defer server.Close()
			request := `{"schema_version":1,"model":"main-key","positive_prompt":"test","seed":` + seed + `}`
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app := cli.NewWithIO(strings.NewReader(request), &stdout, &stderr)

			exitCode := app.Run(t.Context(), []string{"generate", "--no-wait", "--request", "-", "--url", server.URL, "--json"})

			if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 || requests.Load() != 0 {
				t.Fatalf("exit code = %d, requests = %d, stderr = %q, stdout = %q", exitCode, requests.Load(), stderr.String(), stdout.String())
			}
			var envelope result.Envelope
			if err := jsonv2.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Error == nil || envelope.Error.Code != result.CodeInvalidRequest {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
		})
	}
}

func TestGenerateDoesNotRetryInconclusiveEnqueue(t *testing.T) {
	isolateUserConfigDir(t)
	var enqueueRequests atomic.Int32
	server := newAnimaGenerationServer(nil, animaModelInventory(), func(w http.ResponseWriter, _ *http.Request) {
		enqueueRequests.Add(1)
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack enqueue connection: %v", err)
			return
		}
		_ = connection.Close()
	})
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{
		"generate", "--no-wait", "--model", "main-key", "--prompt", "test",
		"--width", "768", "--height", "768", "--steps", "20", "--scheduler", "euler", "--guidance", "4.5",
		"--seed", "7", "--output-count", "1", "--vae", "vae-key", "--qwen3-encoder", "encoder-key",
		"--url", server.URL, "--json",
	})

	if exitCode != result.ExitInvokeAIFailure || stderr.Len() != 0 || enqueueRequests.Load() != 1 {
		t.Fatalf("exit code = %d, enqueue requests = %d, stderr = %q, stdout = %q", exitCode, enqueueRequests.Load(), stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := jsonv2.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error == nil || envelope.Error.Code != "outcome_unknown" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestGenerateTreatsIncompleteSuccessfulEnqueueResponseAsOutcomeUnknown(t *testing.T) {
	isolateUserConfigDir(t)
	var enqueueRequests atomic.Int32
	server := newAnimaGenerationServer(nil, animaModelInventory(), func(w http.ResponseWriter, _ *http.Request) {
		enqueueRequests.Add(1)
		_ = jsonv2.MarshalWrite(w, map[string]any{})
	})
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{
		"generate", "--no-wait", "--model", "main-key", "--prompt", "test",
		"--width", "768", "--height", "768", "--steps", "20", "--scheduler", "euler", "--guidance", "4.5",
		"--seed", "7", "--output-count", "1", "--vae", "vae-key", "--qwen3-encoder", "encoder-key",
		"--url", server.URL, "--json",
	})

	if exitCode != result.ExitInvokeAIFailure || stderr.Len() != 0 || enqueueRequests.Load() != 1 {
		t.Fatalf("exit code = %d, enqueue requests = %d, stderr = %q, stdout = %q", exitCode, enqueueRequests.Load(), stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := jsonv2.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error == nil || envelope.Error.Code != "outcome_unknown" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestGenerateTreatsPartiallyAcceptedBatchAsOutcomeUnknown(t *testing.T) {
	isolateUserConfigDir(t)
	var enqueueRequests atomic.Int32
	server := newAnimaGenerationServer(nil, animaModelInventory(), func(w http.ResponseWriter, _ *http.Request) {
		enqueueRequests.Add(1)
		_ = jsonv2.MarshalWrite(w, map[string]any{
			"queue_id": "default", "enqueued": 1, "requested": 2, "item_ids": []int{17},
			"batch": map[string]any{"batch_id": "partial-batch"},
		})
	})
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{
		"generate", "--no-wait", "--model", "main-key", "--prompt", "test", "--seed", "7", "--output-count", "2",
		"--vae", "vae-key", "--qwen3-encoder", "encoder-key", "--url", server.URL, "--json",
	})

	if exitCode != result.ExitInvokeAIFailure || stderr.Len() != 0 || enqueueRequests.Load() != 1 {
		t.Fatalf("exit code = %d, enqueues = %d, stderr = %q, stdout = %q", exitCode, enqueueRequests.Load(), stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error == nil || envelope.Error.Code != result.CodeOutcomeUnknown {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestGenerateRejectsNonFiniteGuidanceAfterInventory(t *testing.T) {
	for _, guidance := range []string{"NaN", "+Inf"} {
		t.Run(guidance, func(t *testing.T) {
			isolateUserConfigDir(t)
			var inventoryReads, enqueues atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v2/models/" {
					inventoryReads.Add(1)
				}
				if serveAnimaPreflight(w, r, "6.14.1", animaOpenAPIFixture("", ""), animaModelInventory()) {
					return
				}
				if r.URL.Path == "/api/v1/queue/default/enqueue_batch" {
					enqueues.Add(1)
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app := cli.New(&stdout, &stderr)

			exitCode := app.Run(t.Context(), []string{
				"generate", "--no-wait", "--model", "main-key", "--prompt", "test",
				"--width", "768", "--height", "768", "--steps", "20", "--scheduler", "euler", "--guidance", guidance,
				"--seed", "7", "--output-count", "1", "--vae", "vae-key", "--qwen3-encoder", "encoder-key",
				"--url", server.URL, "--json",
			})

			if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 || inventoryReads.Load() != 1 || enqueues.Load() != 0 {
				t.Fatalf("exit code = %d, inventory reads = %d, enqueues = %d, stderr = %q, stdout = %q", exitCode, inventoryReads.Load(), enqueues.Load(), stderr.String(), stdout.String())
			}
			var envelope result.Envelope
			if err := jsonv2.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Error == nil || envelope.Error.Code != "invalid_request" {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
		})
	}
}

func TestGenerateRejectsNonPositiveOutputCountAfterInventory(t *testing.T) {
	for _, outputCount := range []string{"0", "-1"} {
		t.Run(outputCount, func(t *testing.T) {
			isolateUserConfigDir(t)
			var inventoryReads, enqueues atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v2/models/" {
					inventoryReads.Add(1)
				}
				if serveAnimaPreflight(w, r, "6.14.1", animaOpenAPIFixture("", ""), animaModelInventory()) {
					return
				}
				if r.URL.Path == "/api/v1/queue/default/enqueue_batch" {
					enqueues.Add(1)
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app := cli.New(&stdout, &stderr)

			exitCode := app.Run(t.Context(), []string{
				"generate", "--no-wait", "--model", "main-key", "--prompt", "test",
				"--output-count", outputCount, "--url", server.URL, "--json",
			})

			if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 || inventoryReads.Load() != 1 || enqueues.Load() != 0 {
				t.Fatalf("exit code = %d, inventory reads = %d, enqueues = %d, stderr = %q, stdout = %q", exitCode, inventoryReads.Load(), enqueues.Load(), stderr.String(), stdout.String())
			}
			var envelope result.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Error == nil || envelope.Error.Code != result.CodeInvalidRequest {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
		})
	}
}

// generateWaitCounters records the requests a waiting generate performs so a
// test can prove the enqueue mutation is sent exactly once, polling stays
// read-only, and no cancellation endpoint is called.
type generateWaitCounters struct {
	enqueues  atomic.Int32
	itemPolls atomic.Int32
	imageGets atomic.Int32
	mutex     sync.Mutex
	mutations []string
}

func (c *generateWaitCounters) recordMutation(method, path string) {
	if method == http.MethodGet {
		return
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.mutations = append(c.mutations, method+" "+path)
}

func (c *generateWaitCounters) mutationRequests() []string {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return slices.Clone(c.mutations)
}

// newGenerateWaitServer serves the version, model inventory, single enqueue, and
// scripted queue-item polls of a waiting generate. items holds one queue-item
// payload per poll and repeats its last entry; transientFailures counts leading
// item polls answered with a service-unavailable failure; onPoll runs after each
// item response so a test can interrupt or observe the wait.
func newGenerateWaitServer(t *testing.T, items []map[string]any, transientFailures int, onPoll func(poll int)) (*httptest.Server, *generateWaitCounters) {
	t.Helper()
	counters := &generateWaitCounters{}
	var failures atomic.Int32
	failures.Store(int32(transientFailures))
	models := []map[string]any{
		{"key": "main-key", "hash": "blake3:main", "name": "Anima Main", "base": "anima", "type": "main"},
		{"key": "vae-key", "hash": "blake3:vae", "name": "Anima VAE", "base": "anima", "type": "vae"},
		{"key": "encoder-key", "hash": "blake3:encoder", "name": "Qwen3 Encoder", "base": "any", "type": "qwen3_encoder"},
	}
	openAPI := animaOpenAPIFixture("", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counters.recordMutation(r.Method, r.URL.Path)
		if serveAnimaPreflight(w, r, "6.14.1", openAPI, models) {
			return
		}
		switch {
		case r.URL.Path == "/api/v1/queue/default/enqueue_batch":
			counters.enqueues.Add(1)
			_ = jsonv2.MarshalWrite(w, map[string]any{
				"queue_id": "default", "enqueued": 1, "requested": 1, "item_ids": []int{17},
				"batch": map[string]any{"batch_id": "batch-1"},
			})
		case r.URL.Path == "/api/v1/recall/default":
			_, _ = w.Write([]byte(`{"status":"success"}`))
		case r.URL.Path == "/api/v1/queue/default/i/17":
			requests := int(counters.itemPolls.Add(1))
			if failures.Load() > 0 {
				failures.Add(-1)
				http.Error(w, "queue inspection is temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			poll := requests - 1 - transientFailures
			_ = jsonv2.MarshalWrite(w, items[min(poll, len(items)-1)])
			if onPoll != nil {
				onPoll(poll)
			}
		case strings.HasPrefix(r.URL.Path, "/api/v1/images/i/"):
			counters.imageGets.Add(1)
			name := strings.TrimPrefix(r.URL.Path, "/api/v1/images/i/")
			if name == "missing.png" {
				http.NotFound(w, r)
				return
			}
			responseName := name
			if name == "mismatched.png" {
				responseName = "other.png"
			}
			_ = jsonv2.MarshalWrite(w, map[string]any{
				"image_name": responseName, "image_url": "/api/v1/images/i/" + name + "/full",
				"thumbnail_url": "/api/v1/images/i/" + name + "/thumbnail",
				"image_origin":  "internal", "image_category": "general", "width": 768, "height": 1024,
				"created_at": "2026-01-01 00:00:05.000", "updated_at": "2026-01-01 00:00:05.000",
				"is_intermediate": false, "starred": false, "has_workflow": false,
				"session_id": "session-1", "node_id": "node-1",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, counters
}

// queueItemPayload is one InvokeAI queue-item record as queue get returns it.
func queueItemPayload(status string, extra map[string]any) map[string]any {
	item := map[string]any{
		"item_id": 17, "queue_id": "default", "batch_id": "batch-1", "session_id": "session-1",
		"status": status, "priority": 0,
		"created_at": "2026-01-01 00:00:00.000", "updated_at": "2026-01-01 00:00:00.000",
		"field_values": []map[string]any{{"node_path": "seed", "field_name": "value", "value": 42}},
	}
	for key, value := range extra {
		item[key] = value
	}
	return item
}

// completedItemResults is the queue-item session result of completed image
// outputs, keyed the way InvokeAI records one result per invocation node.
func completedItemResults(imageNames ...string) map[string]any {
	results := make(map[string]any, len(imageNames))
	for index, name := range imageNames {
		results["node-"+strconv.Itoa(index+1)] = map[string]any{
			"type": "image_output", "image": map[string]any{"image_name": name},
		}
	}
	return map[string]any{"results": results}
}

// generateWaitArgs is the convenience-flag form of one exact Anima request.
func generateWaitArgs(serverURL string, extra ...string) []string {
	return slices.Concat([]string{
		"generate", "--model", "main-key", "--prompt", "a lighthouse in a storm", "--negative-prompt", "text",
		"--width", "768", "--height", "1024", "--steps", "24", "--scheduler", "heun", "--guidance", "4.25",
		"--seed", "42", "--output-count", "1", "--vae", "vae-key", "--qwen3-encoder", "encoder-key",
		"--url", serverURL,
	}, extra)
}

// receiptEnvelope is the public Execution Receipt envelope; outputs is empty for
// an accepted --no-wait result.
type receiptEnvelope struct {
	SchemaVersion int    `json:"schema_version"`
	OK            bool   `json:"ok"`
	Operation     string `json:"operation"`
	Data          struct {
		SubmittedRequest map[string]any `json:"submitted_request"`
		ResolvedSettings struct {
			NegativePrompt string            `json:"negative_prompt"`
			Width          int               `json:"width"`
			Height         int               `json:"height"`
			Steps          int               `json:"steps"`
			Scheduler      string            `json:"scheduler"`
			Guidance       float64           `json:"guidance"`
			OutputCount    int               `json:"output_count"`
			BoardID        string            `json:"board_id"`
			ModelKey       string            `json:"model_key"`
			ComponentKeys  map[string]string `json:"component_keys"`
			Seeds          []uint32          `json:"seeds"`
		} `json:"resolved_settings"`
		Queue struct {
			QueueID string `json:"queue_id"`
			BatchID string `json:"batch_id"`
			ItemIDs []int  `json:"item_ids"`
		} `json:"queue"`
		Outputs []struct {
			ItemID int    `json:"item_id"`
			Seed   uint32 `json:"seed"`
			Image  struct {
				ImageName     string  `json:"image_name"`
				ImageURL      string  `json:"image_url"`
				ThumbnailURL  string  `json:"thumbnail_url"`
				ImageOrigin   string  `json:"image_origin"`
				ImageCategory string  `json:"image_category"`
				Width         int     `json:"width"`
				Height        int     `json:"height"`
				SessionID     *string `json:"session_id"`
				NodeID        *string `json:"node_id"`
				BoardID       *string `json:"board_id"`
			} `json:"image"`
		} `json:"outputs"`
		Warnings []result.Warning `json:"warnings"`
	} `json:"data"`
	Warnings []result.Warning `json:"warnings"`
}

// queueFailure is the public structured failure of a generation whose queue
// item did not produce a completed receipt.
type queueFailure struct {
	SchemaVersion int    `json:"schema_version"`
	OK            bool   `json:"ok"`
	Operation     string `json:"operation"`
	Error         *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details struct {
			QueueID string `json:"queue_id"`
			BatchID string `json:"batch_id"`
			ItemIDs []int  `json:"item_ids"`
			ItemID  int    `json:"item_id"`
			Status  string `json:"status"`
		} `json:"details"`
	} `json:"error"`
	Warnings []result.Warning `json:"warnings"`
}

func TestGenerateWaitsForCompletionAndReturnsCompletedReceipt(t *testing.T) {
	isolateUserConfigDir(t)
	server, counters := newGenerateWaitServer(t, []map[string]any{
		queueItemPayload("pending", nil),
		queueItemPayload("waiting", nil),
		queueItemPayload("in_progress", map[string]any{"started_at": "2026-01-01 00:00:01.000"}),
		queueItemPayload("completed", map[string]any{
			"completed_at": "2026-01-01 00:00:05.000",
			"session":      completedItemResults("generated.png"),
		}),
	}, 0, nil)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), generateWaitArgs(server.URL, "--json"))

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if counters.enqueues.Load() != 1 || counters.itemPolls.Load() < 3 || counters.imageGets.Load() != 1 {
		t.Fatalf("enqueues = %d, item polls = %d, image gets = %d", counters.enqueues.Load(), counters.itemPolls.Load(), counters.imageGets.Load())
	}
	if mutations := counters.mutationRequests(); !slices.Equal(mutations, generationSyncMutations) {
		t.Fatalf("non-read requests = %v, want one enqueue then one Recall", mutations)
	}
	var envelope receiptEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.SchemaVersion != 1 || !envelope.OK || envelope.Operation != "generate" ||
		envelope.Data.SubmittedRequest["model"] != "main-key" || envelope.Data.ResolvedSettings.ModelKey != "main-key" ||
		!reflect.DeepEqual(envelope.Data.ResolvedSettings.ComponentKeys, map[string]string{"vae": "vae-key", "qwen3_encoder": "encoder-key"}) ||
		!reflect.DeepEqual(envelope.Data.ResolvedSettings.Seeds, []uint32{42}) ||
		envelope.Data.Queue.QueueID != "default" || envelope.Data.Queue.BatchID != "batch-1" ||
		!reflect.DeepEqual(envelope.Data.Queue.ItemIDs, []int{17}) {
		t.Fatalf("unexpected receipt: %#v", envelope)
	}
	assertPartialSyncWarning(t, envelope.Data.Warnings, envelope.Warnings)
	if len(envelope.Data.Outputs) != 1 {
		t.Fatalf("outputs = %#v, want one completed output", envelope.Data.Outputs)
	}
	output := envelope.Data.Outputs[0]
	if output.ItemID != 17 || output.Seed != 42 ||
		output.Image.ImageName != "generated.png" ||
		output.Image.ImageURL != server.URL+"/api/v1/images/i/generated.png/full" ||
		output.Image.ThumbnailURL != server.URL+"/api/v1/images/i/generated.png/thumbnail" ||
		output.Image.ImageOrigin != "internal" || output.Image.ImageCategory != "general" ||
		output.Image.Width != 768 || output.Image.Height != 1024 ||
		output.Image.SessionID == nil || *output.Image.SessionID != "session-1" ||
		output.Image.NodeID == nil || *output.Image.NodeID != "node-1" {
		t.Fatalf("unexpected completed output: %#v", output)
	}
}

func TestGenerateWaitsForOrderedBatchCompletion(t *testing.T) {
	isolateUserConfigDir(t)
	var enqueueRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", animaOpenAPIFixture("", ""), animaModelInventory()) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/queue/default/enqueue_batch":
			enqueueRequests.Add(1)
			var payload struct {
				Batch struct {
					Data [][]struct {
						Items []uint32 `json:"items"`
					} `json:"data"`
					Graph struct {
						Nodes map[string]json.RawMessage `json:"nodes"`
					} `json:"graph"`
				} `json:"batch"`
			}
			if err := jsonv2.UnmarshalRead(r.Body, &payload); err != nil {
				t.Errorf("decode enqueue request: %v", err)
				return
			}
			if len(payload.Batch.Data) != 1 || len(payload.Batch.Data[0]) != 1 ||
				!slices.Equal(payload.Batch.Data[0][0].Items, []uint32{43, 42}) {
				t.Errorf("batch seed data = %#v", payload.Batch.Data)
			}
			var decode map[string]any
			if err := json.Unmarshal(payload.Batch.Graph.Nodes["decode"], &decode); err != nil {
				t.Errorf("decode output node: %v", err)
			} else if _, exists := decode["board"]; exists {
				t.Errorf("decode node unexpectedly contains a board: %#v", decode)
			}
			_ = jsonv2.MarshalWrite(w, map[string]any{
				"queue_id": "default", "enqueued": 2, "requested": 2, "item_ids": []int{23, 22},
				"batch": map[string]any{"batch_id": "batch-2"},
			})
		case "/api/v1/queue/default/i/23":
			_ = jsonv2.MarshalWrite(w, completedBatchItem(23, "batch-2", 42, "first.png"))
		case "/api/v1/queue/default/i/22":
			_ = jsonv2.MarshalWrite(w, completedBatchItem(22, "batch-2", 43, "second.png"))
		case "/api/v1/images/i/first.png", "/api/v1/images/i/second.png":
			name := strings.TrimPrefix(r.URL.Path, "/api/v1/images/i/")
			_ = jsonv2.MarshalWrite(w, map[string]any{
				"image_name": name, "image_url": r.URL.Path + "/full", "thumbnail_url": r.URL.Path + "/thumbnail",
				"image_origin": "internal", "image_category": "general", "width": 1024, "height": 1024,
				"created_at": "2026-01-01", "updated_at": "2026-01-01", "is_intermediate": false,
				"starred": false, "has_workflow": false,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{
		"generate", "--model", "main-key", "--prompt", "test", "--seed", "42", "--output-count", "2",
		"--vae", "vae-key", "--qwen3-encoder", "encoder-key", "--url", server.URL, "--json",
	})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 || enqueueRequests.Load() != 1 {
		t.Fatalf("exit code = %d, enqueues = %d, stderr = %q, stdout = %q", exitCode, enqueueRequests.Load(), stderr.String(), stdout.String())
	}
	var envelope receiptEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(envelope.Data.ResolvedSettings.Seeds, []uint32{42, 43}) ||
		!slices.Equal(envelope.Data.Queue.ItemIDs, []int{23, 22}) || len(envelope.Data.Outputs) != 2 {
		t.Fatalf("unexpected receipt: %#v", envelope.Data)
	}
	for index, want := range []struct {
		itemID int
		seed   uint32
		image  string
	}{{23, 42, "first.png"}, {22, 43, "second.png"}} {
		output := envelope.Data.Outputs[index]
		if output.ItemID != want.itemID || output.Seed != want.seed || output.Image.ImageName != want.image || output.Image.BoardID != nil {
			t.Fatalf("output %d = %#v", index, output)
		}
	}
}

func completedBatchItem(itemID int, batchID string, seed uint32, imageName string) map[string]any {
	return map[string]any{
		"item_id": itemID, "queue_id": "default", "batch_id": batchID, "session_id": "session-" + strconv.Itoa(itemID),
		"status": "completed", "priority": 0, "created_at": "2026-01-01", "updated_at": "2026-01-01",
		"field_values": []map[string]any{{"node_path": "seed", "field_name": "value", "value": seed}},
		"session":      completedItemResults(imageName),
	}
}

func TestGenerateWaitSurvivesTransientPollFailures(t *testing.T) {
	isolateUserConfigDir(t)
	server, counters := newGenerateWaitServer(t, []map[string]any{
		queueItemPayload("in_progress", nil),
		queueItemPayload("completed", map[string]any{"session": completedItemResults("generated.png")}),
	}, 1, nil)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), generateWaitArgs(server.URL, "--json"))

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope receiptEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if len(envelope.Data.Outputs) != 1 || envelope.Data.Outputs[0].ItemID != 17 {
		t.Fatalf("a transient read failure ended the wait: %#v", envelope.Data.Outputs)
	}
	if counters.enqueues.Load() != 1 || counters.itemPolls.Load() < 2 {
		t.Fatalf("enqueues = %d, item polls = %d", counters.enqueues.Load(), counters.itemPolls.Load())
	}
	if mutations := counters.mutationRequests(); !slices.Equal(mutations, generationSyncMutations) {
		t.Fatalf("non-read requests = %v, want one enqueue then one Recall", mutations)
	}
}

func TestGenerateWaitTimeoutReportsTheUncanceledItem(t *testing.T) {
	isolateUserConfigDir(t)
	server, counters := newGenerateWaitServer(t, []map[string]any{queueItemPayload("in_progress", nil)}, 0, nil)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), generateWaitArgs(server.URL, "--timeout", "150ms", "--json"))

	if exitCode != result.ExitInvokeAIFailure || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope queueFailure
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.OK || envelope.Operation != "generate" || envelope.Error == nil || envelope.Error.Code != result.CodeWaitTimeout {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
	if envelope.Error.Details.QueueID != "default" || envelope.Error.Details.BatchID != "batch-1" ||
		!reflect.DeepEqual(envelope.Error.Details.ItemIDs, []int{17}) {
		t.Fatalf("timeout details = %#v, want the accepted queue identifiers", envelope.Error.Details)
	}
	if !strings.Contains(strings.ToLower(envelope.Error.Message), "not cancel") {
		t.Fatalf("timeout message does not state that the remote item was not canceled: %q", envelope.Error.Message)
	}
	if counters.enqueues.Load() != 1 || counters.itemPolls.Load() < 1 {
		t.Fatalf("enqueues = %d, item polls = %d", counters.enqueues.Load(), counters.itemPolls.Load())
	}
	if mutations := counters.mutationRequests(); !slices.Equal(mutations, generationSyncMutations) {
		t.Fatalf("non-read requests = %v, want one enqueue then one Recall", mutations)
	}
}

func TestGenerateInterruptionStopsOnlyTheLocalWait(t *testing.T) {
	isolateUserConfigDir(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server, counters := newGenerateWaitServer(t, []map[string]any{queueItemPayload("in_progress", nil)}, 0, func(int) { cancel() })
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(ctx, generateWaitArgs(server.URL, "--json"))

	if exitCode != result.ExitInterrupted || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope queueFailure
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.OK || envelope.Operation != "generate" || envelope.Error == nil || envelope.Error.Code != result.CodeInterrupted {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
	if envelope.Error.Details.QueueID != "default" || envelope.Error.Details.BatchID != "batch-1" ||
		!reflect.DeepEqual(envelope.Error.Details.ItemIDs, []int{17}) {
		t.Fatalf("interruption details = %#v, want the accepted queue identifiers", envelope.Error.Details)
	}
	if counters.enqueues.Load() != 1 {
		t.Fatalf("enqueues = %d, want the accepted item to be enqueued exactly once", counters.enqueues.Load())
	}
	if mutations := counters.mutationRequests(); !slices.Equal(mutations, generationSyncMutations) {
		t.Fatalf("non-read requests = %v, want one enqueue then one Recall", mutations)
	}
}

func TestGenerateReportsConclusiveItemFailuresWithoutTracebacks(t *testing.T) {
	for _, test := range []struct {
		name        string
		item        map[string]any
		wantStatus  string
		wantMessage []string
	}{
		{
			name: "failed",
			item: queueItemPayload("failed", map[string]any{
				"error_type":      "TypeError",
				"error_message":   "the anima graph rejected the request",
				"error_traceback": "/server/private/traceback",
			}),
			wantStatus:  "failed",
			wantMessage: []string{"queue item 17", "failed", "TypeError", "the anima graph rejected the request"},
		},
		{
			name:        "canceled",
			item:        queueItemPayload("canceled", nil),
			wantStatus:  "canceled",
			wantMessage: []string{"queue item 17", "canceled"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			server, counters := newGenerateWaitServer(t, []map[string]any{test.item}, 0, nil)
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app := cli.New(&stdout, &stderr)

			exitCode := app.Run(t.Context(), generateWaitArgs(server.URL, "--json"))

			if exitCode != result.ExitInvokeAIFailure || stderr.Len() != 0 {
				t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
			}
			var envelope queueFailure
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
			}
			if envelope.OK || envelope.Operation != "generate" || envelope.Error == nil || envelope.Error.Code != result.CodeInvokeAIOperationFailed {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
			for _, want := range test.wantMessage {
				if !strings.Contains(envelope.Error.Message, want) {
					t.Fatalf("failure message %q does not contain %q", envelope.Error.Message, want)
				}
			}
			if envelope.Error.Details.QueueID != "default" || envelope.Error.Details.BatchID != "batch-1" ||
				!reflect.DeepEqual(envelope.Error.Details.ItemIDs, []int{17}) ||
				envelope.Error.Details.ItemID != 17 || envelope.Error.Details.Status != test.wantStatus {
				t.Fatalf("failure details = %#v, want the accepted item", envelope.Error.Details)
			}
			if counters.imageGets.Load() != 0 {
				t.Fatalf("image gets = %d, want no hydration for a failed item", counters.imageGets.Load())
			}
			if output := stdout.String() + stderr.String(); strings.Contains(output, "/server/private/traceback") {
				t.Fatalf("output exposes a server traceback: %q", output)
			}
			if mutations := counters.mutationRequests(); !slices.Equal(mutations, generationSyncMutations) {
				t.Fatalf("non-read requests = %v, want one enqueue then one Recall", mutations)
			}
		})
	}
}

func TestGenerateRejectsQueueResultsOutsideTheTestedContract(t *testing.T) {
	for _, test := range []struct {
		name       string
		item       map[string]any
		wantStatus string
		wantDetail string
	}{
		{
			name:       "undocumented status",
			item:       queueItemPayload("queued", nil),
			wantStatus: "queued",
			wantDetail: `status "queued"`,
		},
		{
			name:       "completed without an image output",
			item:       queueItemPayload("completed", nil),
			wantStatus: "completed",
			wantDetail: "0 image outputs",
		},
		{
			name: "completed with several image outputs",
			item: queueItemPayload("completed", map[string]any{
				"session": completedItemResults("first.png", "second.png"),
			}),
			wantStatus: "completed",
			wantDetail: "2 image outputs",
		},
		{
			name: "completed with duplicate image output records",
			item: queueItemPayload("completed", map[string]any{
				"session": completedItemResults("same.png", "same.png"),
			}),
			wantStatus: "completed",
			wantDetail: "2 image outputs",
		},
		{
			name: "completed with an unavailable image output",
			item: queueItemPayload("completed", map[string]any{
				"session": completedItemResults("missing.png"),
			}),
			wantStatus: "completed",
			wantDetail: "0 accessible Image References",
		},
		{
			name: "completed with a malformed typed image output",
			item: queueItemPayload("completed", map[string]any{
				"session": map[string]any{"results": map[string]any{
					"valid":     map[string]any{"type": "image_output", "image": map[string]any{"image_name": "valid.png"}},
					"malformed": map[string]any{"type": "image_output", "image": "invalid"},
				}},
			}),
			wantStatus: "completed",
			wantDetail: "malformed image output",
		},
		{
			name: "completed with contradictory hydrated image name",
			item: queueItemPayload("completed", map[string]any{
				"session": completedItemResults("mismatched.png"),
			}),
			wantStatus: "completed",
			wantDetail: "contradictory image name",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			server, counters := newGenerateWaitServer(t, []map[string]any{test.item}, 0, nil)
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app := cli.New(&stdout, &stderr)

			exitCode := app.Run(t.Context(), generateWaitArgs(server.URL, "--json"))

			if exitCode != result.ExitInvokeAIFailure || stderr.Len() != 0 {
				t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
			}
			var envelope queueFailure
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
			}
			if envelope.OK || envelope.Operation != "generate" || envelope.Error == nil || envelope.Error.Code != result.CodeInvalidInvokeAIResponse {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
			if !strings.Contains(envelope.Error.Message, test.wantDetail) {
				t.Fatalf("failure message %q does not contain %q", envelope.Error.Message, test.wantDetail)
			}
			if envelope.Error.Details.ItemID != 17 || envelope.Error.Details.Status != test.wantStatus ||
				!reflect.DeepEqual(envelope.Error.Details.ItemIDs, []int{17}) {
				t.Fatalf("failure details = %#v, want the rejected item", envelope.Error.Details)
			}
			if counters.itemPolls.Load() != 1 {
				t.Fatalf("item polls = %d, want the rejected result to stop the wait", counters.itemPolls.Load())
			}
			if mutations := counters.mutationRequests(); !slices.Equal(mutations, generationSyncMutations) {
				t.Fatalf("non-read requests = %v, want one enqueue then one Recall", mutations)
			}
		})
	}
}

func TestGenerateRejectsWaitOptionsThatCannotApply(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "wait timeout without waiting", args: []string{"--no-wait", "--timeout", "1s"}},
		{name: "negative wait timeout", args: []string{"--timeout=-1s"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				requests.Add(1)
			}))
			defer server.Close()
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app := cli.New(&stdout, &stderr)

			exitCode := app.Run(t.Context(), generateWaitArgs(server.URL, slices.Concat(test.args, []string{"--json"})...))

			if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 || requests.Load() != 0 {
				t.Fatalf("exit code = %d, requests = %d, stderr = %q, stdout = %q", exitCode, requests.Load(), stderr.String(), stdout.String())
			}
			var envelope result.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Error == nil || envelope.Error.Code != result.CodeInvalidRequest {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
		})
	}
}

func TestGenerateHumanOutputReportsTheCompletedImage(t *testing.T) {
	isolateUserConfigDir(t)
	server, _ := newGenerateWaitServer(t, []map[string]any{
		queueItemPayload("completed", map[string]any{"session": completedItemResults("generated.png")}),
	}, 0, nil)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), generateWaitArgs(server.URL))

	if exitCode != result.ExitSuccess || !strings.Contains(stderr.String(), "ui_sync_partial:") {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if want := "Generated generated.png for seed 42 from queue item 17\n"; stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestModelsListJSONReturnsSafeModelSummaries(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/models/" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{
			{
				"key": "model-z", "name": "Zeta", "base": "sdxl", "type": "main", "format": "diffusers",
				"file_size": 200, "description": "second", "path": "/server/secret/zeta", "hash": "secret-hash-z",
			},
			{
				"key": "model-a", "name": "Alpha", "base": "anima", "type": "main", "format": "checkpoint",
				"file_size": 100, "description": "first", "source": "https://example.invalid/?token=secret",
			},
		}})
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"models", "list", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	var envelope struct {
		SchemaVersion int    `json:"schema_version"`
		OK            bool   `json:"ok"`
		Operation     string `json:"operation"`
		Data          struct {
			Models []map[string]any `json:"models"`
		} `json:"data"`
		Warnings []result.Warning `json:"warnings"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	wantModels := []map[string]any{
		{"key": "model-a", "name": "Alpha", "base": "anima", "type": "main", "format": "checkpoint", "size_bytes": float64(100), "description": "first"},
		{"key": "model-z", "name": "Zeta", "base": "sdxl", "type": "main", "format": "diffusers", "size_bytes": float64(200), "description": "second"},
	}
	if envelope.SchemaVersion != 1 || !envelope.OK || envelope.Operation != "models.list" || !reflect.DeepEqual(envelope.Data.Models, wantModels) || len(envelope.Warnings) != 0 {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestModelsListAcceptsRequestDocument(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/models/" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{}})
	}))
	defer server.Close()

	requestPath := filepath.Join(t.TempDir(), "models-list.json")
	if err := os.WriteFile(requestPath, []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"models", "list", "--request", requestPath, "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if !envelope.OK || envelope.Operation != "models.list" || stderr.Len() != 0 {
		t.Fatalf("unexpected result: envelope=%#v stderr=%q", envelope, stderr.String())
	}
}

func TestModelsListAcceptsRequestDocumentFromStandardInput(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{}})
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(`{"schema_version":1}`), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"models", "list", "--request", "-", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
}

func TestModelsListRejectsUnsupportedRequestSchemaVersion(t *testing.T) {
	isolateUserConfigDir(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(`{"schema_version":2}`), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"models", "list", "--request", "-", "--url", "http://127.0.0.1:1", "--json"})

	if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestModelsListRejectsNonCanonicalRequestDocuments(t *testing.T) {
	isolateUserConfigDir(t)
	tests := []struct {
		name    string
		request string
	}{
		{name: "unknown field", request: `{"schema_version":1,"typo":true}`},
		{name: "trailing value", request: `{"schema_version":1}{"schema_version":1}`},
		{name: "malformed JSON", request: `{"schema_version":1`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app := cli.NewWithIO(strings.NewReader(test.request), &stdout, &stderr)

			exitCode := app.Run(t.Context(), []string{"models", "list", "--request", "-", "--url", "http://127.0.0.1:1", "--json"})

			if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 {
				t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
			}
			var envelope result.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
			}
			if envelope.Error == nil || envelope.Error.Code != "invalid_request" {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
		})
	}
}

func TestModelsListFlagsCompileToTypedFilters(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if !reflect.DeepEqual(query["base_models"], []string{"anima", "sdxl"}) ||
			query.Get("model_type") != "main" || query.Get("model_format") != "checkpoint" || query.Get("model_name") != "Exact Name" {
			t.Errorf("unexpected query: %v", query)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{}})
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{
		"models", "list", "--base", "anima", "--base", "sdxl", "--type", "main", "--format", "checkpoint", "--name", "Exact Name",
		"--url", server.URL, "--json",
	})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
}

func TestModelsListRejectsMixedRequestDocumentAndOperationFlags(t *testing.T) {
	isolateUserConfigDir(t)
	requestPath := filepath.Join(t.TempDir(), "models-list.json")
	if err := os.WriteFile(requestPath, []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"models", "list", "--request", requestPath, "--base", "anima", "--json"})

	if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.OK || envelope.Operation != "models.list" || envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestModelsListClassifiesConnectionFailure(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"models", "list", "--url", url, "--json"})

	if exitCode != result.ExitConnection || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "connection_failed" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestModelsListClassifiesAuthenticationFailure(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "denied", http.StatusUnauthorized)
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"models", "list", "--url", server.URL, "--token", "wrong", "--json"})

	if exitCode != result.ExitConnection || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "authentication_failed" || strings.Contains(stdout.String(), "wrong") {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestModelsListClassifiesInvalidInvokeAIResponse(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"models", "list", "--url", server.URL, "--json"})

	if exitCode != result.ExitInvokeAIFailure || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "invalid_invokeai_response" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestModelsListClassifiesLocalInterruption(t *testing.T) {
	isolateUserConfigDir(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(ctx, []string{"models", "list", "--url", "http://127.0.0.1:1", "--json"})

	if exitCode != result.ExitInterrupted || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "interrupted" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestImagesListJSONReturnsPaginatedImageReferences(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if r.URL.Path != "/api/v1/images/" || query.Get("offset") != "0" || query.Get("limit") != "20" ||
			query.Get("is_intermediate") != "false" || query.Get("order_dir") != "DESC" || query.Get("starred_first") != "false" {
			t.Errorf("unexpected request path=%q query=%v", r.URL.Path, query)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"offset": 0,
			"limit":  20,
			"total":  1,
			"items": []map[string]any{{
				"image_name": "image-1.png", "image_url": "api/v1/images/i/image-1.png/full", "thumbnail_url": "api/v1/images/i/image-1.png/thumbnail",
				"image_origin": "external", "image_category": "user", "width": 640, "height": 480,
				"created_at": "2026-09-20 10:00:00.000", "updated_at": "2026-09-20 10:01:00.000",
				"is_intermediate": false, "session_id": "session-1", "node_id": "node-1", "starred": true,
				"has_workflow": true, "board_id": "board-1", "deleted_at": nil, "image_subfolder": "private",
			}},
		})
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "list", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope struct {
		OK        bool   `json:"ok"`
		Operation string `json:"operation"`
		Data      struct {
			Offset int              `json:"offset"`
			Limit  int              `json:"limit"`
			Total  int              `json:"total"`
			Items  []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	wantItems := []map[string]any{{
		"image_name": "image-1.png", "image_url": server.URL + "/api/v1/images/i/image-1.png/full", "thumbnail_url": server.URL + "/api/v1/images/i/image-1.png/thumbnail",
		"image_origin": "external", "image_category": "user", "width": float64(640), "height": float64(480),
		"created_at": "2026-09-20 10:00:00.000", "updated_at": "2026-09-20 10:01:00.000", "is_intermediate": false,
		"session_id": "session-1", "node_id": "node-1", "starred": true, "has_workflow": true, "board_id": "board-1",
	}}
	if !envelope.OK || envelope.Operation != "images.list" || envelope.Data.Offset != 0 || envelope.Data.Limit != 20 || envelope.Data.Total != 1 || !reflect.DeepEqual(envelope.Data.Items, wantItems) {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestImagesListFlagsCompileToTypedRequest(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("offset") != "10" || query.Get("limit") != "5" || query.Get("board_id") != "none" {
			t.Errorf("unexpected query: %v", query)
		}
		if _, exists := query["is_intermediate"]; exists {
			t.Errorf("is_intermediate should be omitted when intermediates are included: %v", query)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"offset": 10, "limit": 5, "total": 0, "items": []any{}})
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{
		"images", "list", "--offset", "10", "--limit", "5", "--board", "none", "--include-intermediate",
		"--url", server.URL, "--json",
	})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
}

func TestImagesListAcceptsTypedRequestDocument(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("offset") != "4" || query.Get("limit") != "2" || query.Get("board_id") != "none" {
			t.Errorf("unexpected query: %v", query)
		}
		if _, exists := query["is_intermediate"]; exists {
			t.Errorf("unexpected intermediate filter: %v", query)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"offset": 4, "limit": 2, "total": 0, "items": []any{}})
	}))
	defer server.Close()
	request := `{"schema_version":1,"offset":4,"limit":2,"board_id":"none","include_intermediate":true}`
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(request), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "list", "--request", "-", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
}

func TestImagesListRejectsOutOfRangeLimitAsInvalidRequest(t *testing.T) {
	isolateUserConfigDir(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "list", "--limit", "101", "--url", "http://127.0.0.1:1", "--json"})

	if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestImagesGetJSONReturnsExactImageReference(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/images/i/image-1.png" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"image_name": "image-1.png", "image_url": "api/v1/images/i/image-1.png/full", "thumbnail_url": "api/v1/images/i/image-1.png/thumbnail",
			"image_origin": "internal", "image_category": "general", "width": 1024, "height": 768,
			"created_at": "created", "updated_at": "updated", "is_intermediate": false, "starred": false, "has_workflow": false,
		})
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "get", "image-1.png", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope struct {
		OK        bool   `json:"ok"`
		Operation string `json:"operation"`
		Data      struct {
			Image map[string]any `json:"image"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if !envelope.OK || envelope.Operation != "images.get" || envelope.Data.Image["image_name"] != "image-1.png" ||
		envelope.Data.Image["image_url"] != server.URL+"/api/v1/images/i/image-1.png/full" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestImagesGetAcceptsTypedRequestDocument(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/images/i/from-request.png" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"image_name": "from-request.png", "image_url": "api/v1/images/i/from-request.png/full", "thumbnail_url": "api/v1/images/i/from-request.png/thumbnail",
			"image_origin": "internal", "image_category": "general", "width": 1, "height": 1,
			"created_at": "created", "updated_at": "updated", "is_intermediate": false, "starred": false, "has_workflow": false,
		})
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(`{"schema_version":1,"image_name":"from-request.png"}`), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "get", "--request", "-", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
}

func TestImagesGetRejectsRequestWithoutImageName(t *testing.T) {
	isolateUserConfigDir(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(`{"schema_version":1}`), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "get", "--request", "-", "--url", "http://127.0.0.1:1", "--json"})

	if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestImagesGetClassifiesMissingImage(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "get", "missing.png", "--url", server.URL, "--json"})

	if exitCode != result.ExitInvokeAIFailure || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "not_found" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestQueueListJSONReturnsOnlyRequestedSummaryPage(t *testing.T) {
	isolateUserConfigDir(t)
	var summaries atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/queue/default/item_ids":
			if r.Method != http.MethodGet || r.URL.Query().Get("order_dir") != "DESC" {
				t.Errorf("unexpected item id request: %s %s", r.Method, r.URL.String())
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"item_ids": []int{9, 8, 7}, "total_count": 3})
		case "/api/v1/queue/default/item_summaries_by_ids":
			summaries.Add(1)
			if r.Method != http.MethodPost {
				t.Errorf("summary method = %s, want POST", r.Method)
			}
			var body struct {
				ItemIDs []int `json:"item_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode summary request: %v", err)
				return
			}
			if !reflect.DeepEqual(body.ItemIDs, []int{8}) {
				t.Errorf("hydrated item ids = %v, want [8]", body.ItemIDs)
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"item_id": 8, "status": "completed", "batch_id": "batch-8", "origin": "generate", "destination": "generate",
				"created_at": "created", "started_at": "started", "completed_at": "completed", "device": "cuda:0",
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "list", "--offset", "1", "--limit", "1", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 || summaries.Load() != 1 {
		t.Fatalf("exit code = %d, summary requests = %d, stderr = %q, stdout = %q", exitCode, summaries.Load(), stderr.String(), stdout.String())
	}
	var envelope struct {
		OK        bool   `json:"ok"`
		Operation string `json:"operation"`
		Data      struct {
			Offset int              `json:"offset"`
			Limit  int              `json:"limit"`
			Total  int              `json:"total"`
			Items  []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if !envelope.OK || envelope.Operation != "queue.list" || envelope.Data.Offset != 1 || envelope.Data.Limit != 1 || envelope.Data.Total != 3 ||
		len(envelope.Data.Items) != 1 || envelope.Data.Items[0]["item_id"] != float64(8) || envelope.Data.Items[0]["queue_id"] != "default" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestQueueListPreservesNewestFirstItemIDOrder(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/queue/default/item_ids":
			_ = json.NewEncoder(w).Encode(map[string]any{"item_ids": []int{9, 8}, "total_count": 2})
		case "/api/v1/queue/default/item_summaries_by_ids":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"item_id": 8, "status": "pending", "batch_id": "batch-8", "created_at": "older"},
				{"item_id": 9, "status": "completed", "batch_id": "batch-9", "created_at": "newer"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "list", "--limit", "2", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope struct {
		Data struct {
			Items []struct {
				ItemID int `json:"item_id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if len(envelope.Data.Items) != 2 || envelope.Data.Items[0].ItemID != 9 || envelope.Data.Items[1].ItemID != 8 {
		t.Fatalf("queue items are not newest first: %#v", envelope.Data.Items)
	}
}

func TestQueueListKeepsFinalPageWithinAvailableItemIDs(t *testing.T) {
	isolateUserConfigDir(t)
	var summaries atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/queue/default/item_ids":
			_ = json.NewEncoder(w).Encode(map[string]any{"item_ids": []int{9, 8, 7}, "total_count": 3})
		case "/api/v1/queue/default/item_summaries_by_ids":
			summaries.Add(1)
			var body struct {
				ItemIDs []int `json:"item_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode summary request: %v", err)
				return
			}
			if !reflect.DeepEqual(body.ItemIDs, []int{7}) {
				t.Errorf("hydrated item ids = %v, want [7]", body.ItemIDs)
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"item_id": 7, "status": "completed", "batch_id": "batch-7", "created_at": "oldest"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "list", "--offset", "2", "--limit", "5", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 || summaries.Load() != 1 {
		t.Fatalf("exit code = %d, summary requests = %d, stderr = %q, stdout = %q", exitCode, summaries.Load(), stderr.String(), stdout.String())
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Offset int              `json:"offset"`
			Limit  int              `json:"limit"`
			Total  int              `json:"total"`
			Items  []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if !envelope.OK || envelope.Data.Offset != 2 || envelope.Data.Limit != 5 || envelope.Data.Total != 3 ||
		len(envelope.Data.Items) != 1 || envelope.Data.Items[0]["item_id"] != float64(7) {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestQueueListSkipsHydrationWhenOffsetIsPastAvailableItemIDs(t *testing.T) {
	for _, test := range []struct {
		name   string
		offset int
	}{
		{name: "offset at available item count", offset: 3},
		{name: "offset beyond available item count", offset: 10},
	} {
		t.Run(test.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			var summaries atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/queue/default/item_ids":
					_ = json.NewEncoder(w).Encode(map[string]any{"item_ids": []int{9, 8, 7}, "total_count": 3})
				case "/api/v1/queue/default/item_summaries_by_ids":
					summaries.Add(1)
					http.Error(w, "summaries are outside the requested page", http.StatusInternalServerError)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app := cli.New(&stdout, &stderr)

			exitCode := app.Run(t.Context(), []string{"queue", "list", "--offset", strconv.Itoa(test.offset), "--limit", "2", "--url", server.URL, "--json"})

			if exitCode != result.ExitSuccess || stderr.Len() != 0 || summaries.Load() != 0 {
				t.Fatalf("exit code = %d, summary requests = %d, stderr = %q, stdout = %q", exitCode, summaries.Load(), stderr.String(), stdout.String())
			}
			var envelope struct {
				OK   bool `json:"ok"`
				Data struct {
					Offset int              `json:"offset"`
					Limit  int              `json:"limit"`
					Total  int              `json:"total"`
					Items  []map[string]any `json:"items"`
				} `json:"data"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
			}
			if !envelope.OK || envelope.Data.Offset != test.offset || envelope.Data.Limit != 2 || envelope.Data.Total != 3 || len(envelope.Data.Items) != 0 {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
		})
	}
}

func TestQueueListAcceptsTypedRequestDocument(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/queue/custom/item_ids" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"item_ids": []int{}, "total_count": 0})
	}))
	defer server.Close()
	request := `{"schema_version":1,"queue_id":"custom","offset":3,"limit":4}`
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(request), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "list", "--request", "-", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
}

func TestQueueListRejectsOutOfRangeLimitAsInvalidRequest(t *testing.T) {
	isolateUserConfigDir(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "list", "--limit", "0", "--url", "http://127.0.0.1:1", "--json"})

	if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestQueueGetJSONReturnsNormalizedItemAndOutputImages(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/queue/default/i/8":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"item_id": 8, "queue_id": "default", "batch_id": "batch-8", "session_id": "session-8", "status": "completed", "priority": 0,
				"origin": "generate", "destination": "generate", "created_at": "created", "updated_at": "updated", "started_at": "started", "completed_at": "completed",
				"error_type": nil, "error_message": nil, "error_traceback": "/server/private/traceback",
				"session": map[string]any{"results": map[string]any{
					"node-1": map[string]any{"type": "image_output", "image": map[string]any{"image_name": "output.png"}, "width": 512, "height": 512},
					"node-2": map[string]any{"type": "integer_output", "value": 42},
				}},
			})
		case "/api/v1/images/i/output.png":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"image_name": "output.png", "image_url": "api/v1/images/i/output.png/full", "thumbnail_url": "api/v1/images/i/output.png/thumbnail",
				"image_origin": "internal", "image_category": "general", "width": 512, "height": 512,
				"created_at": "created", "updated_at": "updated", "is_intermediate": false, "starred": false, "has_workflow": true,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "get", "8", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope struct {
		OK        bool   `json:"ok"`
		Operation string `json:"operation"`
		Data      struct {
			Item map[string]any `json:"item"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	images, ok := envelope.Data.Item["images"].([]any)
	if !envelope.OK || envelope.Operation != "queue.get" || envelope.Data.Item["item_id"] != float64(8) || !ok || len(images) != 1 {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
	if strings.Contains(stdout.String(), "traceback") || strings.Contains(stdout.String(), "/server/private") {
		t.Fatalf("server traceback leaked: %s", stdout.String())
	}
}

func TestQueueGetSucceedsWhenHistoricalOutputImageIsMissing(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/queue/default/i/8":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"item_id": 8, "queue_id": "default", "batch_id": "batch-8", "session_id": "session-8", "status": "completed", "priority": 0,
				"created_at": "created", "updated_at": "updated",
				"session": map[string]any{"results": map[string]any{
					"node-1": map[string]any{"type": "image_output", "image": map[string]any{"image_name": "deleted.png"}},
				}},
			})
		case "/api/v1/images/i/deleted.png":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "get", "8", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope struct {
		Data struct {
			Item struct {
				Images []any `json:"images"`
			} `json:"item"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if len(envelope.Data.Item.Images) != 0 {
		t.Fatalf("missing historical output must be omitted, got %#v", envelope.Data.Item.Images)
	}
}

func TestQueueGetCollectsUniqueOutputImageNamesInLexicographicOrder(t *testing.T) {
	isolateUserConfigDir(t)
	var requests struct {
		sync.Mutex
		names []string
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/queue/default/i/8":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"item_id": 8, "queue_id": "default", "batch_id": "batch-8", "session_id": "session-8", "status": "completed", "priority": 0,
				"created_at": "created", "updated_at": "updated",
				"session": map[string]any{"results": map[string]any{
					"node-1": map[string]any{"type": "image_output", "image": map[string]any{"image_name": "zulu.png"}},
					"node-2": map[string]any{"type": "image_output", "image": map[string]any{"image_name": "alpha.png"}},
					"node-3": map[string]any{"type": "image_output", "image": map[string]any{"image_name": "alpha.png"}},
					"node-4": map[string]any{"type": "integer_output", "value": 42},
				}},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/images/i/"):
			name := strings.TrimPrefix(r.URL.Path, "/api/v1/images/i/")
			requests.Lock()
			requests.names = append(requests.names, name)
			requests.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"image_name": name, "image_url": "api/v1/images/i/" + name + "/full", "thumbnail_url": "api/v1/images/i/" + name + "/thumbnail",
				"image_origin": "internal", "image_category": "general", "width": 512, "height": 512,
				"created_at": "created", "updated_at": "updated", "is_intermediate": false, "starred": false, "has_workflow": true,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "get", "8", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	requests.Lock()
	requested := requests.names
	requests.Unlock()
	if !reflect.DeepEqual(requested, []string{"alpha.png", "zulu.png"}) {
		t.Fatalf("output image requests = %v, want [alpha.png zulu.png]", requested)
	}
	var envelope struct {
		Data struct {
			Item struct {
				Images []struct {
					ImageName string `json:"image_name"`
				} `json:"images"`
			} `json:"item"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	var names []string
	for _, image := range envelope.Data.Item.Images {
		names = append(names, image.ImageName)
	}
	if !reflect.DeepEqual(names, []string{"alpha.png", "zulu.png"}) {
		t.Fatalf("output images = %v, want [alpha.png zulu.png]", names)
	}
}

// Output image failures reach the CLI wrapped by the queue hydration context
// message, so classification must match through the error chain.
func TestQueueGetClassifiesWrappedOutputImageFailures(t *testing.T) {
	tests := []struct {
		name         string
		imageHandler func(t *testing.T, w http.ResponseWriter)
		exitCode     int
		errorCode    string
	}{
		{
			name: "authentication failure",
			imageHandler: func(_ *testing.T, w http.ResponseWriter) {
				http.Error(w, "denied", http.StatusUnauthorized)
			},
			exitCode:  result.ExitConnection,
			errorCode: "authentication_failed",
		},
		{
			name: "connection failure",
			imageHandler: func(t *testing.T, w http.ResponseWriter) {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Errorf("hijack output image connection: %v", err)
					return
				}
				_ = conn.Close()
			},
			exitCode:  result.ExitConnection,
			errorCode: "connection_failed",
		},
		{
			name: "invalid response",
			imageHandler: func(_ *testing.T, w http.ResponseWriter) {
				_, _ = w.Write([]byte("not-json"))
			},
			exitCode:  result.ExitInvokeAIFailure,
			errorCode: "invalid_invokeai_response",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/queue/default/i/8":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"item_id": 8, "queue_id": "default", "batch_id": "batch-8", "session_id": "session-8", "status": "completed", "priority": 0,
						"created_at": "created", "updated_at": "updated",
						"session": map[string]any{"results": map[string]any{
							"node-1": map[string]any{"type": "image_output", "image": map[string]any{"image_name": "output.png"}},
						}},
					})
				case "/api/v1/images/i/output.png":
					test.imageHandler(t, w)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app := cli.New(&stdout, &stderr)

			exitCode := app.Run(t.Context(), []string{"queue", "get", "8", "--url", server.URL, "--json"})

			if exitCode != test.exitCode || stderr.Len() != 0 {
				t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
			}
			var envelope result.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
			}
			if envelope.OK || envelope.Operation != "queue.get" || envelope.Error == nil || envelope.Error.Code != test.errorCode {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
		})
	}
}

func TestQueueGetDoesNotExposeInvokeAIErrorBodies(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error_traceback":"/server/private/traceback"}`, http.StatusInternalServerError)
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "get", "8", "--url", server.URL, "--json"})

	if exitCode != result.ExitInvokeAIFailure || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if strings.Contains(stdout.String(), "traceback") || strings.Contains(stdout.String(), "/server/private") {
		t.Fatalf("InvokeAI error body leaked: %s", stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "invokeai_operation_failed" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestQueueGetAcceptsTypedRequestDocument(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/queue/custom/i/5" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"item_id": 5, "queue_id": "custom", "batch_id": "batch-5", "session_id": "session-5", "status": "pending", "priority": 0,
			"created_at": "created", "updated_at": "updated", "session": map[string]any{"results": map[string]any{}},
		})
	}))
	defer server.Close()
	request := `{"schema_version":1,"queue_id":"custom","item_id":5}`
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(request), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "get", "--request", "-", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
}

func TestQueueGetRejectsRequestWithoutPositiveItemID(t *testing.T) {
	isolateUserConfigDir(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(`{"schema_version":1,"queue_id":"default"}`), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"queue", "get", "--request", "-", "--url", "http://127.0.0.1:1", "--json"})

	if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestImagesUploadJSONUploadsOneValidatedLocalFile(t *testing.T) {
	isolateUserConfigDir(t)
	imagePath := filepath.Join(t.TempDir(), "source.png")
	imageContent := writeTestPNG(t, imagePath)
	var uploads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "6.14.1"})
		case "/api/v1/images/upload":
			uploads.Add(1)
			query := r.URL.Query()
			if r.Method != http.MethodPost || query.Get("image_category") != "user" || query.Get("is_intermediate") != "false" {
				t.Errorf("unexpected upload request: %s %s", r.Method, r.URL.String())
			}
			file, header, err := r.FormFile("file")
			if err != nil {
				t.Errorf("read uploaded form file: %v", err)
				return
			}
			defer func() { _ = file.Close() }()
			body, err := io.ReadAll(file)
			if err != nil {
				t.Errorf("read uploaded image: %v", err)
				return
			}
			if header.Filename != "source.png" || !bytes.Equal(body, imageContent) {
				t.Errorf("uploaded file name=%q body=%q", header.Filename, body)
			}
			if header.Header.Get("Content-Type") != "image/png" {
				t.Errorf("uploaded file content type = %q, want image/png", header.Header.Get("Content-Type"))
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"image_name": "uploaded.png", "image_url": "api/v1/images/i/uploaded.png/full", "thumbnail_url": "api/v1/images/i/uploaded.png/thumbnail",
				"image_origin": "external", "image_category": "user", "width": 320, "height": 240,
				"created_at": "created", "updated_at": "updated", "is_intermediate": false, "starred": false, "has_workflow": false,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "upload", imagePath, "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 || uploads.Load() != 1 {
		t.Fatalf("exit code = %d, uploads = %d, stderr = %q, stdout = %q", exitCode, uploads.Load(), stderr.String(), stdout.String())
	}
	var envelope struct {
		OK        bool   `json:"ok"`
		Operation string `json:"operation"`
		Data      struct {
			Image map[string]any `json:"image"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if !envelope.OK || envelope.Operation != "images.upload" || envelope.Data.Image["image_name"] != "uploaded.png" ||
		envelope.Data.Image["image_url"] != server.URL+"/api/v1/images/i/uploaded.png/full" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestImagesUploadAcceptsTypedRequestDocument(t *testing.T) {
	isolateUserConfigDir(t)
	imagePath := filepath.Join(t.TempDir(), "source.png")
	writeTestPNG(t, imagePath)
	request, err := json.Marshal(map[string]any{"schema_version": 1, "path": imagePath})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "6.14.1"})
		case "/api/v1/images/upload":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"image_name": "uploaded.png", "image_url": "api/v1/images/i/uploaded.png/full", "thumbnail_url": "api/v1/images/i/uploaded.png/thumbnail",
				"image_origin": "external", "image_category": "user", "width": 1, "height": 1,
				"created_at": "created", "updated_at": "updated", "is_intermediate": false, "starred": false, "has_workflow": false,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewWithIO(bytes.NewReader(request), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "upload", "--request", "-", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
}

func TestImagesUploadRejectsUnsupportedRequestSchemaVersion(t *testing.T) {
	isolateUserConfigDir(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(`{"schema_version":2,"path":"/tmp/source.png"}`), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "upload", "--request", "-", "--url", "http://127.0.0.1:1", "--json"})

	if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestImagesUploadLostResponseReturnsUnknownOutcomeWithoutRetry(t *testing.T) {
	isolateUserConfigDir(t)
	imagePath := filepath.Join(t.TempDir(), "source.png")
	writeTestPNG(t, imagePath)
	var uploads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "6.14.1"})
		case "/api/v1/images/upload":
			uploads.Add(1)
			_, _ = io.Copy(io.Discard, r.Body)
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack upload connection: %v", err)
				return
			}
			_ = connection.Close()
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "upload", imagePath, "--url", server.URL, "--json"})

	if exitCode != result.ExitInvokeAIFailure || stderr.Len() != 0 || uploads.Load() != 1 {
		t.Fatalf("exit code = %d, uploads = %d, stderr = %q, stdout = %q", exitCode, uploads.Load(), stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.OK || envelope.Operation != "images.upload" || envelope.Error == nil || envelope.Error.Code != "outcome_unknown" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestImagesUploadRejectsRelativePathBeforeConnecting(t *testing.T) {
	isolateUserConfigDir(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "upload", "relative.png", "--url", "http://127.0.0.1:1", "--json"})

	if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.OK || envelope.Operation != "images.upload" || envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestImagesUploadRejectsNonRegularPathBeforeConnecting(t *testing.T) {
	isolateUserConfigDir(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "upload", t.TempDir(), "--url", "http://127.0.0.1:1", "--json"})

	if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

// /proc/self/mem names a regular file whose reads fail with EIO at offset zero,
// so the only reachable invalid request is the non-EOF upload content read
// failure; a swallowed read error would instead reach the connection attempt.
func TestImagesUploadRejectsUnreadableFileContentBeforeConnecting(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the Linux proc filesystem provides a regular file whose reads fail")
	}
	isolateUserConfigDir(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "upload", "/proc/self/mem", "--url", "http://127.0.0.1:1", "--json"})

	if exitCode != result.ExitInvalidRequest || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.OK || envelope.Operation != "images.upload" || envelope.Error == nil || envelope.Error.Code != "invalid_request" ||
		!strings.HasPrefix(envelope.Error.Message, "read upload file:") {
		t.Fatalf("unexpected envelope: %#v; error = %#v", envelope, envelope.Error)
	}
}

func TestImagesUploadRejectsUnsupportedInvokeAIVersionBeforeMutation(t *testing.T) {
	isolateUserConfigDir(t)
	imagePath := filepath.Join(t.TempDir(), "source.png")
	writeTestPNG(t, imagePath)
	var uploads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/app/version" {
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "6.15.0"})
			return
		}
		if r.URL.Path == "/api/v1/images/upload" {
			uploads.Add(1)
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "upload", imagePath, "--url", server.URL, "--json"})

	if exitCode != result.ExitUnsupportedCapability || uploads.Load() != 0 || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, uploads = %d, stderr = %q, stdout = %q", exitCode, uploads.Load(), stderr.String(), stdout.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "unsupported_capability" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestConfigSetJSONNeverPrintsToken(t *testing.T) {
	isolateUserConfigDir(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"config", "set", "--token", "top-secret", "--json"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	if strings.Contains(stdout.String(), "top-secret") {
		t.Fatalf("token leaked to stdout: %s", stdout.String())
	}
	var setEnvelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &setEnvelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v", err)
	}
	if !setEnvelope.OK || setEnvelope.Operation != "config.set" {
		t.Fatalf("unexpected set envelope: %#v", setEnvelope)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = app.Run(t.Context(), []string{"config", "get", "--json"})
	if exitCode != result.ExitSuccess {
		t.Fatalf("get exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	var getEnvelope struct {
		OK        bool   `json:"ok"`
		Operation string `json:"operation"`
		Data      struct {
			Stored struct {
				TokenConfigured bool `json:"token_configured"`
			} `json:"stored"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &getEnvelope); err != nil {
		t.Fatalf("config get stdout is not one JSON object: %v", err)
	}
	if !getEnvelope.OK || getEnvelope.Operation != "config.get" || !getEnvelope.Data.Stored.TokenConfigured {
		t.Fatalf("unexpected get envelope: %#v", getEnvelope)
	}
}

func TestConfigGetFailsWhenHumanOutputCannotBeWritten(t *testing.T) {
	isolateUserConfigDir(t)
	var stderr bytes.Buffer
	app := cli.New(errorWriter{}, &stderr)

	exitCode := app.Run(t.Context(), []string{"config", "get"})

	if exitCode != result.ExitInvokeAIFailure {
		t.Fatalf("exit code = %d, want %d", exitCode, result.ExitInvokeAIFailure)
	}
	if !strings.Contains(stderr.String(), "output_write_failed") {
		t.Fatalf("stderr = %q, want output_write_failed", stderr.String())
	}
}

func TestConfigGetHumanOutputShowsUnsetStoredValues(t *testing.T) {
	isolateUserConfigDir(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"config", "get"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	want := "Configuration: " + filepath.Join(userConfigDir, "bediz", "config.json") + "\n" +
		"Stored URL: not set\n" +
		"Stored token: not configured\n" +
		"Effective URL: http://127.0.0.1:9090 (default)\n" +
		"Effective token: not configured\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func isolateUserConfigDir(t *testing.T) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "config")
	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", dir)
	case "darwin":
		t.Setenv("HOME", dir)
	default:
		t.Setenv("XDG_CONFIG_HOME", dir)
	}
}

func TestInvalidCommandUsesJSONEnvelope(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"unknown", "--json"})

	if exitCode != result.ExitInvalidRequest {
		t.Fatalf("exit code = %d, want %d", exitCode, result.ExitInvalidRequest)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v", err)
	}
	if envelope.OK || envelope.Operation != "cli" || envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestInvalidCommandWithJSONDisabledUsesHumanDiagnostic(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"unknown", "--json=false"})

	if exitCode != result.ExitInvalidRequest {
		t.Fatalf("exit code = %d, want %d", exitCode, result.ExitInvalidRequest)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "invalid_request:") || !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("stderr = %q, want human invalid-command diagnostic", stderr.String())
	}
}

func TestInvalidJSONValueUsesHumanDiagnostic(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"version", "--json=invalid"})

	if exitCode != result.ExitInvalidRequest {
		t.Fatalf("exit code = %d, want %d", exitCode, result.ExitInvalidRequest)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "invalid_request:") || !strings.Contains(stderr.String(), "invalid syntax") {
		t.Fatalf("stderr = %q, want human invalid-Boolean diagnostic", stderr.String())
	}
}

func TestRootHelpListsVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"--help"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "  version     Print the Bediz version\n") {
		t.Fatalf("root help does not list version: %q", stdout.String())
	}
}

func TestVersionReturnsStableHumanContract(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"version"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if want := version.Current().Version + "\n"; stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestVersionReportsHumanOutputWriteFailure(t *testing.T) {
	var stderr bytes.Buffer
	app := cli.New(errorWriter{}, &stderr)

	exitCode := app.Run(t.Context(), []string{"version"})

	if exitCode != result.ExitInvokeAIFailure {
		t.Fatalf("exit code = %d, want %d", exitCode, result.ExitInvokeAIFailure)
	}
	if !strings.Contains(stderr.String(), "output_write_failed") {
		t.Fatalf("stderr = %q, want output_write_failed", stderr.String())
	}
}

func TestGlobalJSONFlagBeforeVersionReturnsStableContract(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"--json", "version"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	var envelope struct {
		SchemaVersion int          `json:"schema_version"`
		OK            bool         `json:"ok"`
		Operation     string       `json:"operation"`
		Data          version.Info `json:"data"`
		Warnings      []any        `json:"warnings"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.SchemaVersion != 1 || !envelope.OK || envelope.Operation != "version" ||
		envelope.Data != version.Current() || len(envelope.Warnings) != 0 {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestDoctorJSONAdvertisesOnlyImplementedCapabilities(t *testing.T) {
	isolateUserConfigDir(t)
	paths := map[string]any{
		"/api/v1/app/version": map[string]any{"get": map[string]any{}},
		"/api/v2/models/":     map[string]any{"get": map[string]any{}},
		"/api/v2/models/install": map[string]any{"post": map[string]any{
			"parameters": []any{map[string]any{"name": "source", "in": "query", "required": true}},
			"responses":  map[string]any{"201": map[string]any{"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/ModelInstallJob"}}}}},
		}},
		"/api/v2/models/install/{id}":                    map[string]any{"get": map[string]any{}},
		"/api/v2/models/starter_models":                  map[string]any{"get": map[string]any{"responses": map[string]any{"200": map[string]any{"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/StarterModelResponse"}}}}}}},
		"/api/v1/images/":                                map[string]any{"get": map[string]any{}},
		"/api/v1/images/i/{image_name}":                  map[string]any{"get": map[string]any{}},
		"/api/v1/images/upload":                          map[string]any{"post": map[string]any{}},
		"/api/v1/queue/{queue_id}/item_ids":              map[string]any{"get": map[string]any{}},
		"/api/v1/queue/{queue_id}/item_summaries_by_ids": map[string]any{"post": map[string]any{}},
		"/api/v1/queue/{queue_id}/i/{item_id}":           map[string]any{"get": map[string]any{}},
		"/api/v1/queue/{queue_id}/enqueue_batch":         map[string]any{"post": map[string]any{}},
	}
	openAPIDocument := sdxlOpenAPIFixture(t)
	paths["/api/v1/recall/{queue_id}"] = openAPIDocument["paths"].(map[string]any)["/api/v1/recall/{queue_id}"]
	paths["/api/v2/models/hf_login"] = map[string]any{"get": map[string]any{}, "post": map[string]any{"requestBody": map[string]any{"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/Body_do_hf_login"}}}}}, "delete": map[string]any{}}
	openAPIDocument["components"].(map[string]any)["schemas"].(map[string]any)["Body_do_hf_login"] = map[string]any{"properties": map[string]any{"token": map[string]any{"type": "string"}}, "required": []any{"token"}}
	openAPIDocument["paths"] = paths
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "6.14.1"})
		case "/openapi.json":
			_ = json.NewEncoder(w).Encode(openAPIDocument)
		case "/api/v2/models/":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{
				{"key": "main", "hash": "blake3:main", "name": "Anima", "base": "anima", "type": "main"},
				{"key": "vae", "hash": "blake3:vae", "name": "VAE", "base": "anima", "type": "vae"},
				{"key": "encoder", "hash": "blake3:encoder", "name": "Qwen3", "base": "any", "type": "qwen3_encoder"},
				{"key": "sdxl", "hash": "blake3:sdxl", "name": "SDXL", "base": "sdxl", "type": "main"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"doctor", "--url", server.URL, "--json"})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope struct {
		SchemaVersion int    `json:"schema_version"`
		OK            bool   `json:"ok"`
		Operation     string `json:"operation"`
		Data          struct {
			Ready        bool `json:"ready"`
			Capabilities []struct {
				Operation  string `json:"operation"`
				Compatible bool   `json:"compatible"`
			} `json:"capabilities"`
			UISync  map[string]string `json:"ui_sync"`
			OpenAPI struct {
				Invocations []any `json:"required_invocations"`
			} `json:"openapi"`
			Models struct {
				Relevant     []any `json:"relevant"`
				Requirements []any `json:"requirements"`
			} `json:"models"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	operations := make([]string, len(envelope.Data.Capabilities))
	for i, entry := range envelope.Data.Capabilities {
		if !entry.Compatible {
			t.Fatalf("implemented capability is not compatible: %#v", entry)
		}
		operations[i] = entry.Operation
	}
	wantOperations := []string{"models.list", "models.install", "models.install", "models.status", "images.list", "images.get", "images.upload", "queue.list", "queue.get", "generate", "generate", "recall", "auth.huggingface.status", "auth.huggingface.login", "auth.huggingface.logout"}
	if envelope.SchemaVersion != 1 || !envelope.OK || envelope.Operation != "doctor" || !envelope.Data.Ready ||
		!slices.Equal(operations, wantOperations) || envelope.Data.UISync["generate"] != "partial" ||
		len(envelope.Data.OpenAPI.Invocations) != 14 || len(envelope.Data.Models.Relevant) != 4 || len(envelope.Data.Models.Requirements) != 4 {
		t.Fatalf("unexpected doctor envelope: %#v", envelope)
	}
}

func TestDoctorReportsHumanOutputWriteFailure(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	var stderr bytes.Buffer
	app := cli.New(errorWriter{}, &stderr)

	exitCode := app.Run(t.Context(), []string{"doctor", "--url", server.URL})

	if exitCode != result.ExitInvokeAIFailure {
		t.Fatalf("exit code = %d, want %d", exitCode, result.ExitInvokeAIFailure)
	}
	if !strings.Contains(stderr.String(), "output_write_failed") {
		t.Fatalf("stderr = %q, want output_write_failed", stderr.String())
	}
}

func TestConfigHelpListsNestedCommands(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"config", "--help"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, command := range []string{"get", "set"} {
		if !bytes.Contains(stdout.Bytes(), []byte(command)) {
			t.Fatalf("help does not list %q command; stdout = %q", command, stdout.String())
		}
	}
}

func TestConfigGetHelpIsGeneratedByCommandTree(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"config", "get", "--help"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("--json")) {
		t.Fatalf("help does not list inherited --json flag; stdout = %q", stdout.String())
	}
}

func TestConfigSetHelpListsConfigurationFlags(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"config", "set", "--help"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, flag := range []string{"--url", "--token", "--unset-url", "--unset-token"} {
		if !bytes.Contains(stdout.Bytes(), []byte(flag)) {
			t.Fatalf("help does not list %q; stdout = %q", flag, stdout.String())
		}
	}
}

func TestDoctorHelpListsConnectionFlags(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"doctor", "--help"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, flag := range []string{"--url", "--token", "--timeout", "--json"} {
		if !bytes.Contains(stdout.Bytes(), []byte(flag)) {
			t.Fatalf("help does not list %q; stdout = %q", flag, stdout.String())
		}
	}
}

func TestDoctorHelpInJSONModeUsesFailureEnvelope(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "JSON flag after help flag", args: []string{"doctor", "--help", "--json"}},
		{name: "JSON flag before command", args: []string{"--json", "doctor", "--help"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app := cli.New(&stdout, &stderr)

			exitCode := app.Run(t.Context(), test.args)

			if exitCode != result.ExitInvalidRequest {
				t.Fatalf("exit code = %d, want %d", exitCode, result.ExitInvalidRequest)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
			var envelope result.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
			}
			if envelope.OK || envelope.Operation != "doctor" || envelope.Error == nil || envelope.Error.Code != "invalid_request" {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
		})
	}
}

func TestDoctorFlagErrorKeepsOperationInJSONEnvelope(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"doctor", "--unknown", "--json"})

	if exitCode != result.ExitInvalidRequest {
		t.Fatalf("exit code = %d, want %d", exitCode, result.ExitInvalidRequest)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.OK || envelope.Operation != "doctor" || envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

// Every implemented remote command reports its authoritative operation name,
// structured error code, and exit status. A rejected connection is one class
// they must all classify identically, including doctor, which shares
// connection resolution but keeps its own diagnostic path.
func TestImplementedRemoteCommandsReportStableOperationsAndFailures(t *testing.T) {
	isolateUserConfigDir(t)
	imagePath := filepath.Join(t.TempDir(), "source.png")
	writeTestPNG(t, imagePath)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "denied", http.StatusUnauthorized)
	}))
	defer server.Close()

	tests := []struct {
		name      string
		args      []string
		operation string
	}{
		{name: "doctor", args: []string{"doctor"}, operation: result.OperationDoctor},
		{name: "models list", args: []string{"models", "list"}, operation: result.OperationModelsList},
		{name: "images list", args: []string{"images", "list"}, operation: result.OperationImagesList},
		{name: "images get", args: []string{"images", "get", "image-1.png"}, operation: result.OperationImagesGet},
		{name: "images upload", args: []string{"images", "upload", imagePath}, operation: result.OperationImagesUpload},
		{name: "queue list", args: []string{"queue", "list"}, operation: result.OperationQueueList},
		{name: "queue get", args: []string{"queue", "get", "5"}, operation: result.OperationQueueGet},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app := cli.New(&stdout, &stderr)
			args := slices.Concat(test.args, []string{"--url", server.URL, "--json"})

			exitCode := app.Run(t.Context(), args)

			if exitCode != result.ExitConnection {
				t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
			var envelope result.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
			}
			if envelope.OK || envelope.Operation != test.operation || envelope.Error == nil || envelope.Error.Code != result.CodeAuthenticationFailed {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
		})
	}
}
