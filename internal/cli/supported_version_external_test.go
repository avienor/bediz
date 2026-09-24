package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

// Every command that requires the Supported Version Range classifies the
// InvokeAI version answer the way doctor does, before any other InvokeAI
// request: an empty version is invalid_version_response, an unreadable one is
// invalid_invokeai_version, and a readable one outside the range is
// unsupported_capability.
func TestVersionGatedCommandsClassifyTheVersionAnswerBeforeAnyOtherRequest(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "source.png")
	writeTestPNG(t, imagePath)
	commands := []struct {
		name      string
		operation string
		stdin     string
		args      []string
	}{
		{name: "generate", operation: "generate", args: []string{"generate", "--no-wait", "--model", "main-key", "--prompt", "test", "--seed", "7"}},
		{name: "upscale", operation: "upscale", args: []string{"upscale", "--no-wait", "--image", "source.png", "--model", "sdxl-main", "--tile-controlnet", "tile", "--seed", "42"}},
		{name: "images upload", operation: "images.upload", args: []string{"images", "upload", imagePath}},
		{name: "images delete", operation: "images.delete", args: []string{"images", "delete", "one-image.png", "--yes"}},
		{name: "recall", operation: "recall", args: []string{"recall", "--seed", "1"}},
		{name: "models install", operation: "models.install", args: []string{"models", "install", "--source-type", "url", "--source", "https://example.org/model.safetensors"}},
		{name: "auth huggingface login", operation: "auth.huggingface.login", stdin: "hf-version-sentinel\n", args: []string{"auth", "huggingface", "login", "--token-stdin"}},
		{name: "auth huggingface logout", operation: "auth.huggingface.logout", args: []string{"auth", "huggingface", "logout"}},
		{name: "boards create", operation: "boards.create", args: []string{"boards", "create", "Portraits"}},
		{name: "queue cancel", operation: "queue.cancel", args: []string{"queue", "cancel", "12"}},
		{name: "queue clear", operation: "queue.clear", args: []string{"queue", "clear", "--yes"}},
	}
	versions := []struct {
		name    string
		version string
		code    string
		details map[string]any
	}{
		{name: "empty", version: "", code: result.CodeInvalidVersionResponse},
		{name: "unreadable", version: "six", code: result.CodeInvalidInvokeAIVersion, details: map[string]any{"version": "six"}},
		{name: "outside range", version: "6.15.0", code: result.CodeUnsupportedCapability},
	}
	for _, command := range commands {
		for _, version := range versions {
			t.Run(command.name+"/"+version.name, func(t *testing.T) {
				isolateUserConfigDir(t)
				var other atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet && r.URL.Path == "/api/v1/app/version" {
						_ = json.NewEncoder(w).Encode(map[string]string{"version": version.version})
						return
					}
					other.Add(1)
					http.NotFound(w, r)
				}))
				t.Cleanup(server.Close)
				var stdout, stderr bytes.Buffer
				app := cli.NewWithIO(strings.NewReader(command.stdin), &stdout, &stderr)

				exitCode := app.Run(t.Context(), append(command.args, "--url", server.URL, "--json"))

				var envelope struct {
					Operation string `json:"operation"`
					Error     *struct {
						Code    string         `json:"code"`
						Details map[string]any `json:"details"`
					} `json:"error"`
				}
				if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
					t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
				}
				if exitCode != result.ExitStatus(version.code) || stderr.Len() != 0 || envelope.Operation != command.operation ||
					envelope.Error == nil || envelope.Error.Code != version.code {
					t.Fatalf("exit code = %d, stderr = %q, stdout = %s", exitCode, stderr.String(), stdout.String())
				}
				for key, want := range version.details {
					if envelope.Error.Details[key] != want {
						t.Fatalf("details = %#v, want %s = %v", envelope.Error.Details, key, want)
					}
				}
				if requests := other.Load(); requests != 0 {
					t.Fatalf("sent %d requests after the version answer, want none", requests)
				}
			})
		}
	}
}
