package cli_test

import (
	"bytes"
	json "encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

func sdxlOpenAPIFixture(t *testing.T) map[string]any {
	t.Helper()
	encoded, err := os.ReadFile("../doctor/testdata/invokeai_6_14_anima_openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func sdxlInventory() []map[string]any {
	return []map[string]any{
		{"key": "sdxl-main", "hash": "main-hash", "name": "SDXL Main", "base": "sdxl", "type": "main", "format": "diffusers"},
		{"key": "sdxl-vae", "hash": "vae-hash", "name": "SDXL VAE", "base": "sdxl", "type": "vae", "format": "diffusers"},
	}
}

func TestGenerateSDXLDefaultsWithBundledVAEAndRecallCFG(t *testing.T) {
	isolateUserConfigDir(t)
	var enqueues, recalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), sdxlInventory()) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/queue/default/enqueue_batch":
			enqueues.Add(1)
			var payload struct {
				Batch struct {
					Graph struct {
						Nodes map[string]map[string]any `json:"nodes"`
					} `json:"graph"`
					Data [][]struct {
						Items []uint32 `json:"items"`
					} `json:"data"`
				} `json:"batch"`
			}
			if err := json.UnmarshalRead(r.Body, &payload); err != nil {
				t.Error(err)
			}
			if _, exists := payload.Batch.Graph.Nodes["vae_loader"]; exists {
				t.Error("bundled VAE must not add vae_loader")
			}
			if payload.Batch.Graph.Nodes["noise"]["width"] != float64(1024) || payload.Batch.Graph.Nodes["denoise"]["cfg_scale"] != float64(7) {
				t.Errorf("unresolved SDXL graph: %#v", payload.Batch.Graph.Nodes)
			}
			_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "enqueued": 1, "requested": 1, "item_ids": []int{17}, "batch": map[string]any{"batch_id": "batch-sdxl"}})
		case "/api/v1/recall/default":
			recalls.Add(1)
			var patch map[string]any
			if err := json.UnmarshalRead(r.Body, &patch); err != nil {
				t.Error(err)
			}
			if patch["cfg_scale"] != float64(7) || patch["model"] != "SDXL Main" || patch["seed"] != float64(42) {
				t.Errorf("SDXL Recall patch = %#v", patch)
			}
			_, _ = w.Write([]byte(`{"status":"success"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := cli.New(&stdout, &stderr).Run(t.Context(), []string{"generate", "--no-wait", "--model", "sdxl-main", "--prompt", "a lighthouse", "--seed", "42", "--url", server.URL, "--json"})
	if code != 0 || stderr.Len() != 0 || enqueues.Load() != 1 || recalls.Load() != 1 {
		t.Fatalf("code=%d enqueues=%d recalls=%d stderr=%s stdout=%s", code, enqueues.Load(), recalls.Load(), stderr.String(), stdout.String())
	}
	var envelope receiptEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	s := envelope.Data.ResolvedSettings
	if !envelope.OK || s.Width != 1024 || s.Height != 1024 || s.Steps != 30 || s.Scheduler != "dpmpp_3m_k" || s.Guidance != 7 || s.OutputCount != 1 || s.NegativePrompt != "" || len(s.ComponentKeys) != 0 || !slices.Equal(s.Seeds, []uint32{42}) || len(envelope.Data.Outputs) != 0 {
		t.Fatalf("SDXL receipt = %#v", envelope)
	}
	assertSDXLWarning(t, envelope.Data.Warnings, envelope.Warnings)
}

func assertSDXLWarning(t *testing.T, receipt, envelope []result.Warning) {
	t.Helper()
	if len(receipt) != 1 || !reflect.DeepEqual(receipt, envelope) || receipt[0].Code != "ui_sync_partial" {
		t.Fatalf("warnings = %#v / %#v", receipt, envelope)
	}
	fields, ok := receipt[0].Details["not_restored"].([]any)
	if !ok {
		t.Fatalf("warning fields = %#v", receipt[0].Details)
	}
	got := make([]string, len(fields))
	for i, field := range fields {
		got[i], ok = field.(string)
		if !ok {
			t.Fatalf("field = %#v", field)
		}
	}
	if !slices.Equal(got, []string{"scheduler", "vae", "output_count", "board_id"}) {
		t.Fatalf("not_restored = %q", got)
	}
}

func TestGenerateSDXLVAEBatchAndBoard(t *testing.T) {
	isolateUserConfigDir(t)
	var enqueues atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), sdxlInventory()) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/queue/default/enqueue_batch":
			enqueues.Add(1)
			var payload struct {
				Batch struct {
					Graph struct {
						Nodes map[string]map[string]any `json:"nodes"`
					} `json:"graph"`
					Data [][]struct {
						Items []uint32 `json:"items"`
					} `json:"data"`
				} `json:"batch"`
			}
			if err := json.UnmarshalRead(r.Body, &payload); err != nil {
				t.Error(err)
			}
			if payload.Batch.Graph.Nodes["vae_loader"]["vae_model"].(map[string]any)["key"] != "sdxl-vae" || payload.Batch.Graph.Nodes["decode"]["board"].(map[string]any)["board_id"] != "board-1" || !slices.Equal(payload.Batch.Data[0][0].Items, []uint32{43, 42}) {
				t.Errorf("SDXL override batch = %#v", payload.Batch)
			}
			_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "enqueued": 2, "requested": 2, "item_ids": []int{23, 22}, "batch": map[string]any{"batch_id": "batch-sdxl"}})
		case "/api/v1/recall/default":
			_, _ = w.Write([]byte(`{"status":"success"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := cli.New(&stdout, &stderr).Run(t.Context(), []string{"generate", "--no-wait", "--model", "sdxl-main", "--prompt", "a lighthouse", "--negative-prompt", "text", "--width", "768", "--height", "512", "--steps", "24", "--scheduler", "heun", "--guidance", "6.5", "--seed", "42", "--output-count", "2", "--board", "board-1", "--vae", "sdxl-vae", "--url", server.URL, "--json"})
	if code != 0 || enqueues.Load() != 1 || stderr.Len() != 0 {
		t.Fatalf("code=%d enqueues=%d stderr=%s stdout=%s", code, enqueues.Load(), stderr.String(), stdout.String())
	}
	var envelope receiptEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	s := envelope.Data.ResolvedSettings
	if !reflect.DeepEqual(s.ComponentKeys, map[string]string{"vae": "sdxl-vae"}) || !slices.Equal(s.Seeds, []uint32{42, 43}) || !slices.Equal(envelope.Data.Queue.ItemIDs, []int{23, 22}) || s.BoardID != "board-1" || s.Guidance != 6.5 {
		t.Fatalf("receipt = %#v", envelope.Data)
	}
	assertSDXLWarning(t, envelope.Data.Warnings, envelope.Warnings)
}

func TestGenerateSDXLRejectsInapplicableAndAmbiguousInputsBeforeEnqueue(t *testing.T) {
	cases := []struct {
		name, document, code string
		addModels            []map[string]any
	}{
		{"qwen3 encoder", `{"schema_version":1,"model":"sdxl-main","positive_prompt":"test","components":{"qwen3_encoder":"encoder"}}`, "invalid_request", nil},
		{"empty qwen3 encoder", `{"schema_version":1,"model":"sdxl-main","positive_prompt":"test","components":{"qwen3_encoder":""}}`, "invalid_request", nil},
		{"t5 encoder", `{"schema_version":1,"model":"sdxl-main","positive_prompt":"test","components":{"t5_encoder":"encoder"}}`, "invalid_request", nil},
		{"clip embed", `{"schema_version":1,"model":"sdxl-main","positive_prompt":"test","components":{"clip_embed":"encoder"}}`, "invalid_request", nil},
		{"single dimension", `{"schema_version":1,"model":"sdxl-main","positive_prompt":"test","width":768}`, "invalid_request", nil},
		{"low guidance", `{"schema_version":1,"model":"sdxl-main","positive_prompt":"test","guidance":0.5}`, "invalid_request", nil},
		{"empty VAE override", `{"schema_version":1,"model":"sdxl-main","positive_prompt":"test","components":{"vae":""}}`, "invalid_request", nil},
		{"ambiguous VAE", `{"schema_version":1,"model":"sdxl-main","positive_prompt":"test","components":{"vae":"Same VAE"}}`, "selection_required", []map[string]any{{"key": "vae-a", "hash": "hash-a", "name": "Same VAE", "base": "sdxl", "type": "vae"}, {"key": "vae-b", "hash": "hash-b", "name": "Same VAE", "base": "sdxl", "type": "vae"}}},
		{"incompatible VAE", `{"schema_version":1,"model":"sdxl-main","positive_prompt":"test","components":{"vae":"wrong-vae"}}`, "unsupported_capability", []map[string]any{{"key": "wrong-vae", "hash": "wrong-hash", "name": "Wrong VAE", "base": "anima", "type": "vae"}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			var enqueues, recalls atomic.Int32
			inventory := append(sdxlInventory(), test.addModels...)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), inventory) {
					return
				}
				if r.URL.Path == "/api/v1/queue/default/enqueue_batch" {
					enqueues.Add(1)
				}
				if r.URL.Path == "/api/v1/recall/default" {
					recalls.Add(1)
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			code := cli.NewWithIO(strings.NewReader(test.document), &stdout, &stderr).Run(t.Context(), []string{"generate", "--no-wait", "--request", "-", "--url", server.URL, "--json"})
			var envelope result.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if code != result.ExitStatus(test.code) || envelope.Error == nil || envelope.Error.Code != test.code || enqueues.Load() != 0 || recalls.Load() != 0 || stderr.Len() != 0 {
				t.Fatalf("code=%d envelope=%#v enqueues=%d recalls=%d stderr=%q", code, envelope, enqueues.Load(), recalls.Load(), stderr.String())
			}
		})
	}
}

func TestGenerateSDXLRejectsEmptyVAEFlagBeforeEnqueue(t *testing.T) {
	isolateUserConfigDir(t)
	var enqueues, recalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), sdxlInventory()) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/queue/default/enqueue_batch":
			enqueues.Add(1)
		case "/api/v1/recall/default":
			recalls.Add(1)
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := cli.New(&stdout, &stderr).Run(t.Context(), []string{"generate", "--model", "sdxl-main", "--prompt", "test", "--vae", "", "--url", server.URL, "--json"})
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if code != result.ExitStatus("invalid_request") || envelope.Error == nil || envelope.Error.Code != "invalid_request" || enqueues.Load() != 0 || recalls.Load() != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d envelope=%#v enqueues=%d recalls=%d stderr=%q", code, envelope, enqueues.Load(), recalls.Load(), stderr.String())
	}
}

func TestRecallAcceptsSDXLAndRejectsBelowMinimum(t *testing.T) {
	for _, test := range []struct {
		size      int
		wantCode  string
		wantPosts int
	}{{64, "", 1}, {56, "invalid_request", 0}} {
		t.Run(string(rune(test.size)), func(t *testing.T) {
			isolateUserConfigDir(t)
			var posts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), sdxlInventory()) {
					return
				}
				if r.URL.Path == "/api/v1/recall/default" {
					posts.Add(1)
					_, _ = w.Write([]byte(`{"status":"success"}`))
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			code := cli.New(&stdout, &stderr).Run(t.Context(), []string{"recall", "--model", "sdxl-main", "--width", fmt.Sprint(test.size), "--height", fmt.Sprint(test.size), "--url", server.URL, "--json"})
			var envelope result.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if posts.Load() != int32(test.wantPosts) || stderr.Len() != 0 || (test.wantCode == "" && (code != 0 || !envelope.OK)) || (test.wantCode != "" && (code != result.ExitStatus(test.wantCode) || envelope.Error == nil || envelope.Error.Code != test.wantCode)) {
				t.Fatalf("code=%d posts=%d envelope=%#v", code, posts.Load(), envelope)
			}
		})
	}
}

func TestGenerateSDXLWaitedBatchKeepsQueueSeedAssociations(t *testing.T) {
	isolateUserConfigDir(t)
	var enqueues atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), sdxlInventory()) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/queue/default/enqueue_batch":
			enqueues.Add(1)
			_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "enqueued": 2, "requested": 2, "item_ids": []int{23, 22}, "batch": map[string]any{"batch_id": "batch-sdxl"}})
		case "/api/v1/recall/default":
			_, _ = w.Write([]byte(`{"status":"success"}`))
		case "/api/v1/queue/default/i/23":
			_ = json.MarshalWrite(w, completedBatchItem(23, "batch-sdxl", 42, "first.png"))
		case "/api/v1/queue/default/i/22":
			_ = json.MarshalWrite(w, completedBatchItem(22, "batch-sdxl", 43, "second.png"))
		case "/api/v1/images/i/first.png", "/api/v1/images/i/second.png":
			name := strings.TrimPrefix(r.URL.Path, "/api/v1/images/i/")
			_ = json.MarshalWrite(w, map[string]any{"image_name": name, "image_url": r.URL.Path + "/full", "thumbnail_url": r.URL.Path + "/thumbnail", "image_origin": "internal", "image_category": "general", "width": 1024, "height": 1024, "created_at": "2026-01-01", "updated_at": "2026-01-01", "is_intermediate": false, "starred": false, "has_workflow": false})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := cli.New(&stdout, &stderr).Run(t.Context(), []string{"generate", "--model", "sdxl-main", "--prompt", "test", "--seed", "42", "--output-count", "2", "--url", server.URL, "--json"})
	if code != 0 || enqueues.Load() != 1 || stderr.Len() != 0 {
		t.Fatalf("code=%d enqueues=%d stderr=%q stdout=%s", code, enqueues.Load(), stderr.String(), stdout.String())
	}
	var envelope receiptEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(envelope.Data.ResolvedSettings.Seeds, []uint32{42, 43}) || !slices.Equal(envelope.Data.Queue.ItemIDs, []int{23, 22}) || len(envelope.Data.Outputs) != 2 {
		t.Fatalf("receipt = %#v", envelope.Data)
	}
	for i, want := range []struct {
		item  int
		seed  uint32
		image string
	}{{23, 42, "first.png"}, {22, 43, "second.png"}} {
		got := envelope.Data.Outputs[i]
		if got.ItemID != want.item || got.Seed != want.seed || got.Image.ImageName != want.image {
			t.Fatalf("output %d = %#v", i, got)
		}
	}
}

func TestGenerateSDXLRecallFailureKeepsSuccessfulReceipt(t *testing.T) {
	isolateUserConfigDir(t)
	var enqueues, recalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), sdxlInventory()) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/queue/default/enqueue_batch":
			enqueues.Add(1)
			_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "enqueued": 1, "requested": 1, "item_ids": []int{17}, "batch": map[string]any{"batch_id": "batch-sdxl"}})
		case "/api/v1/recall/default":
			recalls.Add(1)
			http.Error(w, "Recall failed", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := cli.New(&stdout, &stderr).Run(t.Context(), []string{"generate", "--no-wait", "--model", "sdxl-main", "--prompt", "test", "--seed", "1", "--url", server.URL, "--json"})
	var envelope receiptEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if code != 0 || !envelope.OK || enqueues.Load() != 1 || recalls.Load() != 1 || stderr.Len() != 0 || len(envelope.Warnings) != 1 || envelope.Warnings[0].Code != "ui_sync_failed" || !reflect.DeepEqual(envelope.Warnings, envelope.Data.Warnings) {
		t.Fatalf("code=%d envelope=%#v enqueues=%d recalls=%d stderr=%q", code, envelope, enqueues.Load(), recalls.Load(), stderr.String())
	}
}

func TestGenerateSDXLInconclusiveEnqueueIsNotRetried(t *testing.T) {
	isolateUserConfigDir(t)
	var enqueues, recalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), sdxlInventory()) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/queue/default/enqueue_batch":
			enqueues.Add(1)
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = connection.Close()
		case "/api/v1/recall/default":
			recalls.Add(1)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := cli.New(&stdout, &stderr).Run(t.Context(), []string{"generate", "--no-wait", "--model", "sdxl-main", "--prompt", "test", "--seed", "1", "--url", server.URL, "--json"})
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if code != result.ExitInvokeAIFailure || envelope.Error == nil || envelope.Error.Code != "outcome_unknown" || enqueues.Load() != 1 || recalls.Load() != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d envelope=%#v enqueues=%d recalls=%d stderr=%q", code, envelope, enqueues.Load(), recalls.Load(), stderr.String())
	}
}
