package images_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/images"
)

func TestUploadUsesImageContentTypeForMisleadingExtension(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(imagePath, png, 0o600); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "6.14.1"})
		case "/api/v1/images/upload":
			file, header, err := r.FormFile("file")
			if err != nil {
				t.Errorf("read uploaded image: %v", err)
				return
			}
			defer func() { _ = file.Close() }()
			if got := header.Header.Get("Content-Type"); got != "image/png" {
				t.Errorf("uploaded content type = %q, want image/png", got)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"image_name": "uploaded.png", "image_url": "/api/v1/images/i/uploaded.png/full", "thumbnail_url": "/api/v1/images/i/uploaded.png/thumbnail",
				"image_origin": "external", "image_category": "user", "width": 1, "height": 1,
				"created_at": "created", "updated_at": "updated", "is_intermediate": false, "starred": false, "has_workflow": false,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}

	result, err := images.Upload(t.Context(), client, images.UploadRequest{SchemaVersion: 1, Path: imagePath})

	if err != nil {
		t.Fatal(err)
	}
	if result.Image.ImageName != "uploaded.png" {
		t.Fatalf("unexpected upload result: %#v", result)
	}
}
