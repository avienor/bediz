package models

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
)

type ScanRequest struct {
	SchemaVersion int    `json:"schema_version"`
	Path          string `json:"path"`
}

type ScannedModel struct {
	Path      string `json:"path"`
	Installed bool   `json:"installed"`
}

type ScanResult struct {
	Models []ScannedModel `json:"models"`
}

func Scan(ctx context.Context, client *httpclient.Client, request ScanRequest) (ScanResult, error) {
	if request.SchemaVersion != 1 {
		return ScanResult{}, operation.InvalidRequest(fmt.Sprintf("unsupported request schema version %d", request.SchemaVersion))
	}
	if !isAbsoluteServerPath(request.Path) {
		return ScanResult{}, operation.InvalidRequest("path must be absolute in the InvokeAI server filesystem")
	}
	query := url.Values{"scan_path": {request.Path}}
	var response []struct {
		Path        string `json:"path"`
		IsInstalled *bool  `json:"is_installed"`
	}
	if err := client.GetJSON(ctx, "/api/v2/models/scan_folder?"+query.Encode(), &response); err != nil {
		return ScanResult{}, err
	}
	if response == nil {
		return ScanResult{}, &httpclient.InvalidResponseError{Err: errors.New("scan response must be an array")}
	}
	result := ScanResult{Models: make([]ScannedModel, 0, len(response))}
	for _, model := range response {
		if model.Path == "" || model.IsInstalled == nil {
			return ScanResult{}, &httpclient.InvalidResponseError{Err: errors.New("scan result is missing path or installed status")}
		}
		result.Models = append(result.Models, ScannedModel{Path: model.Path, Installed: *model.IsInstalled})
	}
	slices.SortFunc(result.Models, func(a, b ScannedModel) int { return cmp.Compare(a.Path, b.Path) })
	return result, nil
}
