package cli_test

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

func TestModelsListReportsSortedTriggerPhrasesWhenRecorded(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/models/" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		fmt.Fprint(w, `{"models":[
			{"key":"lora","name":"A LoRA","base":"sdxl","type":"lora","format":"lycoris","trigger_phrases":["zebra style","Alien style","alien style"]},
			{"key":"main","name":"B Main","base":"sdxl","type":"main","format":"diffusers","file_size":200,"description":"main model","trigger_phrases":["zebra","alpha"],"path":"/private/model","source":"private-source"},
			{"key":"absent","name":"C Absent","base":"anima","type":"lora","format":"lycoris"},
			{"key":"null","name":"D Null","base":"flux","type":"lora","format":"lycoris","trigger_phrases":null},
			{"key":"empty","name":"E Empty","base":"sdxl","type":"lora","format":"lycoris","trigger_phrases":[]}
		]}`)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	exitCode := cli.New(&stdout, &stderr).Run(t.Context(), []string{"models", "list", "--url", server.URL, "--json"})
	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	want := map[string]any{
		"schema_version": float64(1), "ok": true, "operation": "models.list", "warnings": []any{},
		"data": map[string]any{"models": []any{
			map[string]any{"key": "lora", "name": "A LoRA", "base": "sdxl", "type": "lora", "format": "lycoris", "trigger_phrases": []any{"Alien style", "alien style", "zebra style"}},
			map[string]any{"key": "main", "name": "B Main", "base": "sdxl", "type": "main", "format": "diffusers", "size_bytes": float64(200), "description": "main model", "trigger_phrases": []any{"alpha", "zebra"}},
			map[string]any{"key": "absent", "name": "C Absent", "base": "anima", "type": "lora", "format": "lycoris"},
			map[string]any{"key": "null", "name": "D Null", "base": "flux", "type": "lora", "format": "lycoris"},
			map[string]any{"key": "empty", "name": "E Empty", "base": "sdxl", "type": "lora", "format": "lycoris"},
		}},
	}
	if !reflect.DeepEqual(envelope, want) {
		t.Fatalf("envelope = %#v, want %#v", envelope, want)
	}
}

func TestModelsListReportsOnlyRecordedLoRADefaultWeights(t *testing.T) {
	for _, test := range []struct {
		name      string
		modelType string
		settings  string
		weight    *float64
	}{
		{name: "recorded", modelType: "lora", settings: `,"default_settings":{"weight":0.6,"weight_min":-1,"weight_max":2}`, weight: new(0.6)},
		{name: "zero", modelType: "lora", settings: `,"default_settings":{"weight":0}`, weight: new(0.0)},
		{name: "negative", modelType: "lora", settings: `,"default_settings":{"weight":-0.5}`, weight: new(-0.5)},
		{name: "outside generation bounds", modelType: "lora", settings: `,"default_settings":{"weight":12}`, weight: new(12.0)},
		{name: "absent settings", modelType: "lora"},
		{name: "null settings", modelType: "lora", settings: `,"default_settings":null`},
		{name: "absent weight", modelType: "lora", settings: `,"default_settings":{"weight_min":-1,"weight_max":2}`},
		{name: "null weight", modelType: "lora", settings: `,"default_settings":{"weight":null}`},
		{name: "main model", modelType: "main", settings: `,"default_settings":{"weight":0.6}`},
		{name: "control LoRA", modelType: "control_lora", settings: `,"default_settings":{"weight":0.6}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v2/models/" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				fmt.Fprintf(w, `{"models":[{"key":"model-key","name":"Model","base":"sdxl","type":%q,"format":"lycoris"%s}]}`, test.modelType, test.settings)
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			exitCode := cli.New(&stdout, &stderr).Run(t.Context(), []string{"models", "list", "--url", server.URL, "--json"})
			if exitCode != result.ExitSuccess || stderr.Len() != 0 {
				t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
			}
			var envelope struct {
				Data struct {
					Models []map[string]any `json:"models"`
				} `json:"data"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
			}
			want := map[string]any{"key": "model-key", "name": "Model", "base": "sdxl", "type": test.modelType, "format": "lycoris"}
			if test.weight != nil {
				want["default_weight"] = *test.weight
			}
			if !reflect.DeepEqual(envelope.Data.Models, []map[string]any{want}) {
				t.Fatalf("models = %#v, want %#v", envelope.Data.Models, want)
			}
		})
	}
}

func TestModelsListHumanOutputShowsRecordedMetadataAndPreservesOtherLines(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/models/" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		fmt.Fprint(w, `{"models":[
			{"key":"both","name":"A Both","base":"sdxl","type":"lora","format":"lycoris","trigger_phrases":["zebra style","alien style"],"default_settings":{"weight":0.6}},
			{"key":"phrases","name":"B Phrases","base":"sdxl","type":"main","format":"diffusers","trigger_phrases":["zebra","alpha"],"default_settings":{"weight":0.9}},
			{"key":"weight","name":"C Weight","base":"flux","type":"lora","format":"lycoris","default_settings":{"weight":0}},
			{"key":"absent","name":"D Absent","base":"anima","type":"lora","format":"lycoris"},
			{"key":"null","name":"E Null","base":"sdxl","type":"main","format":"checkpoint","trigger_phrases":null,"default_settings":null},
			{"key":"empty","name":"F Empty","base":"sdxl","type":"lora","format":"lycoris","trigger_phrases":[],"default_settings":{"weight":null}}
		]}`)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	exitCode := cli.New(&stdout, &stderr).Run(t.Context(), []string{"models", "list", "--url", server.URL})
	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	want := "both\tA Both\tsdxl\tlora\ttrigger phrases: alien style, zebra style\tdefault weight: 0.6\n" +
		"phrases\tB Phrases\tsdxl\tmain\ttrigger phrases: alpha, zebra\n" +
		"weight\tC Weight\tflux\tlora\tdefault weight: 0\n" +
		"absent\tD Absent\tanima\tlora\n" +
		"null\tE Null\tsdxl\tmain\n" +
		"empty\tF Empty\tsdxl\tlora\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}
