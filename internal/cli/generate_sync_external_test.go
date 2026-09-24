package cli_test

import (
	"bytes"
	json "encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

var generationSyncMutations = []string{
	"POST /api/v1/queue/default/enqueue_batch", "POST /api/v1/recall/default",
}

type failFirstWrite struct {
	bytes.Buffer
	writes int
}

func (writer *failFirstWrite) Write(data []byte) (int, error) {
	writer.writes++
	if writer.writes == 1 {
		return 0, errors.New("warning output unavailable")
	}
	return writer.Buffer.Write(data)
}

func assertPartialSyncWarning(t *testing.T, receiptWarnings, envelopeWarnings []result.Warning) {
	t.Helper()
	if len(receiptWarnings) != 1 || !reflect.DeepEqual(receiptWarnings, envelopeWarnings) {
		t.Fatalf("receipt warnings = %#v, envelope warnings = %#v", receiptWarnings, envelopeWarnings)
	}
	warning := envelopeWarnings[0]
	fields, ok := warning.Details["not_restored"].([]any)
	if !ok {
		t.Fatalf("warning details = %#v", warning.Details)
	}
	want := []string{"scheduler", "guidance", "vae", "qwen3_encoder", "output_count", "board_id"}
	got := make([]string, len(fields))
	for index, field := range fields {
		got[index], ok = field.(string)
		if !ok {
			t.Fatalf("non-string field: %#v", field)
		}
	}
	if warning.Code != "ui_sync_partial" || !slices.Equal(got, want) {
		t.Fatalf("warning = %#v", warning)
	}
}

func TestGenerateNoWaitRecallsFirstResolvedSeedAfterAcceptedBatch(t *testing.T) {
	isolateUserConfigDir(t)
	openAPI := animaOpenAPIFixture("", "")
	mutations := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", openAPI, animaModelInventory()) {
			return
		}
		mutations = append(mutations, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/queue/default/enqueue_batch":
			_ = json.MarshalWrite(w, map[string]any{
				"queue_id": "default", "enqueued": 3, "requested": 3, "item_ids": []int{23, 22, 21},
				"batch": map[string]any{"batch_id": "batch-sync"},
			})
		case "/api/v1/recall/default":
			var patch map[string]any
			if err := json.UnmarshalRead(r.Body, &patch); err != nil {
				t.Error(err)
			}
			want := map[string]any{
				"model": "Anima Main", "positive_prompt": "lighthouse", "negative_prompt": "text",
				"width": float64(768), "height": float64(1024), "steps": float64(24), "seed": float64(41),
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
	code := cli.New(&stdout, &stderr).Run(t.Context(), []string{
		"generate", "--no-wait", "--model", "main-key", "--prompt", "lighthouse", "--negative-prompt", "text",
		"--width", "768", "--height", "1024", "--steps", "24", "--seed", "41", "--output-count", "3",
		"--url", server.URL, "--json",
	})
	if code != result.ExitSuccess || stderr.Len() != 0 ||
		!reflect.DeepEqual(mutations, []string{"POST /api/v1/queue/default/enqueue_batch", "POST /api/v1/recall/default"}) {
		t.Fatalf("code=%d stderr=%q mutations=%v stdout=%q", code, stderr.String(), mutations, stdout.String())
	}
	var envelope receiptEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || !reflect.DeepEqual(envelope.Data.ResolvedSettings.Seeds, []uint32{41, 42, 43}) {
		t.Fatalf("envelope=%#v", envelope)
	}
	assertPartialSyncWarning(t, envelope.Data.Warnings, envelope.Warnings)
}

func TestGenerateReportsWarningOutputFailureAsLocalOutputFailure(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", animaOpenAPIFixture("", ""), animaModelInventory()) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/queue/default/enqueue_batch":
			_ = json.MarshalWrite(w, map[string]any{
				"queue_id": "default", "enqueued": 1, "requested": 1, "item_ids": []int{17},
				"batch": map[string]any{"batch_id": "batch-warning"},
			})
		case "/api/v1/recall/default":
			_, _ = w.Write([]byte(`{"status":"success"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout bytes.Buffer
	stderr := &failFirstWrite{}
	code := cli.New(&stdout, stderr).Run(t.Context(), []string{
		"generate", "--no-wait", "--model", "main-key", "--prompt", "test", "--seed", "1", "--url", server.URL,
	})
	if code != result.ExitStatus(result.CodeOutputWriteFailed) ||
		!bytes.Contains(stdout.Bytes(), []byte("Accepted batch batch-warning")) ||
		!bytes.Contains(stderr.Bytes(), []byte(result.CodeOutputWriteFailed)) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestGenerateRecallFailurePreservesSuccessfulReceipt(t *testing.T) {
	for _, test := range []struct {
		name           string
		noWait         bool
		ambiguousModel bool
		lostResponse   bool
		wantRecall     int32
	}{
		{name: "no wait Recall rejected", noWait: true, wantRecall: 1},
		{name: "completed Recall rejected", wantRecall: 1},
		{name: "no wait unsafe model name", noWait: true, ambiguousModel: true},
		{name: "completed unsafe model name", ambiguousModel: true},
		{name: "no wait Recall response lost", noWait: true, lostResponse: true, wantRecall: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			models := animaModelInventory()
			if test.ambiguousModel {
				models = append(models, map[string]any{
					"key": "other-key", "hash": "blake3:other", "name": "Anima Main", "base": "sdxl", "type": "main",
				})
			}
			var enqueues, recalls, polls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveAnimaPreflight(w, r, "6.14.1", animaOpenAPIFixture("", ""), models) {
					return
				}
				switch r.URL.Path {
				case "/api/v1/queue/default/enqueue_batch":
					enqueues.Add(1)
					_ = json.MarshalWrite(w, map[string]any{
						"queue_id": "default", "enqueued": 1, "requested": 1, "item_ids": []int{17},
						"batch": map[string]any{"batch_id": "batch-sync-failure"},
					})
				case "/api/v1/recall/default":
					if enqueues.Load() != 1 {
						t.Error("Recall arrived before conclusive enqueue")
					}
					recalls.Add(1)
					if test.lostResponse {
						connection, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						_ = connection.Close()
						return
					}
					http.Error(w, "Recall failed", http.StatusInternalServerError)
				case "/api/v1/queue/default/i/17":
					polls.Add(1)
					_ = json.MarshalWrite(w, map[string]any{
						"item_id": 17, "queue_id": "default", "batch_id": "batch-sync-failure", "session_id": "session-17",
						"status": "completed", "priority": 0,
						"created_at": "2026-01-01 00:00:00.000", "updated_at": "2026-01-01 00:00:05.000",
						"field_values": []map[string]any{{"node_path": "seed", "field_name": "value", "value": 42}},
						"session":      completedItemResults("generated.png"),
					})
				case "/api/v1/images/i/generated.png":
					_ = json.MarshalWrite(w, map[string]any{
						"image_name": "generated.png", "image_url": "/api/v1/images/i/generated.png/full",
						"thumbnail_url": "/api/v1/images/i/generated.png/thumbnail",
						"image_origin":  "internal", "image_category": "general", "width": 768, "height": 1024,
						"created_at": "2026-01-01 00:00:05.000", "updated_at": "2026-01-01 00:00:05.000",
						"is_intermediate": false, "starred": false, "has_workflow": false,
					})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			args := generateWaitArgs(server.URL, "--json")
			if test.noWait {
				args = append(args, "--no-wait")
			}
			var stdout, stderr bytes.Buffer
			code := cli.New(&stdout, &stderr).Run(t.Context(), args)
			if code != result.ExitSuccess || stderr.Len() != 0 || enqueues.Load() != 1 || recalls.Load() != test.wantRecall {
				t.Fatalf("code=%d stderr=%q enqueues=%d recalls=%d stdout=%q", code, stderr.String(), enqueues.Load(), recalls.Load(), stdout.String())
			}
			var envelope receiptEnvelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("stdout must be one JSON object: %v; %q", err, stdout.String())
			}
			if !envelope.OK || envelope.Operation != "generate" || envelope.Data.Queue.BatchID != "batch-sync-failure" ||
				!slices.Equal(envelope.Data.ResolvedSettings.Seeds, []uint32{42}) || len(envelope.Warnings) != 1 ||
				envelope.Warnings[0].Code != "ui_sync_failed" || !reflect.DeepEqual(envelope.Data.Warnings, envelope.Warnings) {
				t.Fatalf("envelope=%#v", envelope)
			}
			if test.noWait && (polls.Load() != 0 || len(envelope.Data.Outputs) != 0) {
				t.Fatalf("no-wait polls=%d outputs=%#v", polls.Load(), envelope.Data.Outputs)
			}
			if !test.noWait && (polls.Load() != 1 || len(envelope.Data.Outputs) != 1 ||
				envelope.Data.Outputs[0].Seed != 42 || envelope.Data.Outputs[0].Image.ImageName != "generated.png") {
				t.Fatalf("completed polls=%d outputs=%#v", polls.Load(), envelope.Data.Outputs)
			}
		})
	}
}

func TestGenerateDoesNotRecallAfterInconclusiveEnqueue(t *testing.T) {
	isolateUserConfigDir(t)
	var enqueues, recalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", animaOpenAPIFixture("", ""), animaModelInventory()) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/queue/default/enqueue_batch":
			enqueues.Add(1)
			_ = json.MarshalWrite(w, map[string]any{
				"queue_id": "default", "enqueued": 1, "requested": 2, "item_ids": []int{17},
				"batch": map[string]any{"batch_id": "incomplete"},
			})
		case "/api/v1/recall/default":
			recalls.Add(1)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := cli.New(&stdout, &stderr).Run(t.Context(), []string{
		"generate", "--no-wait", "--model", "main-key", "--prompt", "test", "--seed", "9",
		"--output-count", "2", "--url", server.URL, "--json",
	})
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if code != result.ExitInvokeAIFailure || stderr.Len() != 0 || enqueues.Load() != 1 || recalls.Load() != 0 ||
		envelope.OK || envelope.Error == nil || envelope.Error.Code != result.CodeOutcomeUnknown {
		t.Fatalf("code=%d stderr=%q enqueues=%d recalls=%d envelope=%#v", code, stderr.String(), enqueues.Load(), recalls.Load(), envelope)
	}
}
