package capability

import (
	"context"
	"fmt"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
)

// RequireSupportedVersion reads the InvokeAI version and requires it to be in
// the Supported Version Range. It classifies the answer as doctor does: an
// empty or unreadable version is an InvalidInvokeAIVersionError, and a
// readable one outside the range is unsupported_capability.
func RequireSupportedVersion(ctx context.Context, client *httpclient.Client) error {
	var response struct {
		Version string `json:"version"`
	}
	if err := client.GetJSON(ctx, "/api/v1/app/version", &response); err != nil {
		return err
	}
	if response.Version == "" {
		return &operation.InvalidInvokeAIVersionError{}
	}
	supported, err := SupportsInvokeAI(response.Version)
	if err != nil {
		return &operation.InvalidInvokeAIVersionError{Version: response.Version}
	}
	if !supported {
		return operation.UnsupportedCapability(fmt.Sprintf("InvokeAI %s is outside the supported range %s", response.Version, SupportedInvokeAIRange))
	}
	return nil
}
