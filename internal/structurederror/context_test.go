package structurederror_test

import (
	"context"
	"errors"
	"testing"

	"github.com/avienor/bediz/internal/document"
	"github.com/avienor/bediz/internal/generation"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/images"
	"github.com/avienor/bediz/internal/operation"
	"github.com/avienor/bediz/internal/queue"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/sourceimage"
	"github.com/avienor/bediz/internal/structurederror"
	"github.com/avienor/bediz/internal/upscale"
)

func TestFailureContext(t *testing.T) {
	source := images.Reference{
		ImageName: "source.png", ImageURL: "/images/source.png", ThumbnailURL: "/thumbnails/source.png",
		ImageOrigin: "external", ImageCategory: "general", Width: 1024, Height: 768,
		CreatedAt: "2026-09-29T12:00:00Z", UpdatedAt: "2026-09-29T12:01:00Z",
		SessionID: new("session-a"), NodeID: new("node-a"), BoardID: new("board-a"),
		IsIntermediate: true, Starred: true, HasWorkflow: true,
	}
	warning := result.Warning{
		Code: "profile_preference_skipped", Message: "profile VAE was not found",
		Details: map[string]any{"profile": "portrait", "component": "vae", "reason": "not_found"},
	}
	tests := []struct {
		name string
		err  error
		want structurederror.Failure
	}{
		{
			name: "uploaded source adds to invalid request details",
			err:  &sourceimage.UploadedError{Source: source, Err: operation.InvalidField("scheduler", "unsupported scheduler")},
			want: structurederror.Failure{Error: result.Error{
				Code: "invalid_request", Message: "unsupported scheduler",
				Details: map[string]any{"field": "scheduler", "source_image": source, "source_uploaded": true},
			}},
		},
		{
			name: "uploaded source retains only the cause message on an unclassified failure",
			err:  &sourceimage.UploadedError{Source: source, Err: errors.New("submission failed")},
			want: structurederror.Failure{Error: result.Error{
				Code: "invokeai_operation_failed", Message: "submission failed",
				Details: map[string]any{"source_image": source, "source_uploaded": true},
			}},
		},
		{
			name: "applied cancellation retains the cause classification",
			err:  &queue.CancelAppliedError{QueueID: "default", ItemID: 11, Status: "canceled", Err: &httpclient.NetworkError{Err: errors.New("connection lost")}},
			want: structurederror.Failure{Error: result.Error{
				Code: "connection_failed", Message: "could not reach InvokeAI",
				Details: map[string]any{"cancel_applied": true, "queue_id": "default", "item_id": 11, "item_status": "canceled"},
			}},
		},
		{
			name: "applied cancellation details overwrite the cause position",
			err: &queue.CancelAppliedError{QueueID: "default", ItemID: 11, Status: "canceled", Err: &operation.InvalidQueueResultError{
				Position: operation.QueuePosition{QueueID: "other", BatchID: "batch-a", ItemIDs: []int{12}}, ItemID: 12, Status: "completed", Detail: "has invalid outputs",
			}},
			want: structurederror.Failure{Error: result.Error{
				Code: "invalid_invokeai_response", Message: "queue item 12 has invalid outputs",
				Details: map[string]any{"cancel_applied": true, "queue_id": "default", "item_id": 11, "item_status": "canceled", "batch_id": "batch-a", "item_ids": []int{12}, "status": "completed"},
			}},
		},
		{
			name: "profile preference warnings accompany a failure",
			err:  &upscale.ProfilePreferenceError{Err: operation.MissingComponent("vae", "sdxl", "vae", "install an SDXL VAE"), Warnings: []result.Warning{warning}},
			want: structurederror.Failure{
				Error: result.Error{
					Code: "missing_component", Message: "no compatible vae is installed; install an SDXL VAE",
					Details: map[string]any{"component_type": "vae", "required_base": "sdxl", "required_type": "vae", "installation_guidance": "install an SDXL VAE"},
				},
				Warnings: []result.Warning{warning},
			},
		},
		{
			name: "profile preference warnings inside an uploaded source failure",
			err:  &sourceimage.UploadedError{Source: source, Err: &upscale.ProfilePreferenceError{Err: operation.InvalidRequest("invalid model"), Warnings: []result.Warning{warning}}},
			want: structurederror.Failure{
				Error:    result.Error{Code: "invalid_request", Message: "invalid model", Details: map[string]any{"source_image": source, "source_uploaded": true}},
				Warnings: []result.Warning{warning},
			},
		},
		{
			name: "uploaded source extraction preserves the existing outer warning behavior",
			err:  &upscale.ProfilePreferenceError{Err: &sourceimage.UploadedError{Source: source, Err: operation.InvalidRequest("invalid model")}, Warnings: []result.Warning{warning}},
			want: structurederror.Failure{Error: result.Error{
				Code: "invalid_request", Message: "invalid model", Details: map[string]any{"source_image": source, "source_uploaded": true},
			}},
		},
		{
			name: "uploaded source takes precedence over an outer applied cancellation",
			err:  &queue.CancelAppliedError{QueueID: "default", ItemID: 11, Status: "canceled", Err: &sourceimage.UploadedError{Source: source, Err: &httpclient.InvalidResponseError{Err: errors.New("invalid item")}}},
			want: structurederror.Failure{Error: result.Error{
				Code: "invalid_invokeai_response", Message: "InvokeAI returned an invalid response", Details: map[string]any{"source_image": source, "source_uploaded": true},
			}},
		},
		{
			name: "uploaded source takes precedence over an inner applied cancellation",
			err:  &sourceimage.UploadedError{Source: source, Err: &queue.CancelAppliedError{QueueID: "default", ItemID: 11, Status: "canceled", Err: &httpclient.InvalidResponseError{Err: errors.New("invalid item")}}},
			want: structurederror.Failure{Error: result.Error{
				Code: "invalid_invokeai_response", Message: "InvokeAI returned an invalid response", Details: map[string]any{"source_image": source, "source_uploaded": true},
			}},
		},
		{
			name: "uploaded source overrides the scale failure source",
			err: &sourceimage.UploadedError{Source: source, Err: &upscale.ScaleNotAppliedError{
				ExpectedWidth: 2048, ExpectedHeight: 2048, ActualWidth: 1024, ActualHeight: 1024,
				Queue:       upscale.QueueReceipt{QueueID: "default", BatchID: "batch-a", ItemIDs: []int{11}},
				Output:      upscale.Output{ItemID: 11, Seed: 42, Image: images.Reference{ImageName: "output.png"}},
				SourceImage: images.Reference{ImageName: "different-source.png"},
			}},
			want: structurederror.Failure{Error: result.Error{
				Code: "invokeai_operation_failed", Message: "upscale output dimensions 1024 × 1024 do not match expected 2048 × 2048",
				Details: map[string]any{
					"reason": "scale_not_applied", "expected_width": 2048, "expected_height": 2048, "actual_width": 1024, "actual_height": 1024,
					"queue_id": "default", "batch_id": "batch-a", "item_ids": []int{11}, "item_id": 11, "seed": uint32(42),
					"output_image": images.Reference{ImageName: "output.png"}, "source_image": source, "source_uploaded": true,
				},
			}},
		},
		{
			name: "empty preference warnings remain an empty slice",
			err:  &upscale.ProfilePreferenceError{Err: operation.InvalidRequest("invalid model"), Warnings: []result.Warning{}},
			want: structurederror.Failure{Error: result.Error{Code: "invalid_request", Message: "invalid model"}, Warnings: []result.Warning{}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertFailure(t, structurederror.Classify(result.OperationUpscale, test.err), test.want)
		})
	}
}

func TestClassificationPrecedence(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want structurederror.Failure
	}{
		{
			name: "generation profile before upscale profile",
			err: errors.Join(
				&upscale.ProfileLoadError{Name: "upscale", Err: errors.New("invalid document")},
				&generation.ProfileLoadError{Name: "generate", Err: errors.New("invalid document")},
			),
			want: structurederror.Failure{Error: result.Error{Code: "invalid_configuration", Message: `load profile "generate": invalid document`, Details: map[string]any{"name": "generate"}}},
		},
		{
			name: "profile load before its domain cause",
			err:  &generation.ProfileLoadError{Name: "portrait", Err: operation.InvalidRequest("invalid saved request")},
			want: structurederror.Failure{Error: result.Error{Code: "invalid_configuration", Message: `load profile "portrait": invalid saved request`, Details: map[string]any{"name": "portrait"}}},
		},
		{
			name: "output write before its transport cause",
			err:  &operation.OutputWriteError{Path: "/images/output.png", Err: &httpclient.InvalidResponseError{URL: "InvokeAI", Err: errors.New("invalid image")}},
			want: structurederror.Failure{Error: result.Error{Code: "output_write_failed", Message: `write output "/images/output.png": invalid response from InvokeAI: invalid image`, Details: map[string]any{"path": "/images/output.png"}}},
		},
		{
			name: "unknown outcome before interruption",
			err:  &httpclient.OutcomeUnknownError{Err: context.Canceled},
			want: structurederror.Failure{Error: result.Error{Code: "outcome_unknown", Message: "InvokeAI may have accepted the operation; inspect remote state before retrying"}},
		},
		{
			name: "interruption before network failure",
			err:  &httpclient.NetworkError{Err: context.Canceled},
			want: structurederror.Failure{Error: result.Error{Code: "interrupted", Message: "operation was interrupted locally"}},
		},
		{
			name: "network failure before invalid response",
			err:  &httpclient.InvalidResponseError{Err: &httpclient.NetworkError{Err: errors.New("connection lost")}},
			want: structurederror.Failure{Error: result.Error{Code: "connection_failed", Message: "could not reach InvokeAI"}},
		},
		{
			name: "invalid response before its HTTP cause",
			err:  &httpclient.InvalidResponseError{Err: &httpclient.HTTPError{StatusCode: 401}},
			want: structurederror.Failure{Error: result.Error{Code: "invalid_invokeai_response", Message: "InvokeAI returned an invalid response"}},
		},
		{
			name: "domain invalid request message survives a document wrapper",
			err:  &document.FieldError{Field: "model", Err: operation.InvalidRequest("model is required")},
			want: structurederror.Failure{Error: result.Error{Code: "invalid_request", Message: "model is required", Details: map[string]any{"field": "model"}}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertFailure(t, structurederror.Classify(result.OperationGenerate, test.err), test.want)
		})
	}
}
