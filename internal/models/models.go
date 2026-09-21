package models

import (
	"context"
	"fmt"
	"net/url"
	"sort"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
)

type ListRequest struct {
	SchemaVersion int      `json:"schema_version"`
	BaseModels    []string `json:"base_models,omitempty"`
	ModelType     string   `json:"model_type,omitempty"`
	ModelFormat   string   `json:"model_format,omitempty"`
	ModelName     string   `json:"model_name,omitempty"`
}

type Summary struct {
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	Base        string  `json:"base"`
	Type        string  `json:"type"`
	Format      string  `json:"format"`
	SizeBytes   *int64  `json:"size_bytes,omitempty"`
	Description *string `json:"description,omitempty"`
}

type ListResult struct {
	Models []Summary `json:"models"`
}

type modelRecord struct {
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	Base        string  `json:"base"`
	Type        string  `json:"type"`
	Format      string  `json:"format"`
	FileSize    *int64  `json:"file_size"`
	Description *string `json:"description"`
}

type listResponse struct {
	Models []modelRecord `json:"models"`
}

func List(ctx context.Context, client *httpclient.Client, request ListRequest) (ListResult, error) {
	if request.SchemaVersion != 1 {
		return ListResult{}, operation.InvalidRequest(fmt.Sprintf("unsupported request schema version %d", request.SchemaVersion))
	}
	query := url.Values{}
	for _, base := range request.BaseModels {
		query.Add("base_models", base)
	}
	if request.ModelType != "" {
		query.Set("model_type", request.ModelType)
	}
	if request.ModelFormat != "" {
		query.Set("model_format", request.ModelFormat)
	}
	if request.ModelName != "" {
		query.Set("model_name", request.ModelName)
	}
	path := "/api/v2/models/"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var response listResponse
	if err := client.GetJSON(ctx, path, &response); err != nil {
		return ListResult{}, err
	}

	result := ListResult{Models: make([]Summary, 0, len(response.Models))}
	for _, model := range response.Models {
		result.Models = append(result.Models, Summary{
			Key:         model.Key,
			Name:        model.Name,
			Base:        model.Base,
			Type:        model.Type,
			Format:      model.Format,
			SizeBytes:   model.FileSize,
			Description: model.Description,
		})
	}
	sort.Slice(result.Models, func(i, j int) bool {
		if result.Models[i].Name == result.Models[j].Name {
			return result.Models[i].Key < result.Models[j].Key
		}
		return result.Models[i].Name < result.Models[j].Name
	})
	return result, nil
}
