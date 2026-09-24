package images

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
)

type DownloadRequest struct {
	SchemaVersion int    `json:"schema_version"`
	ImageName     string `json:"image_name"`
	Output        string `json:"output"`
}

type DownloadResult struct {
	ImageName   string `json:"image_name"`
	Path        string `json:"path"`
	SizeBytes   int64  `json:"size_bytes"`
	ContentType string `json:"content_type"`
}

// Download writes the full-resolution image to a new local file. The output
// path is checked before any network request. The content is written to a
// temporary file in the target directory and published with a hard link,
// which fails instead of replacing a target that appeared in the meantime; a
// rename would replace it. The temporary file is removed on every path, so a
// failure leaves nothing at the target.
func Download(ctx context.Context, client *httpclient.Client, request DownloadRequest) (DownloadResult, error) {
	if request.SchemaVersion != 1 {
		return DownloadResult{}, operation.InvalidRequest(fmt.Sprintf("unsupported request schema version %d", request.SchemaVersion))
	}
	if request.ImageName == "" {
		return DownloadResult{}, operation.InvalidRequest("image name is required")
	}
	target, err := checkOutput(request.Output)
	if err != nil {
		return DownloadResult{}, err
	}

	result := DownloadResult{ImageName: request.ImageName, Path: target}
	err = client.GetStream(ctx, "/api/v1/images/i/"+url.PathEscape(request.ImageName)+"/full", "image/*",
		func(contentType string, body io.Reader) error {
			mediaType, _, err := mime.ParseMediaType(contentType)
			if err != nil || !strings.HasPrefix(mediaType, "image/") {
				return &httpclient.InvalidResponseError{Err: fmt.Errorf("full image has content type %q, not an image", contentType)}
			}
			result.ContentType = mediaType
			result.SizeBytes, err = publish(target, body)
			return err
		})
	if err != nil {
		return DownloadResult{}, err
	}
	return result, nil
}

// checkOutput resolves the output path against the working directory and
// requires an existing parent directory and an absent target. A link at the
// target counts as existing, even when it dangles.
func checkOutput(output string) (string, error) {
	if output == "" {
		return "", operation.InvalidRequest("output path is required")
	}
	target, err := filepath.Abs(output)
	if err != nil {
		return "", operation.InvalidRequest(fmt.Sprintf("resolve output path: %v", err))
	}
	if _, err := os.Lstat(target); err == nil {
		return "", &operation.OutputExistsError{Path: target}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", operation.InvalidRequest(fmt.Sprintf("inspect output path: %v", err))
	}
	parent, err := os.Stat(filepath.Dir(target))
	if err != nil {
		return "", operation.InvalidRequest(fmt.Sprintf("output directory: %v", err))
	}
	if !parent.IsDir() {
		return "", operation.InvalidRequest(fmt.Sprintf("output directory %q is not a directory", filepath.Dir(target)))
	}
	return target, nil
}

// publish copies body into a temporary file next to target and links it into
// place. Failures to read body are returned unchanged; local failures are
// OutputWriteErrors.
func publish(target string, body io.Reader) (int64, error) {
	writeFailed := func(err error) error { return &operation.OutputWriteError{Path: target, Err: err} }
	// A short fixed pattern keeps every valid target name usable.
	temporary, err := os.CreateTemp(filepath.Dir(target), ".bediz-download-*")
	if err != nil {
		return 0, writeFailed(err)
	}
	// Removal is best effort: once the link exists the download succeeded, and
	// after a failure the target was never created.
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporary.Name())
	}()

	written, err := io.Copy(fileWriter{temporary}, body)
	if err != nil {
		if writeErr, ok := errors.AsType[localWriteError](err); ok {
			return 0, writeFailed(writeErr.err)
		}
		return 0, err
	}
	// CreateTemp makes an owner-only file; a downloaded image gets the usual
	// permissions of a user-created file instead.
	if err := temporary.Chmod(0o644); err != nil {
		return 0, writeFailed(err)
	}
	if err := temporary.Sync(); err != nil {
		return 0, writeFailed(err)
	}
	if err := temporary.Close(); err != nil {
		return 0, writeFailed(err)
	}
	if err := os.Link(temporary.Name(), target); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return 0, &operation.OutputExistsError{Path: target}
		}
		return 0, writeFailed(err)
	}
	return written, nil
}

// fileWriter marks write failures so publish can tell them from failures to
// read the response body.
type fileWriter struct{ file *os.File }

func (w fileWriter) Write(p []byte) (int, error) {
	n, err := w.file.Write(p)
	if err != nil {
		return n, localWriteError{err}
	}
	return n, nil
}

type localWriteError struct{ err error }

func (e localWriteError) Error() string { return e.err.Error() }
