package compatibility_test

import (
	"encoding/json/v2"
	"os"
	"reflect"
	"testing"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/compatibility"
	"github.com/avienor/bediz/internal/result"
)

func TestRecallCapabilityRequiresNullableLoRAArrayAndItemFields(t *testing.T) {
	fixture, err := os.ReadFile("../doctor/testdata/invokeai_6_14_anima_openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"missing field", "non-array", "non-nullable", "wrong reference", "missing item schema", "missing model_name", "missing weight", "wrong model_name", "wrong weight", "valid", "reversed nullable branches"} {
		t.Run(change, func(t *testing.T) {
			var raw map[string]any
			if err := json.Unmarshal(fixture, &raw); err != nil {
				t.Fatal(err)
			}
			schemas := raw["components"].(map[string]any)["schemas"].(map[string]any)
			properties := schemas["RecallParameter"].(map[string]any)["properties"].(map[string]any)
			array := map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/LoRARecallParameter"}}
			properties["loras"] = map[string]any{"anyOf": []any{array, map[string]any{"type": "null"}}}
			items := map[string]any{"model_name": map[string]any{"type": "string"}, "weight": map[string]any{"type": "number"}}
			schemas["LoRARecallParameter"] = map[string]any{"properties": items}
			switch change {
			case "missing field":
				delete(properties, "loras")
			case "non-array":
				array["type"] = "string"
			case "non-nullable":
				properties["loras"] = array
			case "wrong reference":
				array["items"] = map[string]any{"$ref": "#/components/schemas/Other"}
			case "missing item schema":
				delete(schemas, "LoRARecallParameter")
			case "missing model_name":
				delete(items, "model_name")
			case "missing weight":
				delete(items, "weight")
			case "wrong model_name":
				items["model_name"] = map[string]any{"type": "number"}
			case "wrong weight":
				items["weight"] = map[string]any{"type": "string"}
			case "reversed nullable branches":
				properties["loras"] = map[string]any{"anyOf": []any{map[string]any{"type": "null"}, array}}
			}
			encoded, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			var document compatibility.Document
			if err := json.Unmarshal(encoded, &document); err != nil {
				t.Fatal(err)
			}
			entry, found := capability.Find(result.OperationRecall, "", "")
			if !found {
				t.Fatal("Recall capability missing")
			}
			failures := compatibility.Evaluate(entry, compatibility.Snapshot{SupportedVersion: true, OpenAPIAvailable: true, Document: document})
			if change == "valid" || change == "reversed nullable branches" {
				if len(failures) != 0 {
					t.Fatalf("valid schema: %#v", failures)
				}
			} else if !reflect.DeepEqual(failures, []compatibility.Failure{{Code: "incompatible_recall_schema:loras"}}) {
				t.Fatalf("failures = %#v", failures)
			}
		})
	}
}
