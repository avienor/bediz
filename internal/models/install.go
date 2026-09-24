package models

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
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
	FileID    *int   `json:"file_id,omitempty"`
	Artifact  string `json:"artifact,omitempty"`
}

type InstallRequest struct {
	SchemaVersion int           `json:"schema_version"`
	Source        InstallSource `json:"source"`
	Move          *bool         `json:"move,omitempty"`
	Approved      bool          `json:"-"`
	SourceToken   string        `json:"-"`
}

type Installer struct {
	Backend                *httpclient.Client
	PublicRepositoryClient *http.Client
	CivitaiMetadataClient  *http.Client
}

type RepositoryAccessError struct{}

func (*RepositoryAccessError) Error() string {
	return "could not verify public Hugging Face repository access"
}

type InstallJob struct {
	JobID           int    `json:"job_id"`
	Status          string `json:"status"`
	SourceType      string `json:"source_type"`
	Role            string `json:"role"`
	DependencyIndex *int   `json:"dependency_index,omitempty"`
}

type InstallSkip struct {
	Role            string `json:"role"`
	DependencyIndex *int   `json:"dependency_index,omitempty"`
	Reason          string `json:"reason"`
}

type InstallResult struct {
	Jobs    []InstallJob   `json:"jobs"`
	Skipped *[]InstallSkip `json:"skipped,omitempty"`
}

type StarterSubmissionError struct {
	Progress        InstallResult
	Role            string
	DependencyIndex *int
	Cause           error
}

func (e *StarterSubmissionError) Error() string {
	return "starter installation submission did not complete"
}
func (e *StarterSubmissionError) Unwrap() error { return e.Cause }

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
var windowsServerPath = regexp.MustCompile(`^(?:[A-Za-z]:[\\/]|\\\\[^\\]+\\[^\\]+)`)

func isAbsoluteServerPath(value string) bool {
	return strings.HasPrefix(value, "/") || windowsServerPath.MatchString(value)
}

func Install(ctx context.Context, client *httpclient.Client, request InstallRequest) (InstallResult, error) {
	return (Installer{Backend: client}).Install(ctx, request)
}

func (installer Installer) Install(ctx context.Context, request InstallRequest) (InstallResult, error) {
	client := installer.Backend
	if request.SchemaVersion != 1 {
		return InstallResult{}, operation.InvalidRequest("unsupported request schema version")
	}
	if request.Move != nil && request.Source.Type != "path" {
		return InstallResult{}, operation.InvalidRequest("move applies only to a server path source")
	}
	if request.Source.FileID != nil && request.Source.Type != "civitai" {
		return InstallResult{}, operation.InvalidRequest("file_id applies only to a Civitai source")
	}
	if request.Source.Artifact != "" && request.Source.Type != "huggingface" {
		return InstallResult{}, operation.InvalidRequest("artifact applies only to a Hugging Face source")
	}
	if request.Source.Type == "path" {
		if request.SourceToken != "" {
			return InstallResult{}, operation.InvalidRequest("source token does not apply to a server path source")
		}
		if request.Move != nil && *request.Move && !request.Approved {
			return InstallResult{}, operation.InvalidRequest("moving a server model path requires --yes")
		}
	}
	if request.Source.Type == "starter" {
		return installer.installStarter(ctx, request)
	}
	if request.SourceToken != "" && !client.AllowsSourceToken() {
		return InstallResult{}, operation.InvalidRequest("source token requires HTTPS or a loopback InvokeAI target")
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
	case "path":
		if !isAbsoluteServerPath(source) {
			return InstallResult{}, operation.InvalidRequest("server path source must be absolute in the InvokeAI filesystem namespace")
		}
	case "civitai":
		var err error
		source, err = installer.resolveCivitai(ctx, request.Source, request.SourceToken)
		if err != nil {
			return InstallResult{}, err
		}
	default:
		return InstallResult{}, operation.InvalidRequest("source type must be starter, url, huggingface, path, or civitai")
	}
	if err := checkInstallCompatibility(ctx, client, request.SourceToken != "", request.Source.Type); err != nil {
		return InstallResult{}, err
	}
	if request.Source.Type == "huggingface" {
		var err error
		source, err = installer.preflightRepository(ctx, source, request.Source.Artifact, request.SourceToken)
		if err != nil {
			return InstallResult{}, err
		}
	}
	var inplace *bool
	if request.Source.Type == "path" {
		inplace = new(request.Move == nil || !*request.Move)
	}
	job, err := installer.submitJob(ctx, source, request.SourceToken, inplace)
	if err != nil {
		return InstallResult{}, err
	}
	return InstallResult{Jobs: []InstallJob{{JobID: *job.ID, Status: job.Status, SourceType: request.Source.Type, Role: "requested"}}}, nil
}

func (installer Installer) submitJob(ctx context.Context, source, sourceToken string, inplace *bool) (installBackendJob, error) {
	query := url.Values{"source": {source}}
	if inplace != nil {
		query.Set("inplace", strconv.FormatBool(*inplace))
	}
	if sourceToken != "" {
		query.Set("access_token", sourceToken)
	}
	var job installBackendJob
	if err := installer.Backend.DoJSONPrivate(ctx, http.MethodPost, "/api/v2/models/install?"+query.Encode(), map[string]any{}, &job); err != nil {
		return installBackendJob{}, err
	}
	if err := validBackendJob(job); err != nil {
		return installBackendJob{}, &httpclient.OutcomeUnknownError{Method: http.MethodPost, Err: err}
	}
	return job, nil
}

type starterEntry struct {
	Source       string         `json:"source"`
	IsInstalled  *bool          `json:"is_installed"`
	Dependencies []starterEntry `json:"dependencies"`
}

type starterInstallEntry struct {
	Source          string
	Role            string
	DependencyIndex *int
	Repository      bool
	SubfolderRepo   string
	SubfolderPath   string
	// ArtifactRepository is the canonical repository of an exact Hugging Face artifact entry.
	ArtifactRepository string
}

func (installer Installer) installStarter(ctx context.Context, request InstallRequest) (InstallResult, error) {
	client := installer.Backend
	if request.Source.Reference == "" {
		return InstallResult{}, operation.InvalidRequest("starter source reference is required")
	}
	if request.SourceToken != "" && !client.AllowsSourceToken() {
		return InstallResult{}, operation.InvalidRequest("source token requires HTTPS or a loopback InvokeAI target")
	}
	if err := checkInstallCompatibility(ctx, client, request.SourceToken != "", "starter"); err != nil {
		return InstallResult{}, err
	}
	var catalog struct {
		StarterModels []starterEntry `json:"starter_models"`
	}
	if err := client.GetJSON(ctx, "/api/v2/models/starter_models", &catalog); err != nil {
		return InstallResult{}, err
	}
	var selected *starterEntry
	for i := range catalog.StarterModels {
		if catalog.StarterModels[i].Source == request.Source.Reference {
			if selected != nil {
				return InstallResult{}, &httpclient.InvalidResponseError{Err: errors.New("starter catalog contains duplicate source identifiers")}
			}
			selected = &catalog.StarterModels[i]
		}
	}
	if selected == nil {
		return InstallResult{}, operation.InvalidRequest("starter source was not found in the InvokeAI catalog")
	}
	result := InstallResult{Jobs: []InstallJob{}, Skipped: new([]InstallSkip{})}
	toInstall := make([]starterInstallEntry, 0, len(selected.Dependencies)+1)
	addEntry := func(entry starterEntry, role string, index *int) error {
		if entry.IsInstalled == nil || entry.Source == "" {
			return &httpclient.InvalidResponseError{Err: errors.New("starter catalog entry has no source or installed state")}
		}
		if *entry.IsInstalled {
			*result.Skipped = append(*result.Skipped, InstallSkip{Role: role, DependencyIndex: index, Reason: "already_installed"})
			return nil
		}
		if strings.Contains(entry.Source, "::") {
			repository, subfolder, ok := starterSubfolderSource(entry.Source)
			if !ok {
				return operation.UnsupportedCapability("starter catalog entry has an unsupported Hugging Face subfolder source")
			}
			toInstall = append(toInstall, starterInstallEntry{Source: entry.Source, Role: role, DependencyIndex: index, SubfolderRepo: repository, SubfolderPath: subfolder})
			return nil
		}
		source, repoErr := normalizeHuggingFaceReference(entry.Source)
		isRepository := repoErr == nil
		if !isRepository {
			if err := validateArtifactURL(entry.Source); err != nil {
				return operation.UnsupportedCapability("starter catalog entry has an unsupported source reference")
			}
			source = entry.Source
			repository, ok := starterArtifactRepository(source)
			if !ok {
				return operation.UnsupportedCapability("starter catalog entry has an unsupported Hugging Face reference")
			}
			toInstall = append(toInstall, starterInstallEntry{Source: source, Role: role, DependencyIndex: index, ArtifactRepository: repository})
			return nil
		}
		toInstall = append(toInstall, starterInstallEntry{Source: source, Role: role, DependencyIndex: index, Repository: isRepository})
		return nil
	}
	for index, dependency := range selected.Dependencies {
		if err := addEntry(dependency, "dependency", new(index)); err != nil {
			return InstallResult{}, err
		}
	}
	if err := addEntry(*selected, "starter", nil); err != nil {
		return InstallResult{}, err
	}
	if request.SourceToken != "" {
		origin := ""
		for _, entry := range toInstall {
			parsed, err := url.Parse(entry.Source)
			if err != nil || parsed.Host == "" {
				return InstallResult{}, operation.UnsupportedCapability("starter source has no supported credential origin")
			}
			entryOrigin := strings.ToLower(parsed.Scheme + "://" + parsed.Host)
			if origin != "" && entryOrigin != origin {
				return InstallResult{}, operation.UnsupportedCapability("one source token cannot be sent to starter entries on different origins")
			}
			origin = entryOrigin
		}
	}
	if slices.ContainsFunc(toInstall, func(entry starterInstallEntry) bool { return entry.Repository }) {
		if err := checkInstallCompatibility(ctx, client, request.SourceToken != "", "huggingface"); err != nil {
			return InstallResult{}, err
		}
	}
	for _, entry := range toInstall {
		var err error
		switch {
		case entry.SubfolderRepo != "":
			err = installer.preflightStarterSubfolder(ctx, entry.SubfolderRepo, entry.SubfolderPath)
		case entry.Repository:
			_, err = installer.preflightRepository(ctx, entry.Source, "", request.SourceToken)
		case entry.ArtifactRepository != "":
			err = installer.checkRepositoryAccess(ctx, entry.ArtifactRepository, request.SourceToken, false)
		default:
			continue
		}
		if err != nil {
			if _, ok := errors.AsType[*operation.AuthenticationRequiredError](err); ok {
				return InstallResult{}, operation.UnsupportedCapability("starter catalog entry requires unavailable Hugging Face authentication inputs")
			}
			if _, ok := errors.AsType[*operation.SelectionRequiredError](err); ok {
				return InstallResult{}, operation.UnsupportedCapability("starter catalog entry does not identify one supported Hugging Face artifact")
			}
			if _, ok := errors.AsType[*operation.InvalidRequestError](err); ok {
				return InstallResult{}, operation.UnsupportedCapability("starter catalog entry has no supported Hugging Face artifact")
			}
			return InstallResult{}, err
		}
	}
	for _, entry := range toInstall {
		job, err := installer.submitJob(ctx, entry.Source, request.SourceToken, nil)
		if err != nil {
			return InstallResult{}, &StarterSubmissionError{Progress: result, Role: entry.Role, DependencyIndex: entry.DependencyIndex, Cause: err}
		}
		result.Jobs = append(result.Jobs, InstallJob{JobID: *job.ID, Status: job.Status, SourceType: "starter", Role: entry.Role, DependencyIndex: entry.DependencyIndex})
	}
	return result, nil
}

func starterSubfolderSource(source string) (string, string, bool) {
	repository, subfolder, found := strings.Cut(source, "::")
	if !found || !huggingFaceRepoID.MatchString(repository) || strings.Contains(repository, "..") || strings.Contains(repository, "--") || subfolder == "" || strings.HasPrefix(subfolder, "/") || strings.ContainsAny(subfolder, `\:+`) {
		return "", "", false
	}
	for segment := range strings.SplitSeq(subfolder, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", "", false
		}
	}
	return repository, subfolder, true
}

func (installer Installer) preflightStarterSubfolder(ctx context.Context, repository, subfolder string) error {
	if err := installer.checkStarterSubfolderExists(ctx, repository, subfolder); err != nil {
		return err
	}
	public, err := installer.publicRepository(ctx, repository)
	if err == nil && public {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	status, err := huggingface.Status(ctx, installer.Backend)
	if err != nil || status.Status != "valid" {
		return operation.UnsupportedCapability("starter catalog entry requires a valid InvokeAI Hugging Face login")
	}
	return nil
}

type huggingFaceTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

func (installer Installer) checkStarterSubfolderExists(ctx context.Context, repository, subfolder string) error {
	parent, _, found := strings.CutLast(subfolder, "/")
	if !found {
		parent = ""
	}
	entries, err := installer.huggingFaceTree(ctx, repository, parent, false, subfolder)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return operation.UnsupportedCapability("starter catalog subfolder could not be verified")
	}
	for _, entry := range entries {
		if entry.Path != subfolder {
			continue
		}
		switch entry.Type {
		case "file":
			return nil
		case "directory":
			files, err := installer.huggingFaceTree(ctx, repository, subfolder, true, "")
			if err != nil && ctx.Err() != nil {
				return ctx.Err()
			}
			if err == nil && slices.ContainsFunc(files, func(file huggingFaceTreeEntry) bool {
				return file.Type == "file" && strings.HasPrefix(file.Path, subfolder+"/")
			}) {
				return nil
			}
		}
		break
	}
	return operation.UnsupportedCapability("starter catalog subfolder could not be verified")
}

func (installer Installer) huggingFaceTree(ctx context.Context, repository, folder string, recursive bool, wanted string) ([]huggingFaceTreeEntry, error) {
	client := installer.PublicRepositoryClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	base := url.URL{Scheme: "https", Host: "huggingface.co", Path: "/api/models/" + repository + "/tree/main"}
	if folder != "" {
		base.Path += "/" + folder
	}
	if recursive {
		base.RawQuery = "recursive=true"
	}
	next := base.String()
	const maxTreePages = 100
	visited := make(map[string]bool)
	for next != "" {
		if visited[next] {
			return nil, errors.New("Hugging Face tree pagination cycle")
		}
		if len(visited) == maxTreePages {
			return nil, errors.New("Hugging Face tree pagination exceeds the page limit")
		}
		visited[next] = true
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			return nil, err
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return nil, errors.New("Hugging Face tree request was denied or unavailable")
		}
		const maxTreePage = 2 << 20
		body, err := io.ReadAll(io.LimitReader(response.Body, maxTreePage+1))
		response.Body.Close()
		if err != nil || len(body) > maxTreePage {
			return nil, errors.New("Hugging Face tree response is unreadable")
		}
		var entries []huggingFaceTreeEntry
		if err := json.Unmarshal(body, &entries); err != nil || entries == nil {
			return nil, errors.New("Hugging Face tree response is malformed")
		}
		found := false
		for _, entry := range entries {
			if entry.Path == "" || entry.Type == "" {
				return nil, errors.New("Hugging Face tree entry is malformed")
			}
			if entry.Path == wanted {
				found = true
			}
		}
		if recursive || found {
			return entries, nil
		}
		next, err = huggingFaceTreeNext(response.Header, &base)
		if err != nil {
			return nil, err
		}
	}
	return []huggingFaceTreeEntry{}, nil
}

func huggingFaceTreeNext(header http.Header, base *url.URL) (string, error) {
	for _, link := range header.Values("Link") {
		for part := range strings.SplitSeq(link, ",") {
			urlPart, attributes, found := strings.Cut(strings.TrimSpace(part), ";")
			if !found || !strings.Contains(attributes, `rel="next"`) || !strings.HasPrefix(urlPart, "<") || !strings.HasSuffix(urlPart, ">") {
				continue
			}
			next, err := url.Parse(strings.TrimSuffix(strings.TrimPrefix(urlPart, "<"), ">"))
			if err != nil {
				return "", err
			}
			next = base.ResolveReference(next)
			if next.Scheme != "https" || next.Host != "huggingface.co" || next.User != nil || next.Fragment != "" || next.Path != base.Path || next.Query().Get("cursor") == "" {
				return "", errors.New("Hugging Face tree pagination link is invalid")
			}
			return next.String(), nil
		}
	}
	return "", nil
}

// starterArtifactRepository validates a starter artifact source. For an exact
// Hugging Face artifact it also returns the canonical repository URL whose
// access governs the download; other origins return an empty repository.
func starterArtifactRepository(source string) (string, bool) {
	parsed, err := url.Parse(source)
	if err != nil || !strings.EqualFold(parsed.Hostname(), "huggingface.co") {
		return "", true
	}
	if parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "huggingface.co") || path.Clean(parsed.Path) != parsed.Path || strings.Contains(strings.ToLower(parsed.EscapedPath()), "%2f") {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
	if len(parts) < 5 || parts[2] != "resolve" {
		return "", false
	}
	for _, part := range parts {
		if part == "" {
			return "", false
		}
	}
	repository, err := normalizeHuggingFaceReference(parts[0] + "/" + parts[1])
	if err != nil {
		return "", false
	}
	return repository, true
}

// preflightRepository returns the source to submit: the canonical repository,
// or the selected artifact when it is one of the repository's candidates.
func (installer Installer) preflightRepository(ctx context.Context, canonical, artifact, sourceToken string) (string, error) {
	if err := installer.checkRepositoryAccess(ctx, canonical, sourceToken, true); err != nil {
		return "", err
	}
	return checkHuggingFaceRepository(ctx, installer.Backend, canonical, artifact)
}

// checkRepositoryAccess requires a download token for a repository that is not
// verifiably public. needsLogin additionally requires a valid InvokeAI login,
// which InvokeAI uses for protected repository metadata.
func (installer Installer) checkRepositoryAccess(ctx context.Context, canonical, sourceToken string, needsLogin bool) error {
	public, err := installer.publicRepository(ctx, strings.TrimPrefix(canonical, "https://huggingface.co/"))
	if err != nil {
		return err
	}
	if public {
		return nil
	}
	if sourceToken == "" {
		if needsLogin {
			return &operation.AuthenticationRequiredError{Message: "protected Hugging Face repository requires --token-stdin and a valid InvokeAI Hugging Face login"}
		}
		return &operation.AuthenticationRequiredError{Message: "protected Hugging Face artifact requires --token-stdin"}
	}
	if !needsLogin {
		return nil
	}
	status, err := huggingface.Status(ctx, installer.Backend)
	if err != nil {
		return err
	}
	if status.Status != "valid" {
		return &operation.AuthenticationRequiredError{Message: "protected Hugging Face repository requires a valid InvokeAI Hugging Face login"}
	}
	return nil
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

func checkHuggingFaceRepository(ctx context.Context, client *httpclient.Client, canonical, artifact string) (string, error) {
	id := strings.TrimPrefix(canonical, "https://huggingface.co/")
	var metadata struct {
		URLs        []string `json:"urls"`
		IsDiffusers bool     `json:"is_diffusers"`
	}
	if err := client.GetJSON(ctx, "/api/v2/models/hugging_face?"+url.Values{"hugging_face_repo": {id}}.Encode(), &metadata); err != nil {
		return "", err
	}
	candidates := make([]operation.SelectionCandidate, 0, len(metadata.URLs))
	for _, artifact := range metadata.URLs {
		if !strings.HasPrefix(artifact, canonical+"/resolve/") || validateArtifactURL(artifact) != nil {
			return "", &httpclient.InvalidResponseError{Err: errors.New("Hugging Face metadata contains an unsafe artifact URL")}
		}
		parsed, _ := url.Parse(artifact)
		if path.Clean(parsed.Path) != parsed.Path || strings.Contains(strings.ToLower(parsed.EscapedPath()), "%2f") {
			return "", &httpclient.InvalidResponseError{Err: errors.New("Hugging Face metadata contains an unsafe artifact URL")}
		}
		candidates = append(candidates, operation.SelectionCandidate{Key: artifact, Name: path.Base(parsed.Path)})
	}
	if len(metadata.URLs) == 0 && !metadata.IsDiffusers {
		return "", operation.InvalidRequest("Hugging Face repository contains no supported model artifact")
	}
	if artifact != "" {
		if metadata.IsDiffusers || !slices.Contains(metadata.URLs, artifact) {
			return "", operation.InvalidRequest("artifact is not a candidate of the Hugging Face repository")
		}
		return artifact, nil
	}
	if !metadata.IsDiffusers && len(metadata.URLs) > 1 {
		slices.SortFunc(candidates, func(a, b operation.SelectionCandidate) int { return cmp.Compare(a.Key, b.Key) })
		return "", operation.SelectionRequired("huggingface_artifact", canonical, candidates)
	}
	return canonical, nil
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

func checkInstallCompatibility(ctx context.Context, client *httpclient.Client, hasSourceToken bool, sourceType string) error {
	if err := capability.RequireSupportedVersion(ctx, client); err != nil {
		return err
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
	if sourceType == "huggingface" {
		if _, ok := document.Paths["/api/v2/models/hugging_face"]["get"]; !ok {
			return operation.UnsupportedCapability("InvokeAI Hugging Face repository metadata endpoint is unavailable")
		}
	}
	if sourceType == "starter" && !document.Paths["/api/v2/models/starter_models"]["get"].HasStarterCatalogResponse() {
		return operation.UnsupportedCapability("InvokeAI starter model catalog endpoint is unavailable")
	}
	if hasSourceToken && !post.HasAccessTokenQuery() {
		return operation.UnsupportedCapability("InvokeAI generic model installation access token parameter is unavailable")
	}
	if sourceType == "path" && !post.HasInplaceQuery() {
		return operation.UnsupportedCapability("InvokeAI generic model installation in-place parameter is unavailable")
	}
	return nil
}
