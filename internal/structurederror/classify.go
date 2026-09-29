// Package structurederror maps domain failures to the public Structured Error
// contract independently of command parsing and output writing.
package structurederror

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"

	"github.com/avienor/bediz/internal/document"
	"github.com/avienor/bediz/internal/generation"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/huggingface"
	"github.com/avienor/bediz/internal/models"
	"github.com/avienor/bediz/internal/operation"
	queueops "github.com/avienor/bediz/internal/queue"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/sourceimage"
	"github.com/avienor/bediz/internal/upscale"
)

// Failure carries the Structured Error and the warnings that accompany it.
// The caller owns the Result Envelope, output writing, and process exit status.
type Failure struct {
	Error    result.Error
	Warnings []result.Warning
}

// InvalidRequest maps a non-nil decoding or validation failure, retaining an
// identified Request Document field when available.
func InvalidRequest(err error) Failure {
	return Failure{Error: result.Error{
		Code: result.CodeInvalidRequest, Message: err.Error(), Details: invalidRequestDetails(err),
	}}
}

func invalidRequestDetails(err error) map[string]any {
	if invalid, ok := errors.AsType[*operation.InvalidRequestError](err); ok && invalid.Field != "" {
		return map[string]any{"field": invalid.Field}
	}
	if field, ok := errors.AsType[*document.FieldError](err); ok && field.Field != "" {
		return map[string]any{"field": field.Field}
	}
	return nil
}

// Classify maps a non-nil domain failure to its public Structured Error and
// warnings. Uploaded Source Images and applied queue cancellations retain the
// cause's classification and add the completed mutation's details.
func Classify(operationName string, err error) Failure {
	if uploaded, ok := errors.AsType[*sourceimage.UploadedError](err); ok {
		return classify(operationName, uploaded.Err, map[string]any{
			"source_image":    uploaded.Source,
			"source_uploaded": true,
		})
	}
	if applied, ok := errors.AsType[*queueops.CancelAppliedError](err); ok {
		return classify(operationName, applied.Err, map[string]any{
			"cancel_applied": true,
			"queue_id":       applied.QueueID,
			"item_id":        applied.ItemID,
			"item_status":    applied.Status,
		})
	}
	return classify(operationName, err, nil)
}

func classify(operationName string, err error, extra map[string]any) Failure {
	var warnings []result.Warning
	if preference, ok := errors.AsType[*upscale.ProfilePreferenceError](err); ok {
		warnings = preference.Warnings
	}
	fail := func(code, message string, details map[string]any) Failure {
		if len(extra) > 0 {
			details = maps.Clone(details)
			if details == nil {
				details = map[string]any{}
			}
			maps.Copy(details, extra)
		}
		return Failure{Error: result.Error{Code: code, Message: message, Details: details}, Warnings: warnings}
	}
	if profile, ok := errors.AsType[*generation.ProfileLoadError](err); ok {
		if errors.Is(profile.Err, os.ErrNotExist) {
			return fail(result.CodeNotFound, fmt.Sprintf("profile %q was not found", profile.Name), nil)
		}
		return fail(result.CodeInvalidConfiguration, profile.Error(), map[string]any{"name": profile.Name})
	}
	if profile, ok := errors.AsType[*upscale.ProfileLoadError](err); ok {
		if errors.Is(profile.Err, os.ErrNotExist) {
			return fail(result.CodeNotFound, fmt.Sprintf("profile %q was not found", profile.Name), nil)
		}
		return fail(result.CodeInvalidConfiguration, profile.Error(), map[string]any{"name": profile.Name})
	}
	if setting, ok := errors.AsType[*generation.ProfileSettingError](err); ok {
		return fail(result.CodeInvalidRequest, setting.Error(), map[string]any{"source": "profile", "profile": setting.Profile, "field": setting.Field})
	}
	if submission, ok := errors.AsType[*models.StarterSubmissionError](err); ok {
		details := map[string]any{"jobs": submission.Progress.Jobs, "skipped": submission.Progress.Skipped}
		if unknown, uncertain := errors.AsType[*httpclient.OutcomeUnknownError](submission.Cause); uncertain {
			details["uncertain_role"] = submission.Role
			if unknown.StatusCode != 0 {
				details["status"] = unknown.StatusCode
			}
			if submission.DependencyIndex != nil {
				details["uncertain_dependency_index"] = *submission.DependencyIndex
			}
			return fail(result.CodeOutcomeUnknown, "InvokeAI may have accepted the installation; inspect the current model inventory and install job list before submitting again", details)
		}
		details["rejected_role"] = submission.Role
		if submission.DependencyIndex != nil {
			details["rejected_dependency_index"] = *submission.DependencyIndex
		}
		if rejection, ok := errors.AsType[*httpclient.HTTPError](submission.Cause); ok {
			details["status"] = rejection.StatusCode
		}
		return fail(result.CodeInvokeAIOperationFailed, "InvokeAI rejected a starter installation job; reinspect the catalog before retrying", details)
	}
	if _, ok := errors.AsType[*models.RepositoryAccessError](err); ok {
		return fail(result.CodeConnectionFailed, "could not verify public Hugging Face repository access", nil)
	}
	if access, ok := errors.AsType[*models.CivitaiMetadataAccessError](err); ok {
		if access.StatusCode == http.StatusNotFound {
			return fail(result.CodeNotFound, "Civitai metadata was not found", nil)
		}
		return fail(result.CodeConnectionFailed, "could not verify Civitai metadata", nil)
	}
	if auth, ok := errors.AsType[*operation.AuthenticationRequiredError](err); ok {
		return fail(result.CodeAuthenticationFailed, auth.Error(), nil)
	}
	if _, ok := errors.AsType[*huggingface.RejectedTokenError](err); ok {
		return fail(result.CodeAuthenticationFailed, "Hugging Face rejected the token", nil)
	}
	if _, ok := errors.AsType[*huggingface.UnchangedStateError](err); ok {
		return fail(result.CodeInvokeAIOperationFailed, "InvokeAI did not clear the Hugging Face token", nil)
	}
	if exists, ok := errors.AsType[*operation.BoardNameExistsError](err); ok {
		return fail(result.CodeInvalidRequest, exists.Error(), map[string]any{
			"reason": "board_name_exists", "board_ids": exists.BoardIDs,
		})
	}
	if exists, ok := errors.AsType[*operation.OutputExistsError](err); ok {
		return fail(result.CodeInvalidRequest, exists.Error(), map[string]any{
			"reason": "output_exists", "path": exists.Path,
		})
	}
	if failed, ok := errors.AsType[*operation.OutputWriteError](err); ok {
		return fail(result.CodeOutputWriteFailed, failed.Error(), map[string]any{"path": failed.Path})
	}
	if invalid, ok := errors.AsType[*operation.InvalidRequestError](err); ok {
		return fail(result.CodeInvalidRequest, invalid.Error(), invalidRequestDetails(err))
	}
	if invalid, ok := errors.AsType[*operation.InvalidInvokeAIVersionError](err); ok {
		if invalid.Version == "" {
			return fail(result.CodeInvalidVersionResponse, invalid.Error(), nil)
		}
		return fail(result.CodeInvalidInvokeAIVersion, invalid.Error(), map[string]any{"version": invalid.Version})
	}
	if unsupported, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); ok {
		return fail(result.CodeUnsupportedCapability, unsupported.Error(), nil)
	}
	if selection, ok := errors.AsType[*operation.CivitaiFileSelectionError](err); ok {
		return fail(result.CodeSelectionRequired, selection.Error(), map[string]any{
			"kind": "civitai_file", "selector": selection.VersionID, "candidates": selection.Candidates,
		})
	}
	if selection, ok := errors.AsType[*operation.CivitaiVersionSelectionError](err); ok {
		return fail(result.CodeSelectionRequired, selection.Error(), map[string]any{
			"kind": "civitai_version", "selector": selection.ModelID, "candidates": selection.Candidates,
		})
	}
	if selection, ok := errors.AsType[*operation.BoardSelectionError](err); ok {
		return fail(result.CodeSelectionRequired, selection.Error(), map[string]any{
			"kind": "board", "selector": selection.Selector, "candidates": selection.Candidates,
		})
	}
	if missing, ok := errors.AsType[*operation.NotFoundError](err); ok {
		return fail(result.CodeNotFound, missing.Error(), nil)
	}
	if selection, ok := errors.AsType[*operation.SelectionRequiredError](err); ok {
		return fail(result.CodeSelectionRequired, selection.Error(), map[string]any{
			"kind": selection.Kind, "selector": selection.Selector, "candidates": selection.Candidates,
		})
	}
	if missing, ok := errors.AsType[*operation.MissingComponentError](err); ok {
		return fail(result.CodeMissingComponent, missing.Error(), map[string]any{
			"component_type":        missing.ComponentType,
			"required_base":         missing.RequiredBase,
			"required_type":         missing.RequiredType,
			"installation_guidance": missing.InstallationGuidance,
		})
	}
	if failed, ok := errors.AsType[*upscale.ScaleNotAppliedError](err); ok {
		return fail(result.CodeInvokeAIOperationFailed, failed.Error(), map[string]any{
			"reason":          "scale_not_applied",
			"expected_width":  failed.ExpectedWidth,
			"expected_height": failed.ExpectedHeight,
			"actual_width":    failed.ActualWidth,
			"actual_height":   failed.ActualHeight,
			"queue_id":        failed.Queue.QueueID,
			"batch_id":        failed.Queue.BatchID,
			"item_ids":        failed.Queue.ItemIDs,
			"item_id":         failed.Output.ItemID,
			"seed":            failed.Output.Seed,
			"output_image":    failed.Output.Image,
			"source_image":    failed.SourceImage,
		})
	}
	if timeout, ok := errors.AsType[*operation.WaitTimeoutError](err); ok {
		return fail(result.CodeWaitTimeout, timeout.Error(), waitStoppedDetails(timeout.Position, timeout.PendingItemIDs))
	}
	if interrupted, ok := errors.AsType[*operation.InterruptedError](err); ok {
		return fail(result.CodeInterrupted, interrupted.Error(), waitStoppedDetails(interrupted.Position, interrupted.PendingItemIDs))
	}
	if failure, ok := errors.AsType[*operation.ItemFailureError](err); ok {
		return fail(result.CodeInvokeAIOperationFailed, failure.Error(), acceptedItemDetails(failure.Position, failure.ItemID, failure.Status))
	}
	if invalid, ok := errors.AsType[*operation.InvalidQueueResultError](err); ok {
		return fail(result.CodeInvalidInvokeAIResponse, invalid.Error(), acceptedItemDetails(invalid.Position, invalid.ItemID, invalid.Status))
	}
	if unknown, ok := errors.AsType[*httpclient.OutcomeUnknownError](err); ok {
		var details map[string]any
		if unknown.StatusCode != 0 {
			details = map[string]any{"status": unknown.StatusCode}
		}
		if operationName == result.OperationModelsInstall {
			return fail(result.CodeOutcomeUnknown, "InvokeAI may have accepted the installation; inspect the current model inventory and install job list before submitting again", details)
		}
		return fail(result.CodeOutcomeUnknown, "InvokeAI may have accepted the operation; inspect remote state before retrying", details)
	}
	if errors.Is(err, context.Canceled) {
		return fail(result.CodeInterrupted, "operation was interrupted locally", nil)
	}
	if _, ok := errors.AsType[*httpclient.NetworkError](err); ok {
		return fail(result.CodeConnectionFailed, "could not reach InvokeAI", nil)
	}
	if tooLarge, ok := errors.AsType[*httpclient.ResponseTooLargeError](err); ok {
		return fail(result.CodeInvalidInvokeAIResponse, "InvokeAI returned a response larger than the limit", map[string]any{
			"reason": "response_too_large", "limit_bytes": tooLarge.Limit,
		})
	}
	if _, ok := errors.AsType[*httpclient.InvalidResponseError](err); ok {
		return fail(result.CodeInvalidInvokeAIResponse, "InvokeAI returned an invalid response", nil)
	}
	if httpErr, ok := errors.AsType[*httpclient.HTTPError](err); ok {
		switch {
		case httpErr.AuthenticationFailure():
			return fail(result.CodeAuthenticationFailed, "InvokeAI rejected authentication", map[string]any{"status": httpErr.StatusCode})
		case httpErr.StatusCode == http.StatusNotFound:
			return fail(result.CodeNotFound, "the requested InvokeAI resource was not found", nil)
		default:
			details := map[string]any{"status": httpErr.StatusCode}
			if httpErr.Detail != nil {
				details["invokeai_detail"] = httpErr.Detail
			}
			return fail(result.CodeInvokeAIOperationFailed, "InvokeAI rejected the operation", details)
		}
	}
	return fail(result.CodeInvokeAIOperationFailed, err.Error(), nil)
}

// queuePositionDetails reports accepted remote work in the structured failure
// details of a wait-path error. queue wait observes items without a batch
// identity, so its details carry no batch_id.
func queuePositionDetails(position operation.QueuePosition) map[string]any {
	details := map[string]any{
		"queue_id": position.QueueID,
		"item_ids": position.ItemIDs,
	}
	if position.BatchID != "" {
		details["batch_id"] = position.BatchID
	}
	return details
}

// waitStoppedDetails adds the items that were not yet terminal when the wait
// tracked them.
func waitStoppedDetails(position operation.QueuePosition, pending []int) map[string]any {
	details := queuePositionDetails(position)
	if pending != nil {
		details["pending_item_ids"] = pending
	}
	return details
}

// acceptedItemDetails reports accepted remote work and the queue item that
// produced the failure.
func acceptedItemDetails(position operation.QueuePosition, itemID int, status string) map[string]any {
	details := queuePositionDetails(position)
	details["item_id"] = itemID
	details["status"] = status
	return details
}
