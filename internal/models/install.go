package models

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/huggingface"
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

type Installer struct {
	Backend                *httpclient.Client
	PublicRepositoryClient *http.Client
}

type RepositoryAccessError struct{}

func (*RepositoryAccessError) Error() string {
	return "could not verify public Hugging Face repository access"
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

var huggingFaceRepoID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*$`)

func Install(ctx context.Context, client *httpclient.Client, request InstallRequest) (InstallResult, error) {
	return (Installer{Backend: client}).Install(ctx, request)
}

func (installer Installer) Install(ctx context.Context, request InstallRequest) (InstallResult, error) {
	client := installer.Backend
	if request.SchemaVersion != 1 {
		return InstallResult{}, operation.InvalidRequest("unsupported request schema version")
	}
	source := request.Source.Reference
	switch request.Source.Type {
	case "url":
		if err := validateArtifactURL(source); err != nil {
			return InstallResult{}, err
		}
	case "huggingface":
		var err error
		source, err = normalizeHuggingFaceReference(source)
		if err != nil {
			return InstallResult{}, err
		}
	default:
		return InstallResult{}, operation.InvalidRequest("source type must be url or huggingface")
	}
	if request.SourceToken != "" && !client.AllowsSourceToken() {
		return InstallResult{}, operation.InvalidRequest("source token requires HTTPS or a loopback InvokeAI target")
	}
	if err := checkInstallCompatibility(ctx, client, request.SourceToken != "", request.Source.Type == "huggingface"); err != nil {
		return InstallResult{}, err
	}
	if request.Source.Type == "huggingface" {
		public, err := installer.publicRepository(ctx, strings.TrimPrefix(source, "https://huggingface.co/"))
		if err != nil {
			return InstallResult{}, err
		}
		if !public && request.SourceToken == "" {
			return InstallResult{}, &operation.AuthenticationRequiredError{Message: "protected Hugging Face repository requires --token-stdin and a valid InvokeAI Hugging Face login"}
		}
		if !public {
			status, err := huggingface.Status(ctx, client)
			if err != nil {
				return InstallResult{}, err
			}
			if status.Status != "valid" {
				return InstallResult{}, &operation.AuthenticationRequiredError{Message: "protected Hugging Face repository requires a valid InvokeAI Hugging Face login"}
			}
		}
		if err := checkHuggingFaceRepository(ctx, client, source); err != nil {
			return InstallResult{}, err
		}
	}
	query := url.Values{"source": {source}}
	if request.SourceToken != "" {
		query.Set("access_token", request.SourceToken)
	}
	var job installBackendJob
	path := "/api/v2/models/install?" + query.Encode()
	if err := client.DoJSONPrivate(ctx, http.MethodPost, path, map[string]any{}, &job); err != nil {
		return InstallResult{}, err
	}
	if err := validBackendJob(job); err != nil {
		return InstallResult{}, &httpclient.OutcomeUnknownError{Method: http.MethodPost, Err: err}
	}
	return InstallResult{Jobs: []InstallJob{{JobID: *job.ID, Status: job.Status, SourceType: request.Source.Type, Role: "requested"}}}, nil
}

func (installer Installer) publicRepository(ctx context.Context, id string) (bool, error) {
	client := installer.PublicRepositoryClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://huggingface.co/api/models/"+id, nil)
	if err != nil {
		return false, &RepositoryAccessError{}
	}
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return false, err
		}
		return false, &RepositoryAccessError{}
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
		const maxMetadata = 2 << 20
		body, err := io.ReadAll(io.LimitReader(response.Body, maxMetadata+1))
		if err != nil || len(body) > maxMetadata {
			return false, &RepositoryAccessError{}
		}
		var metadata struct {
			Gated   jsontext.Value `json:"gated"`
			Private *bool          `json:"private"`
		}
		if err := json.Unmarshal(body, &metadata); err != nil {
			return false, &RepositoryAccessError{}
		}
		if metadata.Private == nil {
			return false, &RepositoryAccessError{}
		}
		switch string(metadata.Gated) {
		case "false":
			return !*metadata.Private, nil
		case "true", `"manual"`, `"auto"`:
			return false, nil
		default:
			return false, &RepositoryAccessError{}
		}
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return false, nil
	default:
		return false, &RepositoryAccessError{}
	}
}

func normalizeHuggingFaceReference(reference string) (string, error) {
	id := reference
	if strings.HasPrefix(id, "https://huggingface.co/") {
		id = strings.TrimSuffix(strings.TrimPrefix(id, "https://huggingface.co/"), "/")
	}
	if !huggingFaceRepoID.MatchString(id) || strings.Contains(id, "..") || strings.Contains(id, "--") {
		return "", operation.InvalidRequest("Hugging Face source must be a plain org/repo ID or canonical HTTPS repository URL")
	}
	return "https://huggingface.co/" + id, nil
}

func checkHuggingFaceRepository(ctx context.Context, client *httpclient.Client, canonical string) error {
	id := strings.TrimPrefix(canonical, "https://huggingface.co/")
	var metadata struct {
		URLs        []string `json:"urls"`
		IsDiffusers bool     `json:"is_diffusers"`
	}
	if err := client.GetJSON(ctx, "/api/v2/models/hugging_face?"+url.Values{"hugging_face_repo": {id}}.Encode(), &metadata); err != nil {
		return err
	}
	candidates := make([]operation.SelectionCandidate, 0, len(metadata.URLs))
	for _, artifact := range metadata.URLs {
		if !strings.HasPrefix(artifact, canonical+"/resolve/") || validateArtifactURL(artifact) != nil {
			return &httpclient.InvalidResponseError{Err: errors.New("Hugging Face metadata contains an unsafe artifact URL")}
		}
		parsed, _ := url.Parse(artifact)
		if path.Clean(parsed.Path) != parsed.Path || strings.Contains(strings.ToLower(parsed.EscapedPath()), "%2f") {
			return &httpclient.InvalidResponseError{Err: errors.New("Hugging Face metadata contains an unsafe artifact URL")}
		}
		candidates = append(candidates, operation.SelectionCandidate{Key: artifact, Name: path.Base(parsed.Path)})
	}
	if len(metadata.URLs) == 0 && !metadata.IsDiffusers {
		return operation.InvalidRequest("Hugging Face repository contains no supported model artifact")
	}
	if !metadata.IsDiffusers && len(metadata.URLs) > 1 {
		slices.SortFunc(candidates, func(a, b operation.SelectionCandidate) int { return cmp.Compare(a.Key, b.Key) })
		return operation.SelectionRequired("huggingface_artifact", canonical, candidates)
	}
	return nil
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

func checkInstallCompatibility(ctx context.Context, client *httpclient.Client, hasSourceToken, huggingFaceSource bool) error {
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
	if !post.HasJobResponse() {
		return operation.UnsupportedCapability("InvokeAI generic model installation job response is unavailable")
	}
	if huggingFaceSource {
		if _, ok := document.Paths["/api/v2/models/hugging_face"]["get"]; !ok {
			return operation.UnsupportedCapability("InvokeAI Hugging Face repository metadata endpoint is unavailable")
		}
	}
	if hasSourceToken && !post.HasAccessTokenQuery() {
		return operation.UnsupportedCapability("InvokeAI generic model installation access token parameter is unavailable")
	}
	return nil
}
