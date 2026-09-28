// Package sourceimage prepares and resolves an operation's Source Image.
package sourceimage

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/images"
	"github.com/avienor/bediz/internal/operation"
)

// Source is either an existing InvokeAI image or a local path to upload.
type Source struct {
	Type      string `json:"type"`
	Reference string `json:"reference"`
}

// Validate checks the Source Image shape without network access.
func (source Source) Validate() error {
	switch source.Type {
	case "image":
		if source.Reference == "" {
			return operation.InvalidRequest("image source must name an existing InvokeAI image")
		}
	case "path":
		if !filepath.IsAbs(source.Reference) {
			return operation.InvalidRequest("path source must be an absolute local image path")
		}
	default:
		return operation.InvalidRequest("source requires an existing InvokeAI image or an absolute local image path")
	}
	return nil
}

// Prepared holds a checked local file open until all remote preflight checks
// have finished. An existing InvokeAI image needs no local file.
type Prepared struct {
	source Source
	upload *images.PreparedUpload
}

// Prepare checks a path with the same content rules as images upload.
func Prepare(source Source) (*Prepared, error) {
	if err := source.Validate(); err != nil {
		return nil, err
	}
	prepared := &Prepared{source: source}
	if source.Type == "path" {
		var err error
		prepared.upload, err = images.PrepareUpload(source.Reference)
		if err != nil {
			return nil, err
		}
	}
	return prepared, nil
}

// Close releases a local file if Resolve was not called.
func (prepared *Prepared) Close() error {
	if prepared.upload == nil {
		return nil
	}
	return prepared.upload.Close()
}

type Resolved struct {
	Image    images.Reference
	Uploaded bool
}

// Resolve confirms an existing image or sends one prepared upload. Call it
// only after the operation has finished every validation and resolution step.
func (prepared *Prepared) Resolve(ctx context.Context, client *httpclient.Client) (Resolved, error) {
	if prepared.upload != nil {
		image, err := prepared.upload.Send(ctx, client)
		if err != nil {
			return Resolved{}, err
		}
		return Resolved{Image: image, Uploaded: true}, nil
	}
	result, err := images.Get(ctx, client, images.GetRequest{SchemaVersion: 1, ImageName: prepared.source.Reference})
	if err != nil {
		return Resolved{}, err
	}
	if result.Image.ImageName != prepared.source.Reference {
		return Resolved{}, &httpclient.InvalidResponseError{Err: fmt.Errorf("source image has contradictory name %q; expected %q", result.Image.ImageName, prepared.source.Reference)}
	}
	return Resolved{Image: result.Image}, nil
}

// UploadedError retains the complete Image Reference when a step after a
// successful upload fails. The uploaded image remains in InvokeAI.
type UploadedError struct {
	Source images.Reference
	Err    error
}

func (e *UploadedError) Error() string {
	return fmt.Sprintf("%v (uploaded source image %s was retained)", e.Err, e.Source.ImageName)
}

func (e *UploadedError) Unwrap() error { return e.Err }
