package structurederror

import (
	"errors"
	"fmt"
	"os"

	"github.com/avienor/bediz/internal/profiles"
	"github.com/avienor/bediz/internal/result"
)

// Profile maps a non-nil local profile-storage failure. The operation and name
// distinguish read failures from write failures without command-parser state.
func Profile(operationName, name string, err error) Failure {
	failure := Failure{Error: result.Error{Code: result.CodeInvalidConfiguration, Message: err.Error()}}
	switch operationName {
	case result.OperationProfilesCreate:
		if existing, ok := errors.AsType[*profiles.ExistsError](err); ok {
			failure.Error.Code = result.CodeInvalidRequest
			failure.Error.Details = map[string]any{"reason": "profile_exists", "name": existing.Name}
		} else {
			failure.Error.Code = result.CodeConfigurationWriteFailed
		}
	case result.OperationProfilesGet, result.OperationProfilesDelete:
		if errors.Is(err, os.ErrNotExist) {
			failure.Error.Code = result.CodeNotFound
			failure.Error.Message = fmt.Sprintf("profile %q was not found", name)
		} else {
			failure.Error.Details = map[string]any{"name": name}
			if operationName == result.OperationProfilesDelete {
				failure.Error.Code = result.CodeConfigurationWriteFailed
			}
		}
	}
	return failure
}
