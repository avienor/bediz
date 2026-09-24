package images

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
)

// DeleteRequest names exactly one image. Approved is execution approval and
// cannot be supplied in a Request Document.
type DeleteRequest struct {
	SchemaVersion int    `json:"schema_version"`
	ImageName     string `json:"image_name"`
	Approved      bool   `json:"-"`
}

type DeleteResult struct {
	ImageName string `json:"image_name"`
}

// Delete sends one deletion for an exact image name and never retries it.
func Delete(ctx context.Context, client *httpclient.Client, request DeleteRequest) (DeleteResult, error) {
	if request.SchemaVersion != 1 {
		return DeleteResult{}, operation.InvalidRequest(fmt.Sprintf("unsupported request schema version %d", request.SchemaVersion))
	}
	if request.ImageName == "" {
		return DeleteResult{}, operation.InvalidRequest("image name is required")
	}
	// A path-like selector could be cleaned by URL resolution into a different
	// image or an InvokeAI bulk-delete route before the response is checked.
	if request.ImageName == "." || request.ImageName == ".." || strings.ContainsAny(request.ImageName, "/\\%") {
		return DeleteResult{}, operation.InvalidRequest("image name must be one unescaped path segment other than . or ..")
	}
	if !request.Approved {
		return DeleteResult{}, operation.InvalidRequest("deleting an image requires --yes")
	}
	if err := capability.RequireSupportedVersion(ctx, client); err != nil {
		return DeleteResult{}, err
	}

	path := "/api/v1/images/i/" + url.PathEscape(request.ImageName)
	var response struct {
		DeletedImages []string `json:"deleted_images"`
		FailedImages  []string `json:"failed_images"`
	}
	if err := client.DoJSON(ctx, http.MethodDelete, path, nil, &response); err != nil {
		return DeleteResult{}, err
	}
	if len(response.DeletedImages) != 1 || response.DeletedImages[0] != request.ImageName || response.FailedImages == nil || len(response.FailedImages) != 0 {
		return DeleteResult{}, &httpclient.OutcomeUnknownError{
			Method: http.MethodDelete, URL: path,
			Err: errors.New("delete response did not confirm only the requested image"),
		}
	}
	return DeleteResult{ImageName: request.ImageName}, nil
}
