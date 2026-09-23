package images

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
)

type ListRequest struct {
	SchemaVersion       int    `json:"schema_version"`
	Offset              int    `json:"offset"`
	Limit               int    `json:"limit"`
	BoardID             string `json:"board_id,omitempty"`
	IncludeIntermediate bool   `json:"include_intermediate,omitempty"`
}

type Reference struct {
	ImageName      string  `json:"image_name"`
	ImageURL       string  `json:"image_url"`
	ThumbnailURL   string  `json:"thumbnail_url"`
	ImageOrigin    string  `json:"image_origin"`
	ImageCategory  string  `json:"image_category"`
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	IsIntermediate bool    `json:"is_intermediate"`
	SessionID      *string `json:"session_id,omitempty"`
	NodeID         *string `json:"node_id,omitempty"`
	Starred        bool    `json:"starred"`
	HasWorkflow    bool    `json:"has_workflow"`
	BoardID        *string `json:"board_id,omitempty"`
}

type ListResult struct {
	Offset int         `json:"offset"`
	Limit  int         `json:"limit"`
	Total  int         `json:"total"`
	Items  []Reference `json:"items"`
}

type GetRequest struct {
	SchemaVersion int    `json:"schema_version"`
	ImageName     string `json:"image_name"`
}

type GetResult struct {
	Image Reference `json:"image"`
}

type UploadRequest struct {
	SchemaVersion int    `json:"schema_version"`
	Path          string `json:"path"`
}

type listResponse struct {
	Offset int           `json:"offset"`
	Limit  int           `json:"limit"`
	Total  int           `json:"total"`
	Items  []imageRecord `json:"items"`
}

type imageRecord struct {
	ImageName      string  `json:"image_name"`
	ImageURL       string  `json:"image_url"`
	ThumbnailURL   string  `json:"thumbnail_url"`
	ImageOrigin    string  `json:"image_origin"`
	ImageCategory  string  `json:"image_category"`
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	IsIntermediate bool    `json:"is_intermediate"`
	SessionID      *string `json:"session_id"`
	NodeID         *string `json:"node_id"`
	Starred        bool    `json:"starred"`
	HasWorkflow    bool    `json:"has_workflow"`
	BoardID        *string `json:"board_id"`
}

func List(ctx context.Context, client *httpclient.Client, request ListRequest) (ListResult, error) {
	if request.SchemaVersion != 1 {
		return ListResult{}, operation.InvalidRequest(fmt.Sprintf("unsupported request schema version %d", request.SchemaVersion))
	}
	if request.Offset < 0 {
		return ListResult{}, operation.InvalidRequest("offset must be non-negative")
	}
	if request.Limit < 1 || request.Limit > 100 {
		return ListResult{}, operation.InvalidRequest("limit must be between 1 and 100")
	}

	query := url.Values{}
	query.Set("offset", strconv.Itoa(request.Offset))
	query.Set("limit", strconv.Itoa(request.Limit))
	query.Set("order_dir", "DESC")
	query.Set("starred_first", "false")
	if !request.IncludeIntermediate {
		query.Set("is_intermediate", "false")
	}
	if request.BoardID != "" {
		query.Set("board_id", request.BoardID)
	}

	var response listResponse
	if err := client.GetJSON(ctx, "/api/v1/images/?"+query.Encode(), &response); err != nil {
		return ListResult{}, err
	}
	result := ListResult{
		Offset: response.Offset,
		Limit:  response.Limit,
		Total:  response.Total,
		Items:  make([]Reference, 0, len(response.Items)),
	}
	for _, item := range response.Items {
		reference, err := normalizeReference(client, item)
		if err != nil {
			return ListResult{}, err
		}
		result.Items = append(result.Items, reference)
	}
	return result, nil
}

func Get(ctx context.Context, client *httpclient.Client, request GetRequest) (GetResult, error) {
	if request.SchemaVersion != 1 {
		return GetResult{}, operation.InvalidRequest(fmt.Sprintf("unsupported request schema version %d", request.SchemaVersion))
	}
	if request.ImageName == "" {
		return GetResult{}, operation.InvalidRequest("image name is required")
	}
	var response imageRecord
	if err := client.GetJSON(ctx, "/api/v1/images/i/"+url.PathEscape(request.ImageName), &response); err != nil {
		return GetResult{}, err
	}
	reference, err := normalizeReference(client, response)
	if err != nil {
		return GetResult{}, err
	}
	return GetResult{Image: reference}, nil
}

func Upload(ctx context.Context, client *httpclient.Client, request UploadRequest) (GetResult, error) {
	if request.SchemaVersion != 1 {
		return GetResult{}, operation.InvalidRequest(fmt.Sprintf("unsupported request schema version %d", request.SchemaVersion))
	}
	upload, err := PrepareUpload(request.Path)
	if err != nil {
		return GetResult{}, err
	}
	defer func() { _ = upload.Close() }()

	var version struct {
		Version string `json:"version"`
	}
	if err := client.GetJSON(ctx, "/api/v1/app/version", &version); err != nil {
		return GetResult{}, err
	}
	supported, err := capability.SupportsInvokeAI(version.Version)
	if err != nil {
		return GetResult{}, err
	}
	if !supported {
		return GetResult{}, operation.UnsupportedCapability(fmt.Sprintf("InvokeAI %s is outside the supported range %s", version.Version, capability.SupportedInvokeAIRange))
	}
	reference, err := upload.Send(ctx, client)
	if err != nil {
		return GetResult{}, err
	}
	return GetResult{Image: reference}, nil
}

// PreparedUpload is a local image file that passed every check possible
// without network access. Callers that must finish remote validation first
// prepare the upload early and send it later.
type PreparedUpload struct {
	file        *os.File
	path        string
	contentType string
}

// PrepareUpload requires an absolute path naming a readable regular file.
func PrepareUpload(path string) (*PreparedUpload, error) {
	if !filepath.IsAbs(path) {
		return nil, operation.InvalidRequest("upload path must be absolute")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, operation.InvalidRequest(fmt.Sprintf("open upload file: %v", err))
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, operation.InvalidRequest(fmt.Sprintf("inspect upload file: %v", err))
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, operation.InvalidRequest("upload path must name a regular file")
	}
	contentType, err := uploadContentType(file, path)
	if err != nil {
		_ = file.Close()
		return nil, operation.InvalidRequest(fmt.Sprintf("read upload file: %v", err))
	}
	return &PreparedUpload{file: file, path: path, contentType: contentType}, nil
}

// Close releases an upload that was not sent. It is safe after Send.
func (u *PreparedUpload) Close() error {
	if u.file == nil {
		return nil
	}
	err := u.file.Close()
	u.file = nil
	return err
}

// Send uploads the file once as a non-intermediate user image without a
// board, resizing, or injected metadata. It is never retried: a transport
// failure or an incomplete response is an OutcomeUnknownError.
func (u *PreparedUpload) Send(ctx context.Context, client *httpclient.Client) (Reference, error) {
	if u.file == nil {
		return Reference{}, errors.New("upload file was already sent or closed")
	}
	file := u.file
	u.file = nil
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	formContentType := multipartWriter.FormDataContentType()
	go func() {
		defer func() { _ = file.Close() }()
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
			"name":     "file",
			"filename": filepath.Base(u.path),
		}))
		header.Set("Content-Type", u.contentType)
		part, writeErr := multipartWriter.CreatePart(header)
		if writeErr == nil {
			_, writeErr = io.Copy(part, file)
		}
		if closeErr := multipartWriter.Close(); writeErr == nil {
			writeErr = closeErr
		}
		_ = writer.CloseWithError(writeErr)
	}()

	const uploadPath = "/api/v1/images/upload"
	query := url.Values{}
	query.Set("image_category", "user")
	query.Set("is_intermediate", "false")
	var response imageRecord
	if err := client.PostStream(ctx, uploadPath+"?"+query.Encode(), reader, formContentType, &response); err != nil {
		return Reference{}, err
	}
	if response.ImageName == "" {
		uploadURL, err := client.ResolveURL(uploadPath)
		if err != nil {
			return Reference{}, fmt.Errorf("resolve upload URL: %w", err)
		}
		return Reference{}, &httpclient.OutcomeUnknownError{Method: http.MethodPost, URL: uploadURL, Err: errors.New("InvokeAI returned an upload result without an image name")}
	}
	return normalizeReference(client, response)
}

func uploadContentType(file *os.File, path string) (string, error) {
	buffer := make([]byte, 512)
	read, err := file.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	detected := http.DetectContentType(buffer[:read])
	if strings.HasPrefix(detected, "image/") {
		return detected, nil
	}
	if byExtension := mime.TypeByExtension(filepath.Ext(path)); strings.HasPrefix(byExtension, "image/") {
		return byExtension, nil
	}
	return detected, nil
}

func normalizeReference(client *httpclient.Client, image imageRecord) (Reference, error) {
	imageURL, err := client.ResolveURL(image.ImageURL)
	if err != nil {
		return Reference{}, fmt.Errorf("resolve image url: %w", err)
	}
	thumbnailURL, err := client.ResolveURL(image.ThumbnailURL)
	if err != nil {
		return Reference{}, fmt.Errorf("resolve thumbnail url: %w", err)
	}
	return Reference{
		ImageName:      image.ImageName,
		ImageURL:       imageURL,
		ThumbnailURL:   thumbnailURL,
		ImageOrigin:    image.ImageOrigin,
		ImageCategory:  image.ImageCategory,
		Width:          image.Width,
		Height:         image.Height,
		CreatedAt:      image.CreatedAt,
		UpdatedAt:      image.UpdatedAt,
		IsIntermediate: image.IsIntermediate,
		SessionID:      image.SessionID,
		NodeID:         image.NodeID,
		Starred:        image.Starred,
		HasWorkflow:    image.HasWorkflow,
		BoardID:        image.BoardID,
	}, nil
}
