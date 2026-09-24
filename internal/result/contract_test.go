package result

import "testing"

// Operation names, structured error codes, and process exit statuses are the
// public contract of the result envelope. These tables pin their published
// values, so a changed constant is a visible contract change rather than a
// silently renamed identifier.
func TestOperationNamesMatchThePublishedValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
		wire  string
	}{
		{name: "cli", value: OperationCLI, wire: "cli"},
		{name: "version", value: OperationVersion, wire: "version"},
		{name: "doctor", value: OperationDoctor, wire: "doctor"},
		{name: "generate", value: OperationGenerate, wire: "generate"},
		{name: "config", value: OperationConfig, wire: "config"},
		{name: "config get", value: OperationConfigGet, wire: "config.get"},
		{name: "config set", value: OperationConfigSet, wire: "config.set"},
		{name: "models", value: OperationModels, wire: "models"},
		{name: "models list", value: OperationModelsList, wire: "models.list"},
		{name: "models install", value: OperationModelsInstall, wire: "models.install"},
		{name: "models status", value: OperationModelsStatus, wire: "models.status"},
		{name: "images", value: OperationImages, wire: "images"},
		{name: "images list", value: OperationImagesList, wire: "images.list"},
		{name: "images get", value: OperationImagesGet, wire: "images.get"},
		{name: "images upload", value: OperationImagesUpload, wire: "images.upload"},
		{name: "images download", value: OperationImagesDownload, wire: "images.download"},
		{name: "queue", value: OperationQueue, wire: "queue"},
		{name: "queue list", value: OperationQueueList, wire: "queue.list"},
		{name: "queue get", value: OperationQueueGet, wire: "queue.get"},
		{name: "queue wait", value: OperationQueueWait, wire: "queue.wait"},
		{name: "queue cancel", value: OperationQueueCancel, wire: "queue.cancel"},
		{name: "queue clear", value: OperationQueueClear, wire: "queue.clear"},
		{name: "auth", value: OperationAuth, wire: "auth"},
		{name: "auth huggingface", value: OperationAuthHuggingFace, wire: "auth.huggingface"},
		{name: "auth huggingface status", value: OperationAuthHFStatus, wire: "auth.huggingface.status"},
		{name: "auth huggingface login", value: OperationAuthHFLogin, wire: "auth.huggingface.login"},
		{name: "auth huggingface logout", value: OperationAuthHFLogout, wire: "auth.huggingface.logout"},
	}
	for _, test := range tests {
		if test.value != test.wire {
			t.Errorf("%s operation name = %q, want %q", test.name, test.value, test.wire)
		}
	}
}

func TestErrorCodesReportTheirPublishedExitStatus(t *testing.T) {
	tests := []struct {
		name   string
		code   string
		wire   string
		status int
	}{
		{name: "invalid request", code: CodeInvalidRequest, wire: "invalid_request", status: ExitInvalidRequest},
		{name: "invalid configuration", code: CodeInvalidConfiguration, wire: "invalid_configuration", status: ExitInvalidRequest},
		{name: "configuration write failed", code: CodeConfigurationWriteFailed, wire: "configuration_write_failed", status: ExitInvalidRequest},
		{name: "unsupported capability", code: CodeUnsupportedCapability, wire: "unsupported_capability", status: ExitUnsupportedCapability},
		{name: "selection required", code: CodeSelectionRequired, wire: "selection_required", status: ExitSelectionRequired},
		{name: "missing component", code: CodeMissingComponent, wire: "missing_component", status: ExitUnsupportedCapability},
		{name: "connection failed", code: CodeConnectionFailed, wire: "connection_failed", status: ExitConnection},
		{name: "authentication failed", code: CodeAuthenticationFailed, wire: "authentication_failed", status: ExitConnection},
		{name: "interrupted", code: CodeInterrupted, wire: "interrupted", status: ExitInterrupted},
		{name: "output write failed", code: CodeOutputWriteFailed, wire: "output_write_failed", status: ExitInvokeAIFailure},
		{name: "outcome unknown", code: CodeOutcomeUnknown, wire: "outcome_unknown", status: ExitInvokeAIFailure},
		{name: "wait timeout", code: CodeWaitTimeout, wire: "wait_timeout", status: ExitInvokeAIFailure},
		{name: "not found", code: CodeNotFound, wire: "not_found", status: ExitInvokeAIFailure},
		{name: "InvokeAI operation failed", code: CodeInvokeAIOperationFailed, wire: "invokeai_operation_failed", status: ExitInvokeAIFailure},
		{name: "InvokeAI HTTP error", code: CodeInvokeAIHTTPError, wire: "invokeai_http_error", status: ExitInvokeAIFailure},
		{name: "invalid InvokeAI response", code: CodeInvalidInvokeAIResponse, wire: "invalid_invokeai_response", status: ExitInvokeAIFailure},
		{name: "invalid version response", code: CodeInvalidVersionResponse, wire: "invalid_version_response", status: ExitInvokeAIFailure},
		{name: "invalid InvokeAI version", code: CodeInvalidInvokeAIVersion, wire: "invalid_invokeai_version", status: ExitInvokeAIFailure},
	}
	pinned := make(map[string]bool, len(tests))
	for _, test := range tests {
		pinned[test.code] = true
		if test.code != test.wire {
			t.Errorf("%s error code = %q, want %q", test.name, test.code, test.wire)
		}
		if status := ExitStatus(test.code); status != test.status {
			t.Errorf("ExitStatus(%q) = %d, want %d", test.code, status, test.status)
		}
	}
	for code := range exitStatuses {
		if !pinned[code] {
			t.Errorf("exit status for %q is not pinned by this test", code)
		}
	}
}

func TestExitStatusConstantsMatchTheV1Contract(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   int
	}{
		{name: "success", status: ExitSuccess, want: 0},
		{name: "invalid request", status: ExitInvalidRequest, want: 2},
		{name: "selection required", status: ExitSelectionRequired, want: 3},
		{name: "unsupported capability", status: ExitUnsupportedCapability, want: 4},
		{name: "connection", status: ExitConnection, want: 5},
		{name: "InvokeAI failure", status: ExitInvokeAIFailure, want: 6},
		{name: "interrupted", status: ExitInterrupted, want: 130},
	}
	for _, test := range tests {
		if test.status != test.want {
			t.Errorf("%s exit status = %d, want %d", test.name, test.status, test.want)
		}
	}
}
