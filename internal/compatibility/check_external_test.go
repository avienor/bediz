package compatibility_test

import (
	"encoding/json/v2"
	"os"
	"slices"
	"testing"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/compatibility"
	"github.com/avienor/bediz/internal/result"
)

func TestEvaluateCapabilityPredicates(t *testing.T) {
	fixture, err := os.ReadFile("../doctor/testdata/invokeai_6_14_anima_openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		operation string
		family    string
		change    func(*compatibility.Snapshot, map[string]any)
		want      []string
	}{
		{"supported version", result.OperationGenerate, "sdxl", func(s *compatibility.Snapshot, _ map[string]any) { s.SupportedVersion = false }, []string{"unsupported_version"}},
		{"OpenAPI unavailable", result.OperationGenerate, "sdxl", func(s *compatibility.Snapshot, _ map[string]any) { s.OpenAPIAvailable = false }, []string{"openapi_unavailable"}},
		{"endpoint", result.OperationGenerate, "sdxl", func(_ *compatibility.Snapshot, d map[string]any) {
			delete(d["paths"].(map[string]any)["/api/v1/app/version"].(map[string]any), "get")
		}, []string{"missing_endpoint:GET /api/v1/app/version"}},
		{"invocation type", result.OperationGenerate, "sdxl", func(_ *compatibility.Snapshot, d map[string]any) {
			d["components"].(map[string]any)["schemas"].(map[string]any)["SDXLModelLoaderInvocation"].(map[string]any)["properties"].(map[string]any)["type"].(map[string]any)["const"] = "wrong"
		}, []string{"incompatible_invocation:sdxl_model_loader"}},
		{"invocation property", result.OperationGenerate, "sdxl", func(_ *compatibility.Snapshot, d map[string]any) {
			delete(d["components"].(map[string]any)["schemas"].(map[string]any)["SDXLModelLoaderInvocation"].(map[string]any)["properties"].(map[string]any), "model")
		}, []string{"incompatible_invocation:sdxl_model_loader"}},
		{"installed model", result.OperationGenerate, "sdxl", func(s *compatibility.Snapshot, _ map[string]any) { s.Models = nil }, []string{"missing_component:SDXL main model"}},
		{"model inventory unavailable", result.OperationGenerate, "sdxl", func(s *compatibility.Snapshot, _ map[string]any) { s.ModelsAvailable = false }, []string{"models_unavailable"}},
		{"recall body", result.OperationRecall, "", func(_ *compatibility.Snapshot, d map[string]any) {
			d["paths"].(map[string]any)[capability.RecallEndpoint].(map[string]any)["post"] = map[string]any{}
		}, []string{"incompatible_recall_schema:request_body"}},
		{"recall field", result.OperationRecall, "", func(_ *compatibility.Snapshot, d map[string]any) {
			delete(d["components"].(map[string]any)["schemas"].(map[string]any)["RecallParameter"].(map[string]any)["properties"].(map[string]any), "cfg_scale")
		}, []string{"incompatible_recall_schema:cfg_scale"}},
		{"install source and response", result.OperationModelsInstall, "", func(_ *compatibility.Snapshot, d map[string]any) {
			d["paths"].(map[string]any)["/api/v2/models/install"].(map[string]any)["post"] = map[string]any{}
		}, []string{"incompatible_install_schema:source", "incompatible_install_schema:job_response"}},
		{"starter catalog", result.OperationModelsInstall, "starter", func(_ *compatibility.Snapshot, d map[string]any) {
			d["paths"].(map[string]any)["/api/v2/models/starter_models"] = map[string]any{"get": map[string]any{}}
		}, []string{"incompatible_starter_catalog_response"}},
		{"Hugging Face login body", result.OperationAuthHFLogin, "", func(_ *compatibility.Snapshot, d map[string]any) {
			d["paths"].(map[string]any)[capability.HuggingFaceAuthEndpoint].(map[string]any)["post"] = map[string]any{}
		}, []string{"incompatible_hf_login_schema:token"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var raw map[string]any
			if err := json.Unmarshal(fixture, &raw); err != nil {
				t.Fatal(err)
			}
			snapshot := compatibility.Snapshot{SupportedVersion: true, OpenAPIAvailable: true, ModelsAvailable: true, Models: []compatibility.Model{{Key: "sdxl", Hash: "hash", Name: "SDXL", Base: "sdxl", Type: "main"}}}
			test.change(&snapshot, raw)
			encoded, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			err = json.Unmarshal(encoded, &snapshot.Document)
			if err != nil {
				t.Fatal(err)
			}
			var entry capability.Entry
			for _, candidate := range capability.Matrix {
				if candidate.Operation == test.operation && candidate.Family == test.family && (test.operation != result.OperationGenerate || candidate.Mode == "txt2img") {
					entry = candidate
					break
				}
			}
			var got []string
			for _, failure := range compatibility.Evaluate(entry, snapshot) {
				got = append(got, failure.Code)
			}
			if !slices.Equal(got, test.want) {
				t.Fatalf("failures = %q, want %q", got, test.want)
			}
		})
	}
}

func TestEvaluatePreservesFailureOrderAndPreflightMessages(t *testing.T) {
	fixture, err := os.ReadFile("../doctor/testdata/invokeai_6_14_anima_openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(fixture, &raw); err != nil {
		t.Fatal(err)
	}
	delete(raw["paths"].(map[string]any)["/api/v1/app/version"].(map[string]any), "get")
	delete(raw["components"].(map[string]any)["schemas"].(map[string]any), "SDXLModelLoaderInvocation")
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var document compatibility.Document
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	failures := compatibility.Evaluate(capability.SDXLGenerationEntry(), compatibility.Snapshot{
		Document: document, OpenAPIAvailable: true, ModelsAvailable: true,
	})
	want := []compatibility.Failure{
		{Code: "unsupported_version"},
		{Code: "missing_endpoint:GET /api/v1/app/version", Message: "InvokeAI does not provide required endpoint GET /api/v1/app/version"},
		{Code: "incompatible_invocation:sdxl_model_loader", Message: "InvokeAI does not provide required invocation schema SDXLModelLoaderInvocation for sdxl_model_loader"},
		{Code: "missing_component:SDXL main model"},
	}
	if len(failures) != len(want) {
		t.Fatalf("failures = %#v, want %#v", failures, want)
	}
	for i := range want {
		if failures[i] != want[i] {
			t.Fatalf("failure %d = %#v, want %#v", i, failures[i], want[i])
		}
	}
}

func TestEvaluateInvocationMetadataAndModelFormat(t *testing.T) {
	fixture, err := os.ReadFile("../doctor/testdata/invokeai_6_14_anima_openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(fixture, &raw); err != nil {
		t.Fatal(err)
	}
	delete(raw["components"].(map[string]any)["schemas"].(map[string]any)["CoreMetadataInvocation"].(map[string]any), "additionalProperties")
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var document compatibility.Document
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	entry := capability.Entry{Invocations: []capability.InvocationRequirement{{Schema: "CoreMetadataInvocation", Type: "core_metadata", RequiresAdditionalProperties: true}}}
	failures := compatibility.Evaluate(entry, compatibility.Snapshot{OpenAPIAvailable: true, Document: document})
	if len(failures) != 1 || failures[0].Code != "incompatible_invocation:core_metadata" || failures[0].Message != "InvokeAI invocation schema CoreMetadataInvocation does not allow required upscale metadata fields" {
		t.Fatalf("metadata failures = %#v", failures)
	}

	flux := capability.FLUXGenerationEntry().Models[0]
	model := compatibility.Model{Key: "flux", Hash: "hash", Name: "FLUX", Base: "flux", Type: "main", Variant: "dev", Format: "diffusers"}
	if compatibility.InspectModel([]compatibility.Model{model}, flux).Satisfied {
		t.Fatal("unsupported FLUX format satisfied model requirement")
	}
	model.Format = "checkpoint"
	if !compatibility.InspectModel([]compatibility.Model{model}, flux).Satisfied {
		t.Fatal("tested FLUX format did not satisfy model requirement")
	}
}
