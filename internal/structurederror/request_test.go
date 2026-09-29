package structurederror_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/avienor/bediz/internal/document"
	"github.com/avienor/bediz/internal/operation"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/structurederror"
)

func TestInvalidRequestFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want structurederror.Failure
	}{
		{
			name: "local request failure without a field", err: errors.New("request file could not be opened"),
			want: structurederror.Failure{Error: result.Error{Code: "invalid_request", Message: "request file could not be opened"}},
		},
		{
			name: "operation validation without a field", err: operation.InvalidRequest("model is required"),
			want: structurederror.Failure{Error: result.Error{Code: "invalid_request", Message: "model is required"}},
		},
		{
			name: "operation validation names its field", err: operation.InvalidField("scheduler", "unsupported scheduler"),
			want: structurederror.Failure{Error: result.Error{Code: "invalid_request", Message: "unsupported scheduler", Details: map[string]any{"field": "scheduler"}}},
		},
		{
			name: "document decoding names its field", err: &document.FieldError{Field: "components.vae", Err: errors.New("field cannot be null")},
			want: structurederror.Failure{Error: result.Error{Code: "invalid_request", Message: "field cannot be null", Details: map[string]any{"field": "components.vae"}}},
		},
		{
			name: "document decoding without field location", err: &document.FieldError{Err: errors.New("invalid JSON")},
			want: structurederror.Failure{Error: result.Error{Code: "invalid_request", Message: "invalid JSON"}},
		},
		{
			name: "wrapped document failure retains the input diagnostic",
			err:  fmt.Errorf("read request: %w", &document.FieldError{Field: "base_models.1", Err: errors.New("invalid base")}),
			want: structurederror.Failure{Error: result.Error{Code: "invalid_request", Message: "read request: invalid base", Details: map[string]any{"field": "base_models.1"}}},
		},
		{
			name: "operation field takes precedence over document field",
			err:  &document.FieldError{Field: "document", Err: operation.InvalidField("strength", "invalid strength")},
			want: structurederror.Failure{Error: result.Error{Code: "invalid_request", Message: "invalid strength", Details: map[string]any{"field": "strength"}}},
		},
		{
			name: "empty operation field falls through to document field",
			err:  &document.FieldError{Field: "model", Err: operation.InvalidRequest("model is required")},
			want: structurederror.Failure{Error: result.Error{Code: "invalid_request", Message: "model is required", Details: map[string]any{"field": "model"}}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertFailure(t, structurederror.InvalidRequest(test.err), test.want)
		})
	}
}
