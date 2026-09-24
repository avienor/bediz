package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

// mutatingCommand is one command whose InvokeAI mutation a test answers.
type mutatingCommand struct {
	name      string
	operation string
	stdin     string
	args      []string
	// server answers every preflight request and delegates the mutation to
	// mutate.
	server func(t *testing.T, mutate http.HandlerFunc) *httptest.Server
}

// mutatingCommands covers every mutation path whose answer the CLI classifies
// directly: JSON mutations, private mutations, and streamed uploads.
func mutatingCommands(t *testing.T) []mutatingCommand {
	t.Helper()
	imagePath := filepath.Join(t.TempDir(), "source.png")
	writeTestPNG(t, imagePath)
	return []mutatingCommand{
		{
			name:      "generate",
			operation: "generate",
			args: []string{
				"generate", "--no-wait", "--model", "main-key", "--prompt", "test",
				"--width", "768", "--height", "768", "--steps", "20", "--scheduler", "euler", "--guidance", "4.5",
				"--seed", "7", "--output-count", "1", "--vae", "vae-key", "--qwen3-encoder", "encoder-key",
			},
			server: func(_ *testing.T, mutate http.HandlerFunc) *httptest.Server {
				return newAnimaGenerationServer(nil, animaModelInventory(), mutate)
			},
		},
		{
			name:      "upscale",
			operation: "upscale",
			args:      []string{"upscale", "--no-wait", "--image", "source.png", "--model", "sdxl-main", "--tile-controlnet", "tile", "--seed", "42"},
			server: func(t *testing.T, mutate http.HandlerFunc) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), upscaleInventory()) {
						return
					}
					switch r.URL.Path {
					case "/api/v1/images/i/source.png":
						_ = json.NewEncoder(w).Encode(upscaleImage("source.png", 512, 512))
					case "/api/v1/queue/default/enqueue_batch":
						mutate(w, r)
					default:
						http.NotFound(w, r)
					}
				}))
			},
		},
		{
			name:      "images upload",
			operation: "images.upload",
			args:      []string{"images", "upload", imagePath},
			server: func(_ *testing.T, mutate http.HandlerFunc) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/v1/app/version":
						_ = json.NewEncoder(w).Encode(map[string]string{"version": "6.14.1"})
					case "/api/v1/images/upload":
						mutate(w, r)
					default:
						http.NotFound(w, r)
					}
				}))
			},
		},
		{
			name:      "recall",
			operation: "recall",
			args:      []string{"recall", "--seed", "1"},
			server: func(_ *testing.T, mutate http.HandlerFunc) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if serveAnimaPreflight(w, r, "6.14.1", recallOpenAPI(), animaModelInventory()) {
						return
					}
					if r.URL.Path == "/api/v1/recall/default" {
						mutate(w, r)
						return
					}
					http.NotFound(w, r)
				}))
			},
		},
		{
			name:      "models install",
			operation: "models.install",
			args:      []string{"models", "install", "--source-type", "url", "--source", "https://example.org/model.safetensors"},
			server: func(_ *testing.T, mutate http.HandlerFunc) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/v1/app/version":
						_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
					case "/openapi.json":
						_, _ = w.Write([]byte(`{"paths":{"/api/v2/models/install":{"post":{"parameters":[{"name":"source","in":"query","required":true}],"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/ModelInstallJob"}}}}}}}}}`))
					case "/api/v2/models/install":
						mutate(w, r)
					default:
						http.NotFound(w, r)
					}
				}))
			},
		},
		{
			name:      "auth huggingface login",
			operation: "auth.huggingface.login",
			stdin:     "hf-gateway-sentinel\n",
			args:      []string{"auth", "huggingface", "login", "--token-stdin"},
			server:    newHuggingFaceAuthMutationServer,
		},
		{
			name:      "auth huggingface logout",
			operation: "auth.huggingface.logout",
			args:      []string{"auth", "huggingface", "logout"},
			server:    newHuggingFaceAuthMutationServer,
		},
	}
}

// runMutatingCommand runs command with --json against server and returns its
// exit code, the error of its single envelope, and stdout.
func runMutatingCommand(t *testing.T, command mutatingCommand, serverURL string) (int, *mutationFailure, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(command.stdin), &stdout, &stderr)
	exitCode := app.Run(t.Context(), append(command.args, "--url", serverURL, "--json"))
	var envelope struct {
		Operation string           `json:"operation"`
		Error     *mutationFailure `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if stderr.Len() != 0 || envelope.Operation != command.operation || envelope.Error == nil {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %s", exitCode, stderr.String(), stdout.String())
	}
	if strings.Contains(stdout.String(), "hf-gateway-sentinel") {
		t.Fatalf("token leaked: %s", stdout.String())
	}
	return exitCode, envelope.Error, stdout.String()
}

type mutationFailure struct {
	Code    string         `json:"code"`
	Details map[string]any `json:"details"`
}

// A 502, 503, or 504 answer to a mutation can come from an intermediary that
// already forwarded it to InvokeAI, so every mutating command reports
// outcome_unknown with the status and sends the mutation exactly once.
func TestMutationsReportGatewayStatusAsUnknownOutcome(t *testing.T) {
	for _, command := range mutatingCommands(t) {
		for _, status := range []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
			t.Run(command.name+"/"+strconv.Itoa(status), func(t *testing.T) {
				isolateUserConfigDir(t)
				var mutations atomic.Int32
				server := command.server(t, func(w http.ResponseWriter, _ *http.Request) {
					mutations.Add(1)
					http.Error(w, "gateway", status)
				})
				t.Cleanup(server.Close)

				exitCode, failure, stdout := runMutatingCommand(t, command, server.URL)

				if exitCode != result.ExitInvokeAIFailure || failure.Code != result.CodeOutcomeUnknown || failure.Details["status"] != float64(status) {
					t.Fatalf("exit code = %d, stdout = %s", exitCode, stdout)
				}
				if sent := mutations.Load(); sent != 1 {
					t.Fatalf("sent %d mutations, want exactly 1", sent)
				}
			})
		}
	}
}

// Following a redirect would resend a mutation, or turn it into a GET after
// 301, 302, or 303, so every mutating command reports a redirect as a
// conclusive failure with its status and never reaches the redirect target.
func TestMutationsDoNotFollowRedirects(t *testing.T) {
	for _, command := range mutatingCommands(t) {
		for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect} {
			t.Run(command.name+"/"+strconv.Itoa(status), func(t *testing.T) {
				isolateUserConfigDir(t)
				var mutations, forwarded atomic.Int32
				target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					forwarded.Add(1)
					_, _ = w.Write([]byte(`{}`))
				}))
				t.Cleanup(target.Close)
				server := command.server(t, func(w http.ResponseWriter, _ *http.Request) {
					mutations.Add(1)
					w.Header().Set("Location", target.URL+"/forwarded")
					w.WriteHeader(status)
				})
				t.Cleanup(server.Close)

				exitCode, failure, stdout := runMutatingCommand(t, command, server.URL)

				if exitCode != result.ExitInvokeAIFailure || failure.Code != result.CodeInvokeAIOperationFailed || failure.Details["status"] != float64(status) {
					t.Fatalf("exit code = %d, stdout = %s", exitCode, stdout)
				}
				if mutations.Load() != 1 || forwarded.Load() != 0 {
					t.Fatalf("mutations = %d, forwarded = %d, want 1 and 0", mutations.Load(), forwarded.Load())
				}
			})
		}
	}
}

func newHuggingFaceAuthMutationServer(_ *testing.T, mutate http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_, _ = w.Write([]byte(`{"version":"6.14.1"}`))
		case "/openapi.json":
			_, _ = w.Write([]byte(authOpenAPI))
		case "/api/v2/models/hf_login":
			mutate(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
}
