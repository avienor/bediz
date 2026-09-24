package cli_test

import (
	"bytes"
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

func runProfileUpscale(t *testing.T, serverURL string, args ...string) (int, upscaleReceiptEnvelope) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	arguments := append(slices.Clone(args), "--url", serverURL, "--json")
	code := cli.New(&stdout, &stderr).Run(t.Context(), arguments)
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	var envelope upscaleReceiptEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v; stdout=%s", err, stdout.String())
	}
	return code, envelope
}

func runProfileUpscaleDocument(t *testing.T, serverURL, document string) (int, upscaleReceiptEnvelope) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.NewWithIO(strings.NewReader(document), &stdout, &stderr).Run(t.Context(), []string{"upscale", "--no-wait", "--request", "-", "--url", serverURL, "--json"})
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	var envelope upscaleReceiptEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v; stdout=%s", err, stdout.String())
	}
	return code, envelope
}

func newProfileUpscaleServer(t *testing.T, inventory []map[string]any) (*httptest.Server, *int) {
	t.Helper()
	enqueues := new(int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), inventory) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/images/i/source.png":
			_ = json.MarshalWrite(w, upscaleImage("source.png", 512, 512))
		case "/api/v1/queue/default/enqueue_batch":
			*enqueues++
			acceptEnqueue(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, enqueues
}

func TestUpscaleProfileSuppliesModelSettingsAndTileChoice(t *testing.T) {
	isolateUserConfigDir(t)
	createGenerateProfile(t, `{"schema_version":1,"name":"preset","upscale":{"model":"sdxl-main","components":{"tile_controlnet":"tile","upscale_model":"spandrel","vae":"vae"},"scale":2,"creativity":3,"structure":-2,"steps":12,"scheduler":"euler","guidance":5,"tile_size":512,"tile_overlap":32}}`)
	enqueues := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), upscaleInventory()) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/images/i/source.png":
			_ = json.MarshalWrite(w, upscaleImage("source.png", 512, 512))
		case "/api/v1/queue/default/enqueue_batch":
			enqueues++
			acceptEnqueue(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	code, envelope := runProfileUpscale(t, server.URL, "upscale", "--no-wait", "--image", "source.png", "--profile", "preset", "--seed", "42")
	settings := envelope.Data.ResolvedSettings
	if code != 0 || !envelope.OK || enqueues != 1 || envelope.Data.SubmittedRequest["profile"] != "preset" || settings.Profile != "preset" || settings.ModelKey != "sdxl-main" ||
		settings.Scale != 2 || settings.Creativity != 3 || settings.Structure != -2 || settings.Steps != 12 || settings.Scheduler != "euler" ||
		settings.Guidance != 5 || settings.TileSize != 512 || settings.TileOverlap != 32 ||
		!reflect.DeepEqual(settings.ComponentKeys, map[string]string{"upscale_model": "spandrel", "tile_controlnet": "tile", "vae": "vae"}) {
		t.Fatalf("code=%d envelope=%#v enqueues=%d", code, envelope, enqueues)
	}
}

func TestUpscaleProfileLeavesUnspecifiedSettingsAtDefaults(t *testing.T) {
	for _, tc := range []struct {
		name, model, tile string
		inventory         []map[string]any
	}{
		{"SDXL", "sdxl-main", "tile", upscaleInventory()},
		{"SD1.5", "sd1-main", "sd1-tile", sd1UpscaleInventory()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			createGenerateProfile(t, `{"schema_version":1,"name":"preset","upscale":{"model":"`+tc.model+`","components":{"tile_controlnet":"`+tc.tile+`"}}}`)
			server, enqueues := newProfileUpscaleServer(t, tc.inventory)
			code, envelope := runProfileUpscale(t, server.URL, "upscale", "--no-wait", "--image", "source.png", "--profile", "preset", "--seed", "42")
			s := envelope.Data.ResolvedSettings
			if code != 0 || !envelope.OK || *enqueues != 1 || s.Profile != "preset" || s.ModelKey != tc.model || s.Scale != 4 || s.Creativity != 0 || s.Structure != 0 || s.Steps != 30 || s.Scheduler != "kdpm_2" || s.Guidance != 2 || s.TileSize != 1024 || s.TileOverlap != 128 || s.ComponentKeys["tile_controlnet"] != tc.tile {
				t.Fatalf("code=%d envelope=%#v enqueues=%d", code, envelope, *enqueues)
			}
		})
	}
}

func TestUpscaleRequestFieldsOverrideProfileForBothFamilies(t *testing.T) {
	for _, tc := range []struct {
		name, model, profileModel, tile, vae string
		inventory                            []map[string]any
		document                             bool
	}{
		{"SDXL flags", "sdxl-main", "absent-main", "tile", "vae", upscaleInventory(), false},
		{"SD1.5 document", "sd1-main", "sd1-main", "sd1-tile", "sd1-vae", sd1UpscaleInventory(), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			createGenerateProfile(t, `{"schema_version":1,"name":"preset","upscale":{"model":"`+tc.profileModel+`","components":{"tile_controlnet":"absent-tile","upscale_model":"absent-upscaler","vae":"absent-vae"},"scale":2,"creativity":3,"structure":-2,"steps":12,"scheduler":"euler","guidance":5,"tile_size":512,"tile_overlap":32}}`)
			server, enqueues := newProfileUpscaleServer(t, tc.inventory)
			var code int
			var envelope upscaleReceiptEnvelope
			if tc.document {
				code, envelope = runProfileUpscaleDocument(t, server.URL, `{"schema_version":1,"source":{"type":"image","reference":"source.png"},"profile":"preset","scale":4,"creativity":-1,"structure":2,"steps":8,"scheduler":"heun","guidance":4,"tile_size":768,"tile_overlap":64,"seed":42,"components":{"upscale_model":"spandrel","tile_controlnet":"`+tc.tile+`","vae":"`+tc.vae+`"}}`)
			} else {
				code, envelope = runProfileUpscale(t, server.URL, "upscale", "--no-wait", "--image", "source.png", "--profile", "preset", "--model", tc.model, "--scale", "4", "--creativity", "-1", "--structure", "2", "--steps", "8", "--scheduler", "heun", "--guidance", "4", "--tile-size", "768", "--tile-overlap", "64", "--seed", "42", "--upscale-model", "spandrel", "--tile-controlnet", tc.tile, "--vae", tc.vae)
			}
			s := envelope.Data.ResolvedSettings
			if code != 0 || !envelope.OK || *enqueues != 1 || s.Profile != "preset" || s.ModelKey != tc.model || s.Scale != 4 || s.Creativity != -1 || s.Structure != 2 || s.Steps != 8 || s.Scheduler != "heun" || s.Guidance != 4 || s.TileSize != 768 || s.TileOverlap != 64 || !reflect.DeepEqual(s.ComponentKeys, map[string]string{"upscale_model": "spandrel", "tile_controlnet": tc.tile, "vae": tc.vae}) || len(envelope.Warnings) != 1 {
				t.Fatalf("code=%d envelope=%#v enqueues=%d", code, envelope, *enqueues)
			}
		})
	}
}

func TestUpscaleProfileErrorsPrecedeNetwork(t *testing.T) {
	for _, tc := range []struct {
		name, profile, args, code string
	}{
		{"absent", "", "absent", result.CodeNotFound},
		{"no upscale section", `{"schema_version":1,"name":"preset","generate":{"model":"sdxl-main"}}`, "preset", result.CodeInvalidRequest},
		{"no model anywhere", `{"schema_version":1,"name":"preset","upscale":{}}`, "preset", result.CodeInvalidRequest},
		{"invalid profile name", "", "Not Allowed", result.CodeInvalidRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			if tc.profile != "" {
				createGenerateProfile(t, tc.profile)
			}
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				http.NotFound(w, r)
			}))
			defer server.Close()
			code, envelope := runProfileUpscale(t, server.URL, "upscale", "--no-wait", "--image", "source.png", "--profile", tc.args)
			if code != result.ExitStatus(tc.code) || envelope.Error == nil || envelope.Error.Code != tc.code || requests != 0 {
				t.Fatalf("code=%d envelope=%#v requests=%d", code, envelope, requests)
			}
		})
	}
}

func TestUpscaleCombinedProfileSettingsValidateBeforeNetwork(t *testing.T) {
	isolateUserConfigDir(t)
	createGenerateProfile(t, `{"schema_version":1,"name":"preset","upscale":{"model":"sdxl-main","tile_overlap":512}}`)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.NotFound(w, r)
	}))
	defer server.Close()
	code, envelope := runProfileUpscale(t, server.URL, "upscale", "--no-wait", "--image", "source.png", "--profile", "preset", "--tile-size", "512")
	if code != result.ExitInvalidRequest || envelope.Error == nil || envelope.Error.Code != result.CodeInvalidRequest || requests != 0 {
		t.Fatalf("code=%d envelope=%#v requests=%d", code, envelope, requests)
	}
}

func TestGenerateOnlyProfileStillWorksForGenerate(t *testing.T) {
	isolateUserConfigDir(t)
	createGenerateProfile(t, `{"schema_version":1,"name":"preset","generate":{"model":"sdxl-main"}}`)
	envelope, enqueues := runProfileGeneration(t, sdxlInventory(), `{"schema_version":1,"profile":"preset","positive_prompt":"test","seed":42}`)
	if enqueues != 1 || envelope["data"].(map[string]any)["resolved_settings"].(map[string]any)["model_key"] != "sdxl-main" {
		t.Fatalf("enqueues=%d envelope=%#v", enqueues, envelope)
	}
}

func TestUpscaleSkipsUnusableProfileComponentPreferences(t *testing.T) {
	for _, kind := range []string{"upscale_model", "tile_controlnet", "vae"} {
		for _, reason := range []string{"not_found", "incompatible", "ambiguous"} {
			t.Run(kind+"/"+reason, func(t *testing.T) {
				isolateUserConfigDir(t)
				inventory := upscaleInventory()
				selector := "absent-preference"
				key := map[string]string{"upscale_model": "spandrel", "tile_controlnet": "tile", "vae": "vae"}[kind]
				if reason == "incompatible" {
					selector = "wrong-preference"
					modelType := map[string]string{"upscale_model": "spandrel_image_to_image", "tile_controlnet": "controlnet", "vae": "vae"}[kind]
					inventory = append(inventory, map[string]any{"key": selector, "hash": "wrong-hash", "name": "Wrong preference", "base": "sd-1", "type": modelType})
				}
				if reason == "ambiguous" {
					selector = "Shared preference"
					for _, model := range inventory {
						if model["key"] == key {
							model["name"] = selector
						}
					}
					inventory = append(inventory, map[string]any{"key": "other-preference", "hash": "other-hash", "name": selector, "base": "sd-1", "type": "vae"})
				}
				createGenerateProfile(t, `{"schema_version":1,"name":"preset","upscale":{"model":"sdxl-main","components":{"`+kind+`":"`+selector+`"}}}`)
				server, enqueues := newProfileUpscaleServer(t, inventory)
				args := []string{"upscale", "--no-wait", "--image", "source.png", "--profile", "preset", "--seed", "42"}
				if kind != "tile_controlnet" {
					args = append(args, "--tile-controlnet", "tile")
				}
				code, envelope := runProfileUpscale(t, server.URL, args...)
				if kind == "tile_controlnet" {
					if code != result.ExitSelectionRequired || envelope.Error == nil || envelope.Error.Code != result.CodeSelectionRequired || envelope.Error.Details["kind"] != "tile_controlnet" || *enqueues != 0 {
						t.Fatalf("code=%d envelope=%#v enqueues=%d", code, envelope, *enqueues)
					}
				} else if code != 0 || !envelope.OK || *enqueues != 1 {
					t.Fatalf("code=%d envelope=%#v enqueues=%d", code, envelope, *enqueues)
				}
				if len(envelope.Warnings) == 0 || envelope.Warnings[0].Code != "profile_preference_skipped" ||
					envelope.Warnings[0].Details["profile"] != "preset" || envelope.Warnings[0].Details["component"] != kind || envelope.Warnings[0].Details["reason"] != reason {
					t.Fatalf("warnings=%#v", envelope.Warnings)
				}
				if envelope.OK && (!reflect.DeepEqual(envelope.Data.Warnings, envelope.Warnings) || envelope.Data.ResolvedSettings.ComponentKeys["upscale_model"] != "spandrel" || envelope.Data.ResolvedSettings.ComponentKeys["tile_controlnet"] != "tile") {
					t.Fatalf("receipt=%#v", envelope.Data)
				}
			})
		}
	}
}

func TestUpscaleProfilePathResolutionFailuresNeverUpload(t *testing.T) {
	path := writeUploadSource(t)
	for _, tc := range []struct {
		name, profile string
		args          []string
		wantCode      string
	}{
		{"missing Tile preference", `{"schema_version":1,"name":"preset","upscale":{"model":"sdxl-main","components":{"tile_controlnet":"absent"}}}`, nil, result.CodeSelectionRequired},
		{"incompatible Tile preference", `{"schema_version":1,"name":"preset","upscale":{"model":"sdxl-main","components":{"tile_controlnet":"sd1-tile"}}}`, nil, result.CodeSelectionRequired},
		{"invalid combined tile settings", `{"schema_version":1,"name":"preset","upscale":{"model":"sdxl-main","components":{"tile_controlnet":"tile"},"tile_overlap":512}}`, []string{"--tile-size", "512"}, result.CodeInvalidRequest},
		{"unsupported profile main", `{"schema_version":1,"name":"preset","upscale":{"model":"unsupported-main","components":{"tile_controlnet":"tile"}}}`, nil, result.CodeUnsupportedCapability},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			createGenerateProfile(t, tc.profile)
			inventory := sd1UpscaleInventory()
			inventory = append(inventory, map[string]any{"key": "unsupported-main", "hash": "unsupported-hash", "name": "Unsupported", "base": "anima", "type": "main", "variant": "normal"})
			server := newUpscaleUploadServer(t, upscaleUploadHandlers{inventory: inventory})
			args := append([]string{"upscale", "--no-wait", "--image-path", path, "--profile", "preset", "--seed", "42"}, tc.args...)
			code, envelope := runProfileUpscale(t, server.URL, args...)
			if code != result.ExitStatus(tc.wantCode) || envelope.Error == nil || envelope.Error.Code != tc.wantCode || server.count(uploadRequest) != 0 || server.count(enqueueRequest) != 0 {
				t.Fatalf("code=%d envelope=%#v sequence=%q", code, envelope, server.sequence())
			}
		})
	}
}

func TestUpscaleProfilePathUploadsAfterResolution(t *testing.T) {
	isolateUserConfigDir(t)
	createGenerateProfile(t, `{"schema_version":1,"name":"preset","upscale":{"model":"sdxl-main","components":{"tile_controlnet":"tile"},"scale":2}}`)
	server := newUpscaleUploadServer(t, upscaleUploadHandlers{})
	path := writeUploadSource(t)
	code, envelope := runProfileUpscale(t, server.URL, "upscale", "--no-wait", "--image-path", path, "--profile", "preset", "--seed", "42")
	sequence := server.sequence()
	modelIndex, uploadIndex, enqueueIndex := slices.Index(sequence, "GET /api/v2/models/"), slices.Index(sequence, uploadRequest), slices.Index(sequence, enqueueRequest)
	if code != 0 || !envelope.OK || !envelope.Data.SourceUploaded || envelope.Data.ResolvedSettings.Profile != "preset" || server.count(uploadRequest) != 1 || server.count(enqueueRequest) != 1 || modelIndex < 0 || uploadIndex <= modelIndex || enqueueIndex <= uploadIndex {
		t.Fatalf("code=%d envelope=%#v sequence=%q", code, envelope, sequence)
	}
}
