package result

// Operation names identify Bediz commands and operations in the result
// envelope. They are part of the public contract: callers match on them.
const (
	OperationCLI           = "cli"
	OperationVersion       = "version"
	OperationDoctor        = "doctor"
	OperationGenerate      = "generate"
	OperationRecall        = "recall"
	OperationConfig        = "config"
	OperationConfigGet     = "config.get"
	OperationConfigSet     = "config.set"
	OperationModels        = "models"
	OperationModelsList    = "models.list"
	OperationModelsInstall = "models.install"
	OperationModelsStatus  = "models.status"
	OperationImages        = "images"
	OperationImagesList    = "images.list"
	OperationImagesGet     = "images.get"
	OperationImagesUpload  = "images.upload"
	OperationQueue         = "queue"
	OperationQueueList     = "queue.list"
	OperationQueueGet      = "queue.get"
)

// Structured error codes are part of the public contract: callers match on the
// code and never parse the message text.
const (
	CodeInvalidRequest           = "invalid_request"
	CodeInvalidConfiguration     = "invalid_configuration"
	CodeConfigurationWriteFailed = "configuration_write_failed"
	CodeOutputWriteFailed        = "output_write_failed"
	CodeUnsupportedCapability    = "unsupported_capability"
	CodeSelectionRequired        = "selection_required"
	CodeMissingComponent         = "missing_component"
	CodeConnectionFailed         = "connection_failed"
	CodeAuthenticationFailed     = "authentication_failed"
	CodeOutcomeUnknown           = "outcome_unknown"
	CodeWaitTimeout              = "wait_timeout"
	CodeNotFound                 = "not_found"
	CodeInvokeAIOperationFailed  = "invokeai_operation_failed"
	CodeInvokeAIHTTPError        = "invokeai_http_error"
	CodeInvalidInvokeAIResponse  = "invalid_invokeai_response"
	CodeInvalidVersionResponse   = "invalid_version_response"
	CodeInvalidInvokeAIVersion   = "invalid_invokeai_version"
	CodeInterrupted              = "interrupted"
)

// exitStatuses assigns each structured error code the process exit status
// defined by the V1 result contract. Local request and configuration failures
// are invalid requests, connection and authentication failures share one
// category, an interrupted operation reports its own status, and every other
// failure reports the InvokeAI failure category, including local output
// failures and indeterminate outcomes.
var exitStatuses = map[string]int{
	CodeInvalidRequest:           ExitInvalidRequest,
	CodeInvalidConfiguration:     ExitInvalidRequest,
	CodeConfigurationWriteFailed: ExitInvalidRequest,
	CodeUnsupportedCapability:    ExitUnsupportedCapability,
	CodeSelectionRequired:        ExitSelectionRequired,
	CodeMissingComponent:         ExitUnsupportedCapability,
	CodeConnectionFailed:         ExitConnection,
	CodeAuthenticationFailed:     ExitConnection,
	CodeInterrupted:              ExitInterrupted,
	CodeOutputWriteFailed:        ExitInvokeAIFailure,
	CodeOutcomeUnknown:           ExitInvokeAIFailure,
	CodeWaitTimeout:              ExitInvokeAIFailure,
	CodeNotFound:                 ExitInvokeAIFailure,
	CodeInvokeAIOperationFailed:  ExitInvokeAIFailure,
	CodeInvokeAIHTTPError:        ExitInvokeAIFailure,
	CodeInvalidInvokeAIResponse:  ExitInvokeAIFailure,
	CodeInvalidVersionResponse:   ExitInvokeAIFailure,
	CodeInvalidInvokeAIVersion:   ExitInvokeAIFailure,
}

// ExitStatus reports the process exit status for a structured error code. A
// code without an assignment reports the general InvokeAI failure category.
func ExitStatus(code string) int {
	status, ok := exitStatuses[code]
	if !ok {
		return ExitInvokeAIFailure
	}
	return status
}
