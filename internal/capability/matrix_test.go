package capability

import (
	"slices"
	"testing"

	"github.com/avienor/bediz/internal/result"
)

func TestMatrixAdvertisesOnlyImplementedOperations(t *testing.T) {
	want := []string{
		result.OperationModelsList,
		result.OperationModelsInstall,
		result.OperationModelsInstall,
		result.OperationModelsStatus,
		result.OperationImagesList,
		result.OperationImagesGet,
		result.OperationImagesUpload,
		result.OperationQueueList,
		result.OperationQueueGet,
		result.OperationQueueWait,
		result.OperationQueueCancel,
		result.OperationBoardsList,
		result.OperationBoardsGet,
		result.OperationBoardsCreate,
		result.OperationGenerate,
		result.OperationGenerate,
		result.OperationGenerate,
		result.OperationUpscale,
		result.OperationUpscale,
		result.OperationRecall,
		result.OperationAuthHFStatus,
		result.OperationAuthHFLogin,
		result.OperationAuthHFLogout,
	}
	got := make([]string, len(Matrix))
	for i, entry := range Matrix {
		got[i] = entry.Operation
	}

	if !slices.Equal(got, want) {
		t.Fatalf("advertised operations = %q, want implemented operations %q", got, want)
	}
	if Matrix[2].Family != "starter" || !slices.Contains(Matrix[2].Endpoints, EndpointRequirement{Method: "GET", Path: "/api/v2/models/starter_models"}) {
		t.Fatalf("starter capability = %#v", Matrix[2])
	}
}

func TestMatrixRegistersAnimaDirectExecutionRequirements(t *testing.T) {
	var animaEntry *Entry
	for _, entry := range Matrix {
		if entry.Operation == result.OperationGenerate {
			animaEntry = &entry
			break
		}
	}
	if animaEntry == nil {
		t.Fatal("Matrix does not register generate operation")
	}
	if animaEntry.Family != "anima" {
		t.Fatalf("Family = %q, want %q", animaEntry.Family, "anima")
	}
	if animaEntry.VersionPolicy != VersionPolicySupportedRange {
		t.Fatalf("VersionPolicy = %q, want %q", animaEntry.VersionPolicy, VersionPolicySupportedRange)
	}
	if animaEntry.UISync != "partial" {
		t.Fatalf("UISync = %q, want verified partial level", animaEntry.UISync)
	}

	wantEndpoints := []EndpointRequirement{
		{Method: "GET", Path: "/api/v1/app/version"},
		{Method: "GET", Path: "/api/v2/models/"},
		{Method: "POST", Path: "/api/v1/queue/{queue_id}/enqueue_batch"},
		{Method: "GET", Path: "/api/v1/queue/{queue_id}/i/{item_id}"},
		{Method: "GET", Path: "/api/v1/images/i/{image_name}"},
	}
	if !slices.Equal(animaEntry.Endpoints, wantEndpoints) {
		t.Fatalf("Endpoints = %#v, want %#v", animaEntry.Endpoints, wantEndpoints)
	}

	wantInvocations := []InvocationRequirement{
		{
			Schema: "AnimaModelLoaderInvocation", Type: "anima_model_loader",
			Properties: []string{"id", "is_intermediate", "use_cache", "type", "model", "vae_model", "qwen3_encoder_model"},
		},
		{
			Schema: "StringInvocation", Type: "string",
			Properties: []string{"id", "is_intermediate", "use_cache", "type", "value"},
		},
		{
			Schema: "AnimaTextEncoderInvocation", Type: "anima_text_encoder",
			Properties: []string{"id", "is_intermediate", "use_cache", "type", "prompt", "qwen3_encoder"},
		},
		{
			Schema: "CollectInvocation", Type: "collect",
			Properties: []string{"id", "is_intermediate", "use_cache", "type", "collection", "item"},
		},
		{
			Schema: "IntegerInvocation", Type: "integer",
			Properties: []string{"id", "is_intermediate", "use_cache", "type", "value"},
		},
		{
			Schema: "AnimaDenoiseInvocation", Type: "anima_denoise",
			Properties: []string{
				"id", "is_intermediate", "use_cache", "type", "denoising_start", "denoising_end", "add_noise",
				"guidance_scale", "width", "height", "steps", "seed", "scheduler", "transformer",
				"positive_conditioning", "negative_conditioning",
			},
		},
		{
			Schema: "CoreMetadataInvocation", Type: "core_metadata",
			Properties: []string{
				"id", "is_intermediate", "use_cache", "type", "generation_mode", "negative_prompt", "width", "height",
				"cfg_scale", "steps", "scheduler", "model", "vae", "qwen3_encoder", "seed", "positive_prompt",
			},
		},
		{
			Schema: "AnimaLatentsToImageInvocation", Type: "anima_l2i",
			Properties: []string{"id", "is_intermediate", "use_cache", "type", "board", "latents", "metadata", "vae"},
		},
	}
	if !slices.EqualFunc(animaEntry.Invocations, wantInvocations, func(a, b InvocationRequirement) bool {
		return a.Schema == b.Schema && a.Type == b.Type && slices.Equal(a.Properties, b.Properties)
	}) {
		t.Fatalf("Invocations = %#v, want %#v", animaEntry.Invocations, wantInvocations)
	}

	wantModels := []ModelRequirement{
		{Name: "Anima main model", Types: []string{"main"}, Bases: []string{"anima"}, MinimumCount: 1},
		{Name: "Anima VAE", Types: []string{"vae"}, Bases: []string{"anima"}, MinimumCount: 1},
		{Name: "Qwen3 encoder", Types: []string{"qwen3_encoder"}, Bases: []string{"any"}, MinimumCount: 1},
	}
	if !slices.EqualFunc(animaEntry.Models, wantModels, func(a, b ModelRequirement) bool {
		return a.Name == b.Name && a.MinimumCount == b.MinimumCount &&
			slices.Equal(a.Types, b.Types) && slices.Equal(a.Bases, b.Bases)
	}) {
		t.Fatalf("Models = %#v, want %#v", animaEntry.Models, wantModels)
	}
}

func TestMatrixRegistersRecallSeparatelyFromDirectExecution(t *testing.T) {
	var recall Entry
	for _, entry := range Matrix {
		if entry.Operation == result.OperationRecall {
			recall = entry
			break
		}
	}
	if recall.Operation != result.OperationRecall || recall.VersionPolicy != VersionPolicySupportedRange ||
		recall.UISync != "" || !slices.Equal(recall.Endpoints, []EndpointRequirement{{Method: "POST", Path: RecallEndpoint}}) {
		t.Fatalf("Recall capability = %#v, want independent supported-version Recall requirement", recall)
	}
	for _, endpoint := range AnimaGenerationEntry().Endpoints {
		if endpoint.Path == RecallEndpoint {
			t.Fatalf("Direct Execution incorrectly requires Recall: %#v", endpoint)
		}
	}
}

func TestSupportsInvokeAI(t *testing.T) {
	tests := []struct {
		version string
		want    bool
		wantErr bool
	}{
		// Supported range boundaries and stable build metadata.
		{version: "6.14.1", want: true},
		{version: "6.14.9", want: true},
		{version: "6.14.1+build", want: true},
		{version: "6.14.1+build.1.2", want: true},

		// Unsupported minor and major versions, including the exclusive upper boundary.
		{version: "6.13.9", want: false},
		{version: "6.14.0", want: false},
		{version: "6.15.0", want: false},
		{version: "7.0.0", want: false},

		// Ordinary prereleases are well-formed but unsupported.
		{version: "6.14.1-rc1", want: false},
		{version: "6.14.1-rc.1", want: false},
		{version: "6.14.1-0", want: false},
		{version: "6.14.1-0a", want: false},
		{version: "6.14.1-alpha-2", want: false},

		// Empty and malformed versions.
		{version: "", wantErr: true},
		{version: "v6.14.1", wantErr: true},
		{version: "6.14", wantErr: true},
		{version: "6.14.1.2", wantErr: true},
		{version: "6.014.1", wantErr: true},
		{version: "6.14.01", wantErr: true},
		{version: "6.14.1-", wantErr: true},
		{version: "6.14.1-alpha.", wantErr: true},
		{version: "6.14.1-alpha..1", wantErr: true},
		{version: "6.14.1-01", wantErr: true},
		{version: "6.14.1-rc.01", wantErr: true},
		{version: "6.14.1+", wantErr: true},
		{version: "6.14.1+.", wantErr: true},
		{version: "latest", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.version, func(t *testing.T) {
			got, err := SupportsInvokeAI(test.version)
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr %t", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("supported = %t, want %t", got, test.want)
			}
		})
	}
}
