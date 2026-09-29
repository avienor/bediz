package structurederror_test

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/avienor/bediz/internal/profiles"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/structurederror"
)

func TestProfileFailures(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		err       error
		want      structurederror.Failure
	}{
		{
			name: "missing profile on get", operation: result.OperationProfilesGet,
			err: fmt.Errorf("read profile: %w", os.ErrNotExist),
			want: structurederror.Failure{Error: result.Error{
				Code: "not_found", Message: `profile "portrait" was not found`,
			}},
		},
		{
			name: "missing profile on delete", operation: result.OperationProfilesDelete,
			err:  fmt.Errorf("delete profile: %w", os.ErrNotExist),
			want: structurederror.Failure{Error: result.Error{Code: "not_found", Message: `profile "portrait" was not found`}},
		},
		{
			name: "invalid stored profile on get", operation: result.OperationProfilesGet, err: errors.New("invalid profile document"),
			want: structurederror.Failure{Error: result.Error{
				Code: "invalid_configuration", Message: "invalid profile document", Details: map[string]any{"name": "portrait"},
			}},
		},
		{
			name: "removal failed", operation: result.OperationProfilesDelete, err: errors.New("permission denied"),
			want: structurederror.Failure{Error: result.Error{
				Code: "configuration_write_failed", Message: "permission denied", Details: map[string]any{"name": "portrait"},
			}},
		},
		{
			name: "profile listing failed", operation: result.OperationProfilesList, err: errors.New("malformed stored profile"),
			want: structurederror.Failure{Error: result.Error{Code: "invalid_configuration", Message: "malformed stored profile"}},
		},
		{
			name: "profile already exists", operation: result.OperationProfilesCreate, err: &profiles.ExistsError{Name: "portrait"},
			want: structurederror.Failure{Error: result.Error{
				Code: "invalid_request", Message: `profile "portrait" already exists`, Details: map[string]any{"reason": "profile_exists", "name": "portrait"},
			}},
		},
		{
			name: "wrapped existing profile", operation: result.OperationProfilesCreate, err: fmt.Errorf("save: %w", &profiles.ExistsError{Name: "portrait"}),
			want: structurederror.Failure{Error: result.Error{
				Code: "invalid_request", Message: `save: profile "portrait" already exists`, Details: map[string]any{"reason": "profile_exists", "name": "portrait"},
			}},
		},
		{
			name: "profile write failed", operation: result.OperationProfilesCreate, err: errors.New("disk full"),
			want: structurederror.Failure{Error: result.Error{Code: "configuration_write_failed", Message: "disk full"}},
		},
		{
			name: "missing storage directory on create", operation: result.OperationProfilesCreate, err: fmt.Errorf("save: %w", os.ErrNotExist),
			want: structurederror.Failure{Error: result.Error{Code: "configuration_write_failed", Message: "save: file does not exist"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertFailure(t, structurederror.Profile(test.operation, "portrait", test.err), test.want)
		})
	}
}
