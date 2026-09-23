package recall_test

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"errors"
	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
	"github.com/avienor/bediz/internal/recall"
)

func TestSubmitWithFieldsChecksSchemaAndAddsAdapterPatch(t *testing.T) {
	fixture, err := os.ReadFile("../doctor/testdata/invokeai_6_14_anima_openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(fixture, &document); err != nil {
		t.Fatal(err)
	}
	properties := document["components"].(map[string]any)["schemas"].(map[string]any)["RecallParameter"].(map[string]any)["properties"].(map[string]any)
	field := capability.RecallPatchField{Requirement: capability.RecallFieldRequirement{Name: "cfg_scale", Type: "number"}, Value: 7.0}
	for _, supported := range []bool{false, true} {
		t.Run(map[bool]string{false: "schema missing", true: "schema supported"}[supported], func(t *testing.T) {
			if supported {
				properties["cfg_scale"] = map[string]any{"anyOf": []any{map[string]any{"type": "number"}, map[string]any{"type": "null"}}}
			} else {
				delete(properties, "cfg_scale")
			}
			var posts int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/app/version":
					_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
				case "/openapi.json":
					_ = json.MarshalWrite(w, document)
				case "/api/v1/recall/default":
					posts++
					var patch map[string]any
					if err := json.UnmarshalRead(r.Body, &patch); err != nil {
						t.Error(err)
					}
					if patch["seed"] != float64(42) || patch["cfg_scale"] != float64(7) {
						t.Errorf("patch = %#v", patch)
					}
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
			result, err := recall.SubmitWithFields(t.Context(), client, recall.Request{SchemaVersion: 1, Seed: new(uint32(42))}, []capability.RecallPatchField{field})
			if supported {
				if err != nil || posts != 1 || result.QueueID != "default" || result.Mode != "patch" {
					t.Fatalf("result=%#v err=%v posts=%d", result, err, posts)
				}
			} else if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok || posts != 0 {
				t.Fatalf("err=%v posts=%d", err, posts)
			}
		})
	}
}
