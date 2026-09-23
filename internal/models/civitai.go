package models

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/avienor/bediz/internal/operation"
)

type civitaiVersion struct {
	ID          *int   `json:"id"`
	ModelID     *int   `json:"modelId"`
	DownloadURL string `json:"downloadUrl"`
	Files       []struct {
		ID          *int   `json:"id"`
		Name        string `json:"name"`
		Primary     *bool  `json:"primary"`
		DownloadURL string `json:"downloadUrl"`
	} `json:"files"`
}

type civitaiModel struct {
	ID            *int `json:"id"`
	ModelVersions []struct {
		ID   *int   `json:"id"`
		Name string `json:"name"`
	} `json:"modelVersions"`
}

type CivitaiMetadataAccessError struct {
	StatusCode int
}

func (*CivitaiMetadataAccessError) Error() string {
	return "Civitai metadata could not be verified"
}

func (installer Installer) resolveCivitai(ctx context.Context, source InstallSource, token string) (string, error) {
	versionID, modelID, err := parseCivitaiReference(source.Reference)
	if err != nil {
		return "", err
	}
	if source.FileID != nil && *source.FileID <= 0 {
		return "", operation.InvalidRequest("file_id must be a positive integer")
	}
	if versionID == 0 {
		if source.FileID != nil {
			return "", operation.InvalidRequest("file_id requires an exact Civitai version")
		}
		return "", installer.civitaiVersionChoices(ctx, modelID, token)
	}
	body, err := installer.civitaiMetadata(ctx, "https://civitai.com/api/v1/model-versions/"+strconv.Itoa(versionID), token)
	if err != nil {
		return "", err
	}
	var metadata civitaiVersion
	if err := json.Unmarshal(body, &metadata); err != nil || metadata.ID == nil || *metadata.ID != versionID || len(metadata.Files) == 0 || (modelID != 0 && (metadata.ModelID == nil || *metadata.ModelID != modelID)) {
		return "", operation.InvalidRequest("Civitai version metadata is invalid")
	}
	baseURL := "https://civitai.com/api/download/models/" + strconv.Itoa(versionID)
	if metadata.DownloadURL != "" && metadata.DownloadURL != baseURL {
		return "", operation.InvalidRequest("Civitai version metadata requires an unsupported download URL")
	}
	candidates := make([]operation.CivitaiFileCandidate, 0, len(metadata.Files))
	seen := make(map[int]bool, len(metadata.Files))
	for _, file := range metadata.Files {
		if file.ID == nil || *file.ID <= 0 || file.Name == "" || file.Primary == nil || seen[*file.ID] {
			return "", operation.InvalidRequest("Civitai version metadata is invalid")
		}
		seen[*file.ID] = true
		artifact := baseURL + "?fileId=" + strconv.Itoa(*file.ID)
		if file.DownloadURL != artifact {
			return "", operation.InvalidRequest("Civitai file metadata requires an unsupported download URL")
		}
		candidates = append(candidates, operation.CivitaiFileCandidate{ID: *file.ID, Name: file.Name, Primary: *file.Primary})
	}
	selected := -1
	if source.FileID != nil {
		for index, file := range metadata.Files {
			if *file.ID == *source.FileID {
				selected = index
				break
			}
		}
		if selected < 0 {
			return "", operation.InvalidRequest("file_id does not belong to the exact Civitai version")
		}
	} else if len(metadata.Files) == 1 {
		selected = 0
	} else {
		for index, file := range metadata.Files {
			if file.Primary != nil && *file.Primary {
				if selected != -1 {
					selected = -2
					break
				}
				selected = index
			}
		}
	}
	if selected < 0 {
		slices.SortFunc(candidates, func(a, b operation.CivitaiFileCandidate) int { return cmp.Compare(a.ID, b.ID) })
		return "", &operation.CivitaiFileSelectionError{VersionID: versionID, Candidates: candidates}
	}
	file := metadata.Files[selected]
	artifact := baseURL + "?fileId=" + strconv.Itoa(*file.ID)
	return artifact, nil
}

// civitaiVersionChoices returns every version listed for a model page as a
// selection, never choosing one even when only one version exists.
func (installer Installer) civitaiVersionChoices(ctx context.Context, modelID int, token string) error {
	body, err := installer.civitaiMetadata(ctx, "https://civitai.com/api/v1/models/"+strconv.Itoa(modelID), token)
	if err != nil {
		return err
	}
	var metadata civitaiModel
	if err := json.Unmarshal(body, &metadata); err != nil || metadata.ID == nil || *metadata.ID != modelID || len(metadata.ModelVersions) == 0 {
		return operation.InvalidRequest("Civitai model metadata is invalid")
	}
	candidates := make([]operation.CivitaiVersionCandidate, 0, len(metadata.ModelVersions))
	seen := make(map[int]bool, len(metadata.ModelVersions))
	for _, version := range metadata.ModelVersions {
		if version.ID == nil || *version.ID <= 0 || version.Name == "" || seen[*version.ID] {
			return operation.InvalidRequest("Civitai model metadata is invalid")
		}
		seen[*version.ID] = true
		candidates = append(candidates, operation.CivitaiVersionCandidate{ID: *version.ID, Name: version.Name})
	}
	slices.SortFunc(candidates, func(a, b operation.CivitaiVersionCandidate) int { return cmp.Compare(a.ID, b.ID) })
	return &operation.CivitaiVersionSelectionError{ModelID: modelID, Candidates: candidates}
}

// civitaiMetadata reads one Civitai API metadata document. A supplied token is
// sent only to this HTTPS endpoint, and redirects are never followed.
func (installer Installer) civitaiMetadata(ctx context.Context, endpoint, token string) ([]byte, error) {
	client := installer.CivitaiMetadataClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	privateClient := *client
	privateClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, &CivitaiMetadataAccessError{}
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := privateClient.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, context.Canceled
		}
		return nil, &CivitaiMetadataAccessError{}
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, &operation.AuthenticationRequiredError{Message: "Civitai metadata requires a valid source token"}
	}
	if response.StatusCode != http.StatusOK {
		return nil, &CivitaiMetadataAccessError{StatusCode: response.StatusCode}
	}
	const maxMetadata = 2 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, maxMetadata+1))
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, context.Canceled
		}
		return nil, &CivitaiMetadataAccessError{}
	}
	if len(body) > maxMetadata {
		return nil, operation.InvalidRequest("Civitai metadata is invalid")
	}
	return body, nil
}

// parseCivitaiReference returns the exact version ID and, for a model page, its
// model ID. A model page without modelVersionId returns version ID zero.
func parseCivitaiReference(reference string) (int, int, error) {
	if versionID, err := positiveDecimalID(reference); err == nil {
		return versionID, 0, nil
	}
	parsed, err := url.Parse(reference)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "civitai.com" || parsed.User != nil || strings.Contains(reference, "#") || parsed.RawPath != "" || strings.Contains(parsed.EscapedPath(), "%") {
		return 0, 0, operation.InvalidRequest("Civitai source must be a positive decimal version ID or a Civitai model page URL")
	}
	parts := strings.Split(parsed.Path, "/")
	if (len(parts) != 3 && len(parts) != 4) || parts[0] != "" || parts[1] != "models" || (len(parts) == 4 && parts[3] == "") {
		return 0, 0, operation.InvalidRequest("Civitai source must be a model page URL")
	}
	modelID, err := positiveDecimalID(parts[2])
	if err != nil {
		return 0, 0, operation.InvalidRequest("Civitai model page ID must be positive decimal")
	}
	if parsed.RawQuery == "" && !parsed.ForceQuery {
		return 0, modelID, nil
	}
	value, ok := strings.CutPrefix(parsed.RawQuery, "modelVersionId=")
	if !ok {
		return 0, 0, operation.InvalidRequest("Civitai model page query may contain only one explicit modelVersionId")
	}
	versionID, err := positiveDecimalID(value)
	if err != nil {
		return 0, 0, operation.InvalidRequest("Civitai modelVersionId must be positive decimal")
	}
	return versionID, modelID, nil
}

func positiveDecimalID(value string) (int, error) {
	if value == "" || strings.Trim(value, "0123456789") != "" {
		return 0, operation.InvalidRequest("identifier must be positive decimal")
	}
	id, err := strconv.Atoi(value)
	if err != nil || id <= 0 {
		return 0, operation.InvalidRequest("identifier must be positive decimal")
	}
	return id, nil
}
