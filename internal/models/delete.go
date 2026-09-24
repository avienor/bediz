package models

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

// DeleteRequest names exactly one installed model by Model Key. Approved is
// execution approval and cannot be supplied in a Request Document.
type DeleteRequest struct {
	SchemaVersion int    `json:"schema_version"`
	ModelKey      string `json:"model_key"`
	Approved      bool   `json:"-"`
}

// DeleteResult summarizes the model as it was before deletion.
type DeleteResult struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Base   string `json:"base"`
	Type   string `json:"type"`
	Format string `json:"format"`
}

// Delete reads the model by exact key, then sends one deletion that is never
// retried. InvokeAI removes files only for models inside its managed models
// directory; an in-place registration leaves its source file.
func Delete(ctx context.Context, client *httpclient.Client, request DeleteRequest) (DeleteResult, error) {
	if request.SchemaVersion != 1 {
		return DeleteResult{}, operation.InvalidRequest(fmt.Sprintf("unsupported request schema version %d", request.SchemaVersion))
	}
	if request.ModelKey == "" {
		return DeleteResult{}, operation.InvalidRequest("model key is required")
	}
	// A path-like key could be cleaned by URL resolution into another model
	// route before InvokeAI answers.
	if request.ModelKey == "." || request.ModelKey == ".." || strings.ContainsAny(request.ModelKey, "/\\%") {
		return DeleteResult{}, operation.InvalidRequest("model key must be one unescaped path segment other than . or ..")
	}
	if !request.Approved {
		return DeleteResult{}, operation.InvalidRequest("deleting a model requires --yes")
	}
	if err := capability.RequireSupportedVersion(ctx, client); err != nil {
		return DeleteResult{}, err
	}

	path := "/api/v2/models/i/" + url.PathEscape(request.ModelKey)
	var record modelRecord
	if err := client.GetJSON(ctx, path, &record); err != nil {
		return DeleteResult{}, err
	}
	if record.Key != request.ModelKey {
		return DeleteResult{}, &httpclient.InvalidResponseError{Err: errors.New("model record does not match the requested model key")}
	}
	// The summary cannot be read again after deletion, so it must be complete first.
	if record.Name == "" || record.Base == "" || record.Type == "" || record.Format == "" {
		return DeleteResult{}, &httpclient.InvalidResponseError{Err: errors.New("model record is missing name, base, type, or format")}
	}
	if err := client.DoJSON(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{Key: record.Key, Name: record.Name, Base: record.Base, Type: record.Type, Format: record.Format}, nil
}
