package structurederror_test

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/avienor/bediz/internal/generation"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/huggingface"
	"github.com/avienor/bediz/internal/images"
	"github.com/avienor/bediz/internal/models"
	"github.com/avienor/bediz/internal/operation"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/structurederror"
	"github.com/avienor/bediz/internal/upscale"
)

func TestClassifyFailures(t *testing.T) {
	tests := []struct {
		name           string
		operation      string
		err            error
		want           structurederror.Failure
		wrappedMessage string
	}{
		{
			name: "missing generation profile",
			err:  &generation.ProfileLoadError{Name: "portrait", Err: os.ErrNotExist},
			want: structurederror.Failure{Error: result.Error{
				Code: "not_found", Message: `profile "portrait" was not found`,
			}},
		},
		{
			name: "invalid generation profile",
			err:  &generation.ProfileLoadError{Name: "portrait", Err: errors.New("invalid document")},
			want: structurederror.Failure{Error: result.Error{
				Code: "invalid_configuration", Message: `load profile "portrait": invalid document`,
				Details: map[string]any{"name": "portrait"},
			}},
		},
		{
			name: "missing upscale profile",
			err:  &upscale.ProfileLoadError{Name: "portrait", Err: os.ErrNotExist},
			want: structurederror.Failure{Error: result.Error{
				Code: "not_found", Message: `profile "portrait" was not found`,
			}},
		},
		{
			name: "invalid upscale profile",
			err:  &upscale.ProfileLoadError{Name: "portrait", Err: errors.New("invalid document")},
			want: structurederror.Failure{Error: result.Error{
				Code: "invalid_configuration", Message: `load profile "portrait": invalid document`,
				Details: map[string]any{"name": "portrait"},
			}},
		},
		{
			name: "inapplicable profile setting",
			err:  &generation.ProfileSettingError{Profile: "portrait", Field: "components.vae"},
			want: structurederror.Failure{Error: result.Error{
				Code: "invalid_request", Message: `profile "portrait" setting "components.vae" is not applicable to the selected model`,
				Details: map[string]any{"source": "profile", "profile": "portrait", "field": "components.vae"},
			}},
		},
		{
			name: "repository access could not be verified", err: &models.RepositoryAccessError{},
			want: structurederror.Failure{Error: result.Error{
				Code: "connection_failed", Message: "could not verify public Hugging Face repository access",
			}},
		},
		{
			name: "missing Civitai metadata", err: &models.CivitaiMetadataAccessError{StatusCode: 404},
			want: structurederror.Failure{Error: result.Error{Code: "not_found", Message: "Civitai metadata was not found"}},
		},
		{
			name: "Civitai metadata access could not be verified", err: &models.CivitaiMetadataAccessError{StatusCode: 503},
			want: structurederror.Failure{Error: result.Error{Code: "connection_failed", Message: "could not verify Civitai metadata"}},
		},
		{
			name: "authentication required", err: &operation.AuthenticationRequiredError{Message: "source token is required"},
			want: structurederror.Failure{Error: result.Error{Code: "authentication_failed", Message: "source token is required"}},
		},
		{
			name: "Hugging Face rejected token", err: &huggingface.RejectedTokenError{},
			want: structurederror.Failure{Error: result.Error{Code: "authentication_failed", Message: "Hugging Face rejected the token"}},
		},
		{
			name: "Hugging Face token was not cleared", err: &huggingface.UnchangedStateError{},
			want: structurederror.Failure{Error: result.Error{Code: "invokeai_operation_failed", Message: "InvokeAI did not clear the Hugging Face token"}},
		},
		{
			name: "board name exists", err: &operation.BoardNameExistsError{BoardName: "Portraits", BoardIDs: []string{"board-a", "board-b"}},
			want: structurederror.Failure{Error: result.Error{
				Code: "invalid_request", Message: `a visible board is already named "Portraits"`,
				Details: map[string]any{"reason": "board_name_exists", "board_ids": []string{"board-a", "board-b"}},
			}},
		},
		{
			name: "output exists", err: &operation.OutputExistsError{Path: "/images/portrait.png"},
			want: structurederror.Failure{Error: result.Error{
				Code: "invalid_request", Message: `output "/images/portrait.png" already exists; choose a new path`,
				Details: map[string]any{"reason": "output_exists", "path": "/images/portrait.png"},
			}},
		},
		{
			name: "output write failed", err: &operation.OutputWriteError{Path: "/images/portrait.png", Err: errors.New("disk full")},
			want: structurederror.Failure{Error: result.Error{
				Code: "output_write_failed", Message: `write output "/images/portrait.png": disk full`,
				Details: map[string]any{"path": "/images/portrait.png"},
			}},
		},
		{
			name: "invalid request without field", err: operation.InvalidRequest("model is required"),
			want: structurederror.Failure{Error: result.Error{Code: "invalid_request", Message: "model is required"}},
		},
		{
			name: "invalid request names its field",
			err:  operation.InvalidField("scheduler", "scheduler is not supported"),
			want: structurederror.Failure{Error: result.Error{
				Code: "invalid_request", Message: "scheduler is not supported",
				Details: map[string]any{"field": "scheduler"},
			}},
		},
		{
			name: "missing InvokeAI version", err: &operation.InvalidInvokeAIVersionError{},
			want: structurederror.Failure{Error: result.Error{Code: "invalid_version_response", Message: "InvokeAI version response did not contain a version"}},
		},
		{
			name: "invalid InvokeAI version", err: &operation.InvalidInvokeAIVersionError{Version: "unknown"},
			want: structurederror.Failure{Error: result.Error{
				Code: "invalid_invokeai_version", Message: `invalid InvokeAI version "unknown"`,
				Details: map[string]any{"version": "unknown"},
			}},
		},
		{
			name: "unsupported capability", err: operation.UnsupportedCapability("model family is unsupported"),
			want: structurederror.Failure{Error: result.Error{Code: "unsupported_capability", Message: "model family is unsupported"}},
		},
		{
			name: "Civitai file selection", err: &operation.CivitaiFileSelectionError{
				VersionID: 17, Candidates: []operation.CivitaiFileCandidate{{ID: 21, Name: "model.safetensors", Primary: true}},
			},
			want: structurederror.Failure{Error: result.Error{
				Code: "selection_required", Message: "Civitai version requires one exact file selection",
				Details: map[string]any{"kind": "civitai_file", "selector": 17, "candidates": []operation.CivitaiFileCandidate{{ID: 21, Name: "model.safetensors", Primary: true}}},
			}},
		},
		{
			name: "Civitai version selection", err: &operation.CivitaiVersionSelectionError{
				ModelID: 7, Candidates: []operation.CivitaiVersionCandidate{{ID: 17, Name: "v1"}},
			},
			want: structurederror.Failure{Error: result.Error{
				Code: "selection_required", Message: "Civitai model page requires one exact version selection",
				Details: map[string]any{"kind": "civitai_version", "selector": 7, "candidates": []operation.CivitaiVersionCandidate{{ID: 17, Name: "v1"}}},
			}},
		},
		{
			name: "board selection", err: &operation.BoardSelectionError{
				Selector: "Portraits", Candidates: []operation.BoardCandidate{{BoardID: "board-a", BoardName: "Portraits"}},
			},
			want: structurederror.Failure{Error: result.Error{
				Code: "selection_required", Message: "board name requires one exact board selection",
				Details: map[string]any{"kind": "board", "selector": "Portraits", "candidates": []operation.BoardCandidate{{BoardID: "board-a", BoardName: "Portraits"}}},
			}},
		},
		{
			name: "missing domain resource", err: operation.NotFound("image was not found"),
			want: structurederror.Failure{Error: result.Error{Code: "not_found", Message: "image was not found"}},
		},
		{
			name: "model selection", err: operation.SelectionRequired("model", "portrait", []operation.SelectionCandidate{{Key: "main-a", Name: "portrait", Base: "sdxl", Type: "main"}}),
			want: structurederror.Failure{Error: result.Error{
				Code: "selection_required", Message: "model selector requires one exact selection",
				Details: map[string]any{"kind": "model", "selector": "portrait", "candidates": []operation.SelectionCandidate{{Key: "main-a", Name: "portrait", Base: "sdxl", Type: "main"}}},
			}},
		},
		{
			name: "missing component", err: operation.MissingComponent("vae", "sdxl", "vae", "install an SDXL VAE"),
			want: structurederror.Failure{Error: result.Error{
				Code: "missing_component", Message: "no compatible vae is installed; install an SDXL VAE",
				Details: map[string]any{"component_type": "vae", "required_base": "sdxl", "required_type": "vae", "installation_guidance": "install an SDXL VAE"},
			}},
		},
		{
			name: "upscale scale was not applied", err: &upscale.ScaleNotAppliedError{
				ExpectedWidth: 2048, ExpectedHeight: 2048, ActualWidth: 1024, ActualHeight: 1024,
				Queue:       upscale.QueueReceipt{QueueID: "default", BatchID: "batch-a", ItemIDs: []int{11}},
				Output:      upscale.Output{ItemID: 11, Seed: 42, Image: images.Reference{ImageName: "output.png", Width: 1024, Height: 1024}},
				SourceImage: images.Reference{ImageName: "source.png", Width: 1024, Height: 1024},
			},
			want: structurederror.Failure{Error: result.Error{
				Code: "invokeai_operation_failed", Message: "upscale output dimensions 1024 × 1024 do not match expected 2048 × 2048",
				Details: map[string]any{
					"reason": "scale_not_applied", "expected_width": 2048, "expected_height": 2048, "actual_width": 1024, "actual_height": 1024,
					"queue_id": "default", "batch_id": "batch-a", "item_ids": []int{11}, "item_id": 11, "seed": uint32(42),
					"output_image": images.Reference{ImageName: "output.png", Width: 1024, Height: 1024},
					"source_image": images.Reference{ImageName: "source.png", Width: 1024, Height: 1024},
				},
			}},
		},
		{
			name: "graph wait timeout", err: &operation.WaitTimeoutError{Position: operation.QueuePosition{QueueID: "default", BatchID: "batch-a", ItemIDs: []int{11, 12}}},
			want: structurederror.Failure{Error: result.Error{
				Code: "wait_timeout", Message: "wait timeout elapsed before queue items 11, 12 reached a terminal state; the InvokeAI item was not canceled",
				Details: map[string]any{"queue_id": "default", "batch_id": "batch-a", "item_ids": []int{11, 12}},
			}},
		},
		{
			name: "queue wait timeout", err: &operation.WaitTimeoutError{Position: operation.QueuePosition{QueueID: "default", ItemIDs: []int{11, 12}}, PendingItemIDs: []int{12}},
			want: structurederror.Failure{Error: result.Error{
				Code: "wait_timeout", Message: "wait timeout elapsed before queue item 12 reached a terminal state; the InvokeAI item was not canceled",
				Details: map[string]any{"queue_id": "default", "item_ids": []int{11, 12}, "pending_item_ids": []int{12}},
			}},
		},
		{
			name: "graph wait interrupted", err: &operation.InterruptedError{Position: operation.QueuePosition{QueueID: "default", BatchID: "batch-a", ItemIDs: []int{11, 12}}},
			want: structurederror.Failure{Error: result.Error{
				Code: "interrupted", Message: "waiting for queue items 11, 12 was interrupted locally; the InvokeAI item was not canceled",
				Details: map[string]any{"queue_id": "default", "batch_id": "batch-a", "item_ids": []int{11, 12}},
			}},
		},
		{
			name: "queue wait interrupted", err: &operation.InterruptedError{Position: operation.QueuePosition{QueueID: "default", ItemIDs: []int{11, 12}}, PendingItemIDs: []int{12}},
			want: structurederror.Failure{Error: result.Error{
				Code: "interrupted", Message: "waiting for queue item 12 was interrupted locally; the InvokeAI item was not canceled",
				Details: map[string]any{"queue_id": "default", "item_ids": []int{11, 12}, "pending_item_ids": []int{12}},
			}},
		},
		{
			name: "accepted item failed", err: &operation.ItemFailureError{
				Position: operation.QueuePosition{QueueID: "default", BatchID: "batch-a", ItemIDs: []int{11, 12}}, ItemID: 11, Status: "failed", FailureType: "ValueError", FailureMessage: "invalid model",
			},
			want: structurederror.Failure{Error: result.Error{
				Code: "invokeai_operation_failed", Message: `queue item 11 reached terminal status "failed" (ValueError): invalid model`,
				Details: map[string]any{"queue_id": "default", "batch_id": "batch-a", "item_ids": []int{11, 12}, "item_id": 11, "status": "failed"},
			}},
		},
		{
			name: "accepted item has invalid result", err: &operation.InvalidQueueResultError{
				Position: operation.QueuePosition{QueueID: "default", ItemIDs: []int{11}}, ItemID: 11, Status: "completed", Detail: "completed without an image",
			},
			want: structurederror.Failure{Error: result.Error{
				Code: "invalid_invokeai_response", Message: "queue item 11 completed without an image",
				Details: map[string]any{"queue_id": "default", "item_ids": []int{11}, "item_id": 11, "status": "completed"},
			}},
		},
		{
			name: "unknown mutation outcome", err: &httpclient.OutcomeUnknownError{Err: errors.New("connection lost")},
			want: structurederror.Failure{Error: result.Error{Code: "outcome_unknown", Message: "InvokeAI may have accepted the operation; inspect remote state before retrying"}},
		},
		{
			name: "unknown gateway outcome", err: &httpclient.OutcomeUnknownError{StatusCode: 503, Err: errors.New("gateway unavailable")},
			want: structurederror.Failure{Error: result.Error{
				Code: "outcome_unknown", Message: "InvokeAI may have accepted the operation; inspect remote state before retrying", Details: map[string]any{"status": 503},
			}},
		},
		{
			name: "unknown install outcome", operation: result.OperationModelsInstall, err: &httpclient.OutcomeUnknownError{Err: errors.New("connection lost")},
			want: structurederror.Failure{Error: result.Error{Code: "outcome_unknown", Message: "InvokeAI may have accepted the installation; inspect the current model inventory and install job list before submitting again"}},
		},
		{
			name: "unknown install gateway outcome", operation: result.OperationModelsInstall, err: &httpclient.OutcomeUnknownError{StatusCode: 504, Err: errors.New("gateway timeout")},
			want: structurederror.Failure{Error: result.Error{
				Code: "outcome_unknown", Message: "InvokeAI may have accepted the installation; inspect the current model inventory and install job list before submitting again", Details: map[string]any{"status": 504},
			}},
		},
		{
			name: "local interruption", err: context.Canceled,
			want: structurederror.Failure{Error: result.Error{Code: "interrupted", Message: "operation was interrupted locally"}},
		},
		{
			name: "network failure", err: &httpclient.NetworkError{Err: errors.New("connection refused")},
			want: structurederror.Failure{Error: result.Error{Code: "connection_failed", Message: "could not reach InvokeAI"}},
		},
		{
			name: "response too large", err: &httpclient.ResponseTooLargeError{Limit: 4096},
			want: structurederror.Failure{Error: result.Error{
				Code: "invalid_invokeai_response", Message: "InvokeAI returned a response larger than the limit", Details: map[string]any{"reason": "response_too_large", "limit_bytes": int64(4096)},
			}},
		},
		{
			name: "invalid response", err: &httpclient.InvalidResponseError{Err: errors.New("malformed JSON")},
			want: structurederror.Failure{Error: result.Error{Code: "invalid_invokeai_response", Message: "InvokeAI returned an invalid response"}},
		},
		{
			name: "HTTP unauthorized", err: &httpclient.HTTPError{StatusCode: 401, Detail: "private"},
			want: structurederror.Failure{Error: result.Error{Code: "authentication_failed", Message: "InvokeAI rejected authentication", Details: map[string]any{"status": 401}}},
		},
		{
			name: "HTTP forbidden", err: &httpclient.HTTPError{StatusCode: 403, Detail: "private"},
			want: structurederror.Failure{Error: result.Error{Code: "authentication_failed", Message: "InvokeAI rejected authentication", Details: map[string]any{"status": 403}}},
		},
		{
			name: "HTTP not found", err: &httpclient.HTTPError{StatusCode: 404, Detail: "private"},
			want: structurederror.Failure{Error: result.Error{Code: "not_found", Message: "the requested InvokeAI resource was not found"}},
		},
		{
			name: "HTTP rejection without detail", err: &httpclient.HTTPError{StatusCode: 500},
			want: structurederror.Failure{Error: result.Error{Code: "invokeai_operation_failed", Message: "InvokeAI rejected the operation", Details: map[string]any{"status": 500}}},
		},
		{
			name: "HTTP rejection with detail", err: &httpclient.HTTPError{StatusCode: 422, Detail: "invalid input"},
			want: structurederror.Failure{Error: result.Error{Code: "invokeai_operation_failed", Message: "InvokeAI rejected the operation", Details: map[string]any{"status": 422, "invokeai_detail": "invalid input"}}},
		},
		{
			name: "unclassified failure", err: errors.New("unclassified failure"), wrappedMessage: "outer: unclassified failure",
			want: structurederror.Failure{Error: result.Error{Code: "invokeai_operation_failed", Message: "unclassified failure"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operationName := cmp.Or(test.operation, result.OperationGenerate)
			assertFailure(t, structurederror.Classify(operationName, test.err), test.want)
			t.Run("wrapped", func(t *testing.T) {
				want := test.want
				want.Error.Message = cmp.Or(test.wrappedMessage, want.Error.Message)
				assertFailure(t, structurederror.Classify(operationName, fmt.Errorf("outer: %w", test.err)), want)
			})
		})
	}
}

func assertFailure(t *testing.T, got, want structurederror.Failure) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("failure = %#v, want %#v", got, want)
	}
}
