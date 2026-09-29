package structurederror_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/avienor/bediz/internal/generation"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/images"
	"github.com/avienor/bediz/internal/operation"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/sourceimage"
	"github.com/avienor/bediz/internal/structurederror"
)

func TestGenerateProfilePreferenceFailures(t *testing.T) {
	warnings := []result.Warning{
		{Code: "profile_preference_skipped", Message: "profile VAE was not found", Details: map[string]any{"profile": "portrait", "component": "vae", "reason": "not_found"}},
		{Code: "profile_preference_skipped", Message: "profile encoder is ambiguous", Details: map[string]any{"profile": "portrait", "component": "qwen3_encoder", "reason": "ambiguous"}},
	}
	source := images.Reference{ImageName: "source.png", ImageURL: "http://localhost/images/source.png", ThumbnailURL: "http://localhost/thumbnails/source.png", Width: 1024, Height: 768}
	tests := []struct {
		name, wrappedMessage string
		err                  error
		want                 structurederror.Failure
	}{
		{
			name: "fallback resolution retains all warnings in order",
			err:  &generation.ProfilePreferenceError{Err: operation.MissingComponent("vae", "anima", "vae", "install an Anima VAE"), Warnings: warnings},
			want: structurederror.Failure{
				Error: result.Error{
					Code: "missing_component", Message: "no compatible vae is installed; install an Anima VAE",
					Details: map[string]any{"component_type": "vae", "required_base": "anima", "required_type": "vae", "installation_guidance": "install an Anima VAE"},
				},
				Warnings: warnings,
			},
		},
		{
			name: "compatibility failure retains its message",
			err:  &generation.ProfilePreferenceError{Err: operation.UnsupportedCapability("required invocation is missing"), Warnings: warnings},
			want: structurederror.Failure{Error: result.Error{Code: "unsupported_capability", Message: "required invocation is missing"}, Warnings: warnings},
		},
		{
			name:           "compilation failure retains its message",
			err:            &generation.ProfilePreferenceError{Err: errors.New("compile graph: unresolved source"), Warnings: warnings},
			want:           structurederror.Failure{Error: result.Error{Code: "invokeai_operation_failed", Message: "compile graph: unresolved source"}, Warnings: warnings},
			wrappedMessage: "submit: compile graph: unresolved source",
		},
		{
			name: "uploaded source and unknown enqueue retain all context",
			err:  &sourceimage.UploadedError{Source: source, Err: &generation.ProfilePreferenceError{Err: &httpclient.OutcomeUnknownError{StatusCode: 502}, Warnings: warnings}},
			want: structurederror.Failure{
				Error: result.Error{
					Code: "outcome_unknown", Message: "InvokeAI may have accepted the operation; inspect remote state before retrying",
					Details: map[string]any{"status": 502, "source_image": source, "source_uploaded": true},
				},
				Warnings: warnings,
			},
		},
		{
			name: "uploaded source extraction preserves the outer warning exception",
			err:  &generation.ProfilePreferenceError{Err: &sourceimage.UploadedError{Source: source, Err: &httpclient.InvalidResponseError{Err: errors.New("invalid image URL")}}, Warnings: warnings},
			want: structurederror.Failure{Error: result.Error{
				Code: "invalid_invokeai_response", Message: "InvokeAI returned an invalid response",
				Details: map[string]any{"source_image": source, "source_uploaded": true},
			}},
		},
		{
			name: "empty preference warnings remain an empty slice",
			err:  &generation.ProfilePreferenceError{Err: operation.InvalidRequest("invalid model"), Warnings: []result.Warning{}},
			want: structurederror.Failure{Error: result.Error{Code: "invalid_request", Message: "invalid model"}, Warnings: []result.Warning{}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertFailure(t, structurederror.Classify(result.OperationGenerate, test.err), test.want)
			wrapped := test.want
			if test.wrappedMessage != "" {
				wrapped.Error.Message = test.wrappedMessage
			}
			assertFailure(t, structurederror.Classify(result.OperationGenerate, fmt.Errorf("submit: %w", test.err)), wrapped)
		})
	}
}
