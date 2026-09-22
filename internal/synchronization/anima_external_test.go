package synchronization_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"github.com/avienor/bediz/internal/generation"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/synchronization"
)

func TestFailedRecallPreservesExistingReceiptWarning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Recall unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	receipt := generation.ExecutionReceipt{
		ResolvedSettings: generation.ResolvedSettings{ModelKey: "main-key", Seeds: []uint32{42}},
		Queue:            generation.QueueReceipt{BatchID: "existing-batch"},
		Warnings:         []result.Warning{{Code: "existing_warning", Message: "Keep this warning"}},
	}
	want := receipt
	want.Warnings = []result.Warning{
		{Code: "existing_warning", Message: "Keep this warning"},
		{Code: "ui_sync_failed", Message: "Generation was accepted, but the UI Recall patch could not be confirmed."},
	}
	got := synchronization.SynchronizeAnima(t.Context(), client, receipt)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("synchronized receipt = %#v, want %#v", got, want)
	}
}

func TestSuccessfulRecallPreservesExistingReceiptWarning(t *testing.T) {
	openAPI, err := os.ReadFile("../doctor/testdata/invokeai_6_14_anima_openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write(openAPI)
		case "/api/v2/models/":
			_, _ = w.Write([]byte(`{"models":[{"key":"main-key","hash":"blake3:main","name":"Anima Main","base":"anima","type":"main"}]}`))
		case "/api/v1/recall/default":
			_, _ = w.Write([]byte(`{"status":"success"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	receipt := generation.ExecutionReceipt{
		ResolvedSettings: generation.ResolvedSettings{ModelKey: "main-key", Width: 768, Height: 1024, Steps: 24, Seeds: []uint32{42}},
		Queue:            generation.QueueReceipt{BatchID: "existing-batch"},
		Warnings:         []result.Warning{{Code: "existing_warning", Message: "Keep this warning"}},
	}
	want := receipt
	want.Warnings = []result.Warning{
		{Code: "existing_warning", Message: "Keep this warning"},
		{
			Code:    "ui_sync_partial",
			Message: "InvokeAI accepted the Recall patch; stock InvokeAI does not restore all generation controls.",
			Details: map[string]any{"not_restored": []string{
				"scheduler", "guidance", "vae", "qwen3_encoder", "output_count", "board_id",
			}},
		},
	}
	got := synchronization.SynchronizeAnima(t.Context(), client, receipt)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("synchronized receipt = %#v, want %#v", got, want)
	}
}
