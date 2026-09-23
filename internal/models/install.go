package models

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
)

type InstallSource struct {
	Type      string `json:"type"`
	Reference string `json:"reference"`
}

type InstallRequest struct {
	SchemaVersion int           `json:"schema_version"`
	Source        InstallSource `json:"source"`
	SourceToken   string        `json:"-"`
}

type InstallJob struct {
	JobID      int    `json:"job_id"`
	Status     string `json:"status"`
	SourceType string `json:"source_type"`
	Role       string `json:"role"`
}

type InstallResult struct {
	Jobs []InstallJob `json:"jobs"`
}

type StatusRequest struct {
	SchemaVersion int  `json:"schema_version"`
	JobID         *int `json:"job_id"`
}

type StatusResult struct {
	JobID      int    `json:"job_id"`
	Status     string `json:"status"`
	Bytes      *int64 `json:"bytes,omitempty"`
	TotalBytes *int64 `json:"total_bytes,omitempty"`
	ModelKey   string `json:"model_key,omitempty"`
}

type installBackendJob struct {
	ID         *int   `json:"id"`
	Status     string `json:"status"`
	Bytes      *int64 `json:"bytes"`
	TotalBytes *int64 `json:"total_bytes"`
	ConfigOut  *struct {
		Key string `json:"key"`
	} `json:"config_out"`
}

func Install(ctx context.Context, client *httpclient.Client, request InstallRequest) (InstallResult, error) {
	if request.SchemaVersion != 1 {
		return InstallResult{}, operation.InvalidRequest("unsupported request schema version")
	}
	if request.Source.Type != "url" {
		return InstallResult{}, operation.InvalidRequest("source type must be url")
	}
	if err := validateArtifactURL(request.Source.Reference); err != nil {
		return InstallResult{}, err
	}
	if request.SourceToken != "" && !client.AllowsSourceToken() {
		return InstallResult{}, operation.InvalidRequest("source token requires HTTPS or a loopback InvokeAI target")
	}
	if err := checkInstallCompatibility(ctx, client, request.SourceToken != ""); err != nil {
		return InstallResult{}, err
	}
	query := url.Values{"source": {request.Source.Reference}}
	if request.SourceToken != "" {
		query.Set("access_token", request.SourceToken)
	}
	var job installBackendJob
	path := "/api/v2/models/install?" + query.Encode()
	var err error
	if request.SourceToken != "" {
		err = client.DoJSONPrivate(ctx, http.MethodPost, path, map[string]any{}, &job)
	} else {
		err = client.DoJSON(ctx, http.MethodPost, path, map[string]any{}, &job)
	}
	if err != nil {
		return InstallResult{}, err
	}
	if err := validBackendJob(job); err != nil {
		return InstallResult{}, &httpclient.OutcomeUnknownError{Method: http.MethodPost, Err: err}
	}
	return InstallResult{Jobs: []InstallJob{{JobID: *job.ID, Status: job.Status, SourceType: "url", Role: "requested"}}}, nil
}

func Status(ctx context.Context, client *httpclient.Client, request StatusRequest) (StatusResult, error) {
	if request.SchemaVersion != 1 {
		return StatusResult{}, operation.InvalidRequest("unsupported request schema version")
	}
	if request.JobID == nil || *request.JobID < 0 {
		return StatusResult{}, operation.InvalidRequest("job_id must be a non-negative integer")
	}
	var job installBackendJob
	if err := client.GetJSON(ctx, "/api/v2/models/install/"+strconv.Itoa(*request.JobID), &job); err != nil {
		return StatusResult{}, err
	}
	if err := validBackendJob(job); err != nil {
		return StatusResult{}, &httpclient.InvalidResponseError{Err: err}
	}
	if *job.ID != *request.JobID {
		return StatusResult{}, &httpclient.InvalidResponseError{Err: errors.New("install job ID differs from requested ID")}
	}
	result := StatusResult{JobID: *job.ID, Status: job.Status, Bytes: job.Bytes, TotalBytes: job.TotalBytes}
	if job.Status == "completed" && job.ConfigOut != nil {
		result.ModelKey = job.ConfigOut.Key
	}
	return result, nil
}

func validateArtifactURL(reference string) error {
	parsed, err := url.Parse(reference)
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.Opaque != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || strings.Contains(reference, "#") {
		return operation.InvalidRequest("source reference must be an exact HTTP(S) artifact URL without credentials, query, or fragment")
	}
	return nil
}

func validBackendJob(job installBackendJob) error {
	if job.ID == nil || *job.ID < 0 {
		return errors.New("install job has no valid ID")
	}
	switch job.Status {
	case "waiting", "downloading", "downloads_done", "running", "paused", "completed", "error", "cancelled":
		return nil
	default:
		return errors.New("install job has an unknown status")
	}
}

func checkInstallCompatibility(ctx context.Context, client *httpclient.Client, hasSourceToken bool) error {
	var version struct {
		Version string `json:"version"`
	}
	if err := client.GetJSON(ctx, "/api/v1/app/version", &version); err != nil {
		return err
	}
	supported, err := capability.SupportsInvokeAI(version.Version)
	if err != nil {
		return &httpclient.InvalidResponseError{Err: fmt.Errorf("invalid InvokeAI version: %w", err)}
	}
	if !supported {
		return operation.UnsupportedCapability("InvokeAI version is outside the tested model installation range")
	}
	var document struct {
		Paths map[string]map[string]capability.InstallEndpoint `json:"paths"`
	}
	if err := client.GetJSON(ctx, "/openapi.json", &document); err != nil {
		return err
	}
	post, ok := document.Paths["/api/v2/models/install"]["post"]
	if !ok {
		return operation.UnsupportedCapability("InvokeAI generic model installation endpoint is unavailable")
	}
	if !post.HasRequiredSource() {
		return operation.UnsupportedCapability("InvokeAI generic model installation source parameter is unavailable")
	}
	if hasSourceToken && !post.HasAccessTokenQuery() {
		return operation.UnsupportedCapability("InvokeAI generic model installation access token parameter is unavailable")
	}
	return nil
}
