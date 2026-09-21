package models_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/models"
	"github.com/avienor/bediz/internal/operation"
)

func TestListAppliesExactModelFilters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if r.URL.Path != "/api/v2/models/" ||
			!reflect.DeepEqual(query["base_models"], []string{"anima", "sdxl"}) ||
			query.Get("model_type") != "main" ||
			query.Get("model_format") != "checkpoint" ||
			query.Get("model_name") != "Exact Name" {
			t.Errorf("unexpected request path=%q query=%v", r.URL.Path, query)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{
			{"key": "model-key", "name": "Exact Name", "base": "anima", "type": "main", "format": "checkpoint"},
		}})
	}))
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}

	result, err := models.List(context.Background(), client, models.ListRequest{
		SchemaVersion: 1,
		BaseModels:    []string{"anima", "sdxl"},
		ModelType:     "main",
		ModelFormat:   "checkpoint",
		ModelName:     "Exact Name",
	})

	if err != nil {
		t.Fatal(err)
	}
	if len(result.Models) != 1 || result.Models[0].Key != "model-key" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestListSortsModelsByNameThenKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{
			{"key": "same-b", "name": "Same"},
			{"key": "same-a", "name": "Same"},
			{"key": "earlier", "name": "Earlier"},
		}})
	}))
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}

	result, err := models.List(context.Background(), client, models.ListRequest{SchemaVersion: 1})

	if err != nil {
		t.Fatal(err)
	}
	got := []string{result.Models[0].Key, result.Models[1].Key, result.Models[2].Key}
	if want := []string{"earlier", "same-a", "same-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("model order = %v, want %v", got, want)
	}
}

func TestListRejectsUnsupportedSchemaVersionBeforeRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}

	_, err = models.List(context.Background(), client, models.ListRequest{SchemaVersion: 2})

	if _, ok := errors.AsType[*operation.InvalidRequestError](err); !ok {
		t.Fatalf("error = %v, want invalid request", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("requests = %d, want 0", requests.Load())
	}
}
