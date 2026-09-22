package recall

import (
	"context"
	"fmt"
	"net/http"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/generation"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
)

type Request struct {
	SchemaVersion  int     `json:"schema_version"`
	Model          *string `json:"model,omitempty"`
	PositivePrompt *string `json:"positive_prompt,omitempty"`
	NegativePrompt *string `json:"negative_prompt,omitempty"`
	Width          *int    `json:"width,omitempty"`
	Height         *int    `json:"height,omitempty"`
	Steps          *int    `json:"steps,omitempty"`
	Seed           *uint32 `json:"seed,omitempty"`
}

type Result struct {
	QueueID string `json:"queue_id"`
	Mode    string `json:"mode"`
}

type openAPIDocument struct {
	Paths map[string]struct {
		Post struct {
			RequestBody struct {
				Content map[string]struct {
					Schema struct {
						Ref string `json:"$ref"`
					} `json:"schema"`
				} `json:"content"`
			} `json:"requestBody"`
		} `json:"post"`
	} `json:"paths"`
	Components struct {
		Schemas map[string]struct {
			Properties map[string]struct {
				AnyOf []capability.RecallSchemaAlternative `json:"anyOf"`
			} `json:"properties"`
		} `json:"schemas"`
	} `json:"components"`
}

func Submit(ctx context.Context, client *httpclient.Client, request Request) (Result, error) {
	if err := validate(request); err != nil {
		return Result{}, err
	}
	var versionResponse struct {
		Version string `json:"version"`
	}
	if err := client.GetJSON(ctx, "/api/v1/app/version", &versionResponse); err != nil {
		return Result{}, err
	}
	supported, err := capability.SupportsInvokeAI(versionResponse.Version)
	if err != nil {
		return Result{}, operation.UnsupportedCapability(fmt.Sprintf("invalid InvokeAI version %q", versionResponse.Version))
	}
	if !supported {
		return Result{}, operation.UnsupportedCapability(fmt.Sprintf("InvokeAI %s is outside the supported range %s", versionResponse.Version, capability.SupportedInvokeAIRange))
	}
	var openAPI openAPIDocument
	if err := client.GetJSON(ctx, "/openapi.json", &openAPI); err != nil {
		return Result{}, err
	}
	path := openAPI.Paths[capability.RecallEndpoint]
	if path.Post.RequestBody.Content["application/json"].Schema.Ref != capability.RecallSchemaRef {
		return Result{}, operation.UnsupportedCapability("InvokeAI Recall endpoint does not expose the tested request schema")
	}
	properties := openAPI.Components.Schemas["RecallParameter"].Properties
	for _, field := range capability.RecallPatchFields {
		property, ok := properties[field.Name]
		if !ok || !field.MatchesNullableAlternatives(property.AnyOf) {
			return Result{}, operation.UnsupportedCapability(fmt.Sprintf("InvokeAI Recall schema does not support %s as a patch field", field.Name))
		}
	}
	patch := request
	if request.Model != nil {
		var inventory struct {
			Models []generation.ModelIdentifier `json:"models"`
		}
		if err := client.GetJSON(ctx, "/api/v2/models/", &inventory); err != nil {
			return Result{}, err
		}
		model, err := generation.ResolveAnimaMain(inventory.Models, *request.Model)
		if err != nil {
			return Result{}, err
		}
		for _, candidate := range inventory.Models {
			if candidate.Type == "main" && candidate.Name == model.Name && candidate.Key != model.Key {
				return Result{}, operation.UnsupportedCapability(fmt.Sprintf("main model name %q is shared by installed models", model.Name))
			}
		}
		patch.Model = new(model.Name)
	}
	if err := client.DoJSON(ctx, http.MethodPost, "/api/v1/recall/default", patchBody(patch), nil); err != nil {
		return Result{}, err
	}
	return Result{QueueID: "default", Mode: "patch"}, nil
}

func validate(request Request) error {
	if request.SchemaVersion != 1 {
		return operation.InvalidRequest(fmt.Sprintf("unsupported request schema version %d", request.SchemaVersion))
	}
	if request.Model != nil && *request.Model == "" {
		return operation.InvalidRequest("model selector must not be empty")
	}
	if request.Model == nil && request.PositivePrompt == nil && request.NegativePrompt == nil && request.Width == nil && request.Height == nil && request.Steps == nil && request.Seed == nil {
		return operation.InvalidRequest("at least one recall field is required")
	}
	if (request.Width == nil) != (request.Height == nil) {
		return operation.InvalidRequest("width and height must be supplied together or both omitted")
	}
	if (request.Width != nil || request.Steps != nil) && request.Model == nil {
		return operation.InvalidRequest("dimensions and steps require an explicit Anima model")
	}
	if request.Width != nil && (*request.Width < 64 || *request.Width%8 != 0 || *request.Height < 64 || *request.Height%8 != 0) {
		return operation.InvalidRequest("width and height must be multiples of 8 and at least 64 for Recall")
	}
	if request.Steps != nil && *request.Steps < 1 {
		return operation.InvalidRequest("steps must be positive")
	}
	return nil
}

func patchBody(request Request) map[string]any {
	patch := make(map[string]any)
	if request.Model != nil {
		patch["model"] = *request.Model
	}
	if request.PositivePrompt != nil {
		patch["positive_prompt"] = *request.PositivePrompt
	}
	if request.NegativePrompt != nil {
		patch["negative_prompt"] = *request.NegativePrompt
	}
	if request.Width != nil {
		patch["width"] = *request.Width
		patch["height"] = *request.Height
	}
	if request.Steps != nil {
		patch["steps"] = *request.Steps
	}
	if request.Seed != nil {
		patch["seed"] = *request.Seed
	}
	return patch
}
