package cli_test

import (
	"bytes"
	"encoding/base64"
	json "encoding/json/v2"
	"image"
	"image/gif"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

func TestImagesUploadRejectsNonImageBeforeNetwork(t *testing.T) {
	text := []byte("ordinary text, not an image\n")
	inputs := []struct {
		name    string
		content []byte
	}{
		{"id_rsa", text},
		{"source.txt", text},
		{"source.png", text},
		{"source.jpeg", text},
		{"source.webp", text},
		{"source.gif", text},
		{"empty.png", nil},
		{"binary.png", []byte{0, 1, 2, 3}},
	}
	for _, input := range inputs {
		t.Run(input.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), input.name)
			if err := os.WriteFile(path, input.content, 0o600); err != nil {
				t.Fatal(err)
			}
			commands := []struct {
				name      string
				operation string
				args      []string
				document  map[string]any
			}{
				{"images flags", result.OperationImagesUpload, []string{"images", "upload", path}, nil},
				{"images document", result.OperationImagesUpload, []string{"images", "upload", "--request", "-"}, map[string]any{"schema_version": 1, "path": path}},
				{"upscale flags", result.OperationUpscale, []string{"upscale", "--image-path", path, "--model", "sdxl-main", "--tile-controlnet", "tile"}, nil},
				{"upscale document", result.OperationUpscale, []string{"upscale", "--request", "-"}, map[string]any{
					"schema_version": 1, "source": map[string]any{"type": "path", "reference": path}, "model": "sdxl-main", "components": map[string]any{"tile_controlnet": "tile"},
				}},
			}
			for _, command := range commands {
				t.Run(command.name, func(t *testing.T) {
					isolateUserConfigDir(t)
					var uploaded atomic.Bool
					server := newUpscaleUploadServer(t, upscaleUploadHandlers{upload: func(w http.ResponseWriter, r *http.Request) {
						file, _, err := r.FormFile("file")
						if err != nil {
							t.Errorf("read upload: %v", err)
							return
						}
						defer func() { _ = file.Close() }()
						body, err := io.ReadAll(file)
						if err != nil {
							t.Errorf("read upload content: %v", err)
						}
						uploaded.Store(bytes.Equal(body, input.content))
						http.Error(w, "not an image", http.StatusUnsupportedMediaType)
					}})
					document, err := json.Marshal(command.document)
					if err != nil {
						t.Fatal(err)
					}
					args := append(command.args, "--url", server.URL, "--json")
					var stdout, stderr bytes.Buffer
					code := cli.NewWithIO(bytes.NewReader(document), &stdout, &stderr).Run(t.Context(), args)
					var envelope result.Envelope
					if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
						t.Fatalf("stdout is not one JSON object: %v; stdout=%q", err, stdout.String())
					}
					if code != result.ExitInvalidRequest || envelope.SchemaVersion != 1 || envelope.OK || envelope.Operation != command.operation || envelope.Error == nil || envelope.Error.Code != result.CodeInvalidRequest || stderr.Len() != 0 || len(server.sequence()) != 0 {
						t.Fatalf("code=%d envelope=%#v stderr=%q requests=%q uploaded_content=%v", code, envelope, stderr.String(), server.sequence(), uploaded.Load())
					}
				})
			}
		})
	}
}

func TestImagesUploadAcceptsDetectedImageContent(t *testing.T) {
	png := writeTestPNG(t, filepath.Join(t.TempDir(), "source.png"))
	source := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var jpegData, gifData bytes.Buffer
	if err := jpeg.Encode(&jpegData, source, nil); err != nil {
		t.Fatal(err)
	}
	if err := gif.Encode(&gifData, source, nil); err != nil {
		t.Fatal(err)
	}
	webp, err := base64.StdEncoding.DecodeString("UklGRhwAAABXRUJQVlA4TA8AAAAvAAAAAAcQ/Y/+ByKi/wEA")
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct {
		contentType string
		content     []byte
	}{
		{"image/png", png},
		{"image/jpeg", jpegData.Bytes()},
		{"image/webp", webp},
		{"image/gif", gifData.Bytes()},
	} {
		t.Run(input.contentType, func(t *testing.T) {
			isolateUserConfigDir(t)
			path := filepath.Join(t.TempDir(), "source.txt")
			if err := os.WriteFile(path, input.content, 0o600); err != nil {
				t.Fatal(err)
			}
			server := newUpscaleUploadServer(t, upscaleUploadHandlers{upload: func(w http.ResponseWriter, r *http.Request) {
				file, header, err := r.FormFile("file")
				if err != nil {
					t.Errorf("read upload: %v", err)
					return
				}
				defer func() { _ = file.Close() }()
				body, err := io.ReadAll(file)
				if err != nil {
					t.Errorf("read upload content: %v", err)
				}
				if !bytes.Equal(body, input.content) {
					t.Errorf("upload content changed")
				}
				if got := header.Header.Get("Content-Type"); got != input.contentType {
					t.Errorf("upload content type = %q, want %q", got, input.contentType)
				}
				acceptUpload(w, r)
			}})
			var stdout, stderr bytes.Buffer
			code := cli.New(&stdout, &stderr).Run(t.Context(), []string{"images", "upload", path, "--url", server.URL, "--json"})
			var envelope result.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("stdout is not one JSON object: %v; stdout=%q", err, stdout.String())
			}
			if code != result.ExitSuccess || envelope.SchemaVersion != 1 || !envelope.OK || envelope.Operation != result.OperationImagesUpload || envelope.Error != nil || stderr.Len() != 0 || server.count(uploadRequest) != 1 {
				t.Fatalf("code=%d envelope=%#v stderr=%q requests=%q", code, envelope, stderr.String(), server.sequence())
			}
		})
	}
}
