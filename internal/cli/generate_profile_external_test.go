package cli_test

import (
	"bytes"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/avienor/bediz/internal/cli"
)

func createGenerateProfile(t *testing.T, document string) {
	t.Helper()
	status, envelope, stderr := runProfilesJSON(t, document, "create", "--request", "-")
	if status != 0 || stderr != "" {
		t.Fatalf("create profile: status=%d envelope=%#v stderr=%q", status, envelope, stderr)
	}
}

func runProfileGeneration(t *testing.T, inventory []map[string]any, document string) (map[string]any, int) {
	t.Helper()
	enqueues := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), inventory) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/queue/default/enqueue_batch":
			enqueues++
			_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "enqueued": 1, "requested": 1, "item_ids": []int{17}, "batch": map[string]any{"batch_id": "profile-batch"}})
		case "/api/v1/recall/default":
			_, _ = w.Write([]byte(`{"status":"success"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	status := cli.NewWithIO(strings.NewReader(document), &stdout, &stderr).Run(t.Context(), []string{"generate", "--no-wait", "--request", "-", "--url", server.URL, "--json"})
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if status != 0 || stderr.Len() != 0 || envelope["ok"] != true {
		t.Fatalf("status=%d stderr=%q envelope=%#v", status, stderr.String(), envelope)
	}
	return envelope, enqueues
}

func TestGenerateUsesCompatibleProfileComponentPreferences(t *testing.T) {
	for _, tc := range []struct {
		name, profile, request string
		inventory              []map[string]any
		keys                   map[string]any
	}{
		{"Anima", `{"schema_version":1,"name":"preset","generate":{"components":{"vae":"preferred-vae","qwen3_encoder":"Qwen3 Encoder"}}}`, `{"schema_version":1,"profile":"preset","model":"main-key","positive_prompt":"test","seed":42}`, append(animaModelInventory(), map[string]any{"key": "preferred-vae", "hash": "preferred-hash", "name": "Preferred VAE", "base": "anima", "type": "vae"}), map[string]any{"vae": "preferred-vae", "qwen3_encoder": "encoder-key"}},
		{"SDXL", `{"schema_version":1,"name":"preset","generate":{"components":{"vae":"sdxl-vae"}}}`, `{"schema_version":1,"profile":"preset","model":"sdxl-main","positive_prompt":"test","seed":42}`, sdxlInventory(), map[string]any{"vae": "sdxl-vae"}},
		{"FLUX.1", `{"schema_version":1,"name":"preset","generate":{"components":{"t5_encoder":"flux-t5"}}}`, `{"schema_version":1,"profile":"preset","model":"flux-dev","positive_prompt":"test","seed":42}`, append(fluxCLIInventory(), map[string]any{"key": "another-t5", "hash": "another-hash", "name": "Another T5", "base": "any", "type": "t5_encoder"}), map[string]any{"vae": "flux-vae", "t5_encoder": "flux-t5", "clip_embed": "flux-clip"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			createGenerateProfile(t, tc.profile)
			envelope, enqueues := runProfileGeneration(t, tc.inventory, tc.request)
			data := envelope["data"].(map[string]any)
			settings := data["resolved_settings"].(map[string]any)
			if enqueues != 1 || !reflect.DeepEqual(settings["component_keys"], tc.keys) || data["submitted_request"].(map[string]any)["profile"] != "preset" || settings["profile"] != "preset" {
				t.Fatalf("enqueues=%d data=%#v", enqueues, data)
			}
		})
	}
}

func TestGenerateSkipsUnusableProfilePreferenceAndContinuesResolution(t *testing.T) {
	for _, tc := range []struct {
		name, selector, reason string
		additional             []map[string]any
	}{
		{"missing", "absent-vae", "not_found", nil},
		{"incompatible", "wrong-vae", "incompatible", []map[string]any{{"key": "wrong-vae", "hash": "wrong-hash", "name": "Wrong VAE", "base": "sdxl", "type": "vae"}}},
		{"ambiguous", "Shared VAE", "ambiguous", []map[string]any{{"key": "wrong-a", "hash": "hash-a", "name": "Shared VAE", "base": "sdxl", "type": "vae"}, {"key": "wrong-b", "hash": "hash-b", "name": "Shared VAE", "base": "flux", "type": "vae"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			createGenerateProfile(t, `{"schema_version":1,"name":"preset","generate":{"components":{"vae":"`+tc.selector+`"}}}`)
			envelope, enqueues := runProfileGeneration(t, append(animaModelInventory(), tc.additional...), `{"schema_version":1,"profile":"preset","model":"main-key","positive_prompt":"test","seed":42}`)
			data := envelope["data"].(map[string]any)
			settings := data["resolved_settings"].(map[string]any)
			warnings := envelope["warnings"].([]any)
			if enqueues != 1 || settings["component_keys"].(map[string]any)["vae"] != "vae-key" || len(warnings) != 2 || !reflect.DeepEqual(warnings, data["warnings"]) {
				t.Fatalf("enqueues=%d envelope=%#v", enqueues, envelope)
			}
			warning := warnings[0].(map[string]any)
			details := warning["details"].(map[string]any)
			if warning["code"] != "profile_preference_skipped" || details["profile"] != "preset" || details["component"] != "vae" || details["reason"] != tc.reason {
				t.Fatalf("warning=%#v", warning)
			}
		})
	}
}

func TestGenerateExplicitModelAndComponentOverrideProfile(t *testing.T) {
	isolateUserConfigDir(t)
	createGenerateProfile(t, `{"schema_version":1,"name":"preset","generate":{"model":"old-model","components":{"vae":"missing-vae"},"width":768,"height":512}}`)
	envelope, enqueues := runProfileGeneration(t, sdxlInventory(), `{"schema_version":1,"profile":"preset","model":"sdxl-main","positive_prompt":"test","width":512,"height":768,"components":{"vae":"sdxl-vae"},"seed":42}`)
	data := envelope["data"].(map[string]any)
	settings := data["resolved_settings"].(map[string]any)
	if enqueues != 1 || settings["model_key"] != "sdxl-main" || settings["width"] != float64(512) || settings["height"] != float64(768) || settings["component_keys"].(map[string]any)["vae"] != "sdxl-vae" || len(envelope["warnings"].([]any)) != 1 {
		t.Fatalf("enqueues=%d envelope=%#v", enqueues, envelope)
	}
}

func TestGenerateSkipsSDXLProfileVAEPrefAndUsesBundledVAE(t *testing.T) {
	isolateUserConfigDir(t)
	createGenerateProfile(t, `{"schema_version":1,"name":"preset","generate":{"components":{"vae":"missing-vae"}}}`)
	envelope, enqueues := runProfileGeneration(t, sdxlInventory(), `{"schema_version":1,"profile":"preset","model":"sdxl-main","positive_prompt":"test","seed":42}`)
	data := envelope["data"].(map[string]any)
	settings := data["resolved_settings"].(map[string]any)
	if enqueues != 1 || len(settings["component_keys"].(map[string]any)) != 0 || envelope["warnings"].([]any)[0].(map[string]any)["code"] != "profile_preference_skipped" {
		t.Fatalf("enqueues=%d envelope=%#v", enqueues, envelope)
	}
}

func TestGenerateProfileLocalValidationPrecedesNetwork(t *testing.T) {
	for _, tc := range []struct {
		name, profile, document, request, code string
	}{
		{"absent", "absent", "", `{"schema_version":1,"profile":"absent","positive_prompt":"test"}`, "not_found"},
		{"no generate section", "upscale_only", `{"schema_version":1,"name":"upscale_only","upscale":{}}`, `{"schema_version":1,"profile":"upscale_only","positive_prompt":"test"}`, "invalid_request"},
		{"no model", "empty", `{"schema_version":1,"name":"empty","generate":{}}`, `{"schema_version":1,"profile":"empty","positive_prompt":"test"}`, "invalid_request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			if tc.document != "" {
				createGenerateProfile(t, tc.document)
			}
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; http.NotFound(w, r) }))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			status := cli.NewWithIO(strings.NewReader(tc.request), &stdout, &stderr).Run(t.Context(), []string{"generate", "--request", "-", "--no-wait", "--url", server.URL, "--json"})
			var envelope map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if status == 0 || envelope["error"].(map[string]any)["code"] != tc.code || requests != 0 || stderr.Len() != 0 {
				t.Fatalf("status=%d envelope=%#v requests=%d stderr=%q", status, envelope, requests, stderr.String())
			}
		})
	}
}

func TestGenerateRejectsInapplicableProfileFieldsAfterModelResolution(t *testing.T) {
	for _, tc := range []struct {
		name, profile, model, field string
		inventory                   []map[string]any
	}{
		{"schnell guidance", `{"schema_version":1,"name":"preset","generate":{"guidance":4}}`, "flux-schnell", "guidance", fluxCLIInventory()},
		{"SDXL Qwen3", `{"schema_version":1,"name":"preset","generate":{"components":{"qwen3_encoder":"encoder-key"}}}`, "sdxl-main", "qwen3_encoder", sdxlInventory()},
		{"FLUX Qwen3", `{"schema_version":1,"name":"preset","generate":{"components":{"qwen3_encoder":"encoder-key"}}}`, "flux-dev", "qwen3_encoder", fluxCLIInventory()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			createGenerateProfile(t, tc.profile)
			enqueues := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), tc.inventory) {
					return
				}
				if r.URL.Path == "/api/v1/queue/default/enqueue_batch" {
					enqueues++
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			status := cli.New(&stdout, &stderr).Run(t.Context(), []string{"generate", "--no-wait", "--profile", "preset", "--model", tc.model, "--prompt", "test", "--url", server.URL, "--json"})
			var envelope map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if status != 2 || enqueues != 0 || stderr.Len() != 0 {
				t.Fatalf("status=%d enqueues=%d stderr=%q envelope=%#v", status, enqueues, stderr.String(), envelope)
			}
			errorData := envelope["error"].(map[string]any)
			if errorData["code"] != "invalid_request" {
				t.Fatalf("error=%#v", errorData)
			}
			details, ok := errorData["details"].(map[string]any)
			if !ok || details["source"] != "profile" || details["profile"] != "preset" || details["field"] != tc.field {
				t.Fatalf("details=%#v", errorData)
			}
		})
	}
}

func TestGenerateUsesProfileModelAndSettingsFromFlags(t *testing.T) {
	isolateUserConfigDir(t)
	createGenerateProfile(t, `{"schema_version":1,"name":"portrait","generate":{"model":"main-key","width":768,"height":512,"steps":22,"scheduler":"heun","guidance":5.5,"output_count":1}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveAnimaPreflight(w, r, "6.14.1", animaOpenAPIFixture("", ""), animaModelInventory()) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/queue/default/enqueue_batch":
			_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "enqueued": 1, "requested": 1, "item_ids": []int{17}, "batch": map[string]any{"batch_id": "profile-batch"}})
		case "/api/v1/recall/default":
			var patch map[string]any
			if err := json.UnmarshalRead(r.Body, &patch); err != nil {
				t.Error(err)
			}
			if patch["model"] != "Anima Main" || patch["width"] != float64(768) || patch["height"] != float64(512) || patch["steps"] != float64(22) || patch["seed"] != float64(42) {
				t.Errorf("Recall patch = %#v", patch)
			}
			_, _ = w.Write([]byte(`{"status":"success"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	status := cli.New(&stdout, &stderr).Run(t.Context(), []string{"generate", "--no-wait", "--profile", "portrait", "--prompt", "test", "--seed", "42", "--url", server.URL, "--json"})
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if status != 0 || stderr.Len() != 0 || envelope["ok"] != true {
		t.Fatalf("status=%d stderr=%q envelope=%#v", status, stderr.String(), envelope)
	}
	data := envelope["data"].(map[string]any)
	submitted := data["submitted_request"].(map[string]any)
	resolved := data["resolved_settings"].(map[string]any)
	if submitted["profile"] != "portrait" || resolved["profile"] != "portrait" || resolved["model_key"] != "main-key" || resolved["width"] != float64(768) || resolved["height"] != float64(512) || resolved["steps"] != float64(22) || resolved["scheduler"] != "heun" || resolved["guidance"] != float64(5.5) {
		t.Fatalf("submitted=%#v resolved=%#v", submitted, resolved)
	}
}

func TestGenerateProfilePrecedenceForEachFamily(t *testing.T) {
	for _, tc := range []struct {
		name, model      string
		inventory        []map[string]any
		defaultSteps     int
		defaultScheduler string
		defaultGuidance  float64
	}{
		{"Anima", "main-key", animaModelInventory(), 30, "euler", 4.5},
		{"SDXL", "sdxl-main", sdxlInventory(), 30, "dpmpp_3m_k", 7},
		{"FLUX.1 dev", "flux-dev", fluxCLIInventory(), 30, "euler", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			createGenerateProfile(t, `{"schema_version":1,"name":"model_only","generate":{"model":"`+tc.model+`"}}`)
			createGenerateProfile(t, `{"schema_version":1,"name":"settings","generate":{"model":"`+tc.model+`","width":768,"height":512,"steps":21,"scheduler":"heun","guidance":5,"output_count":2}}`)
			for _, run := range []struct {
				name, document              string
				width, height, steps, count int
				scheduler                   string
				guidance                    float64
			}{
				{"defaults", `{"schema_version":1,"profile":"model_only","positive_prompt":"test","seed":42}`, 1024, 1024, tc.defaultSteps, 1, tc.defaultScheduler, tc.defaultGuidance},
				{"profile", `{"schema_version":1,"profile":"settings","positive_prompt":"test","seed":42}`, 768, 512, 21, 2, "heun", 5},
				{"explicit", `{"schema_version":1,"profile":"settings","positive_prompt":"test","width":512,"height":768,"steps":19,"scheduler":"lcm","guidance":6,"output_count":1,"seed":42}`, 512, 768, 19, 1, "lcm", 6},
			} {
				t.Run(run.name, func(t *testing.T) {
					enqueues := 0
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), tc.inventory) {
							return
						}
						switch r.URL.Path {
						case "/api/v1/queue/default/enqueue_batch":
							enqueues++
							items := []int{17}
							if run.count == 2 {
								items = []int{17, 18}
							}
							_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "enqueued": run.count, "requested": run.count, "item_ids": items, "batch": map[string]any{"batch_id": "profile-batch"}})
						case "/api/v1/recall/default":
							_, _ = w.Write([]byte(`{"status":"success"}`))
						default:
							http.NotFound(w, r)
						}
					}))
					defer server.Close()
					var stdout, stderr bytes.Buffer
					status := cli.NewWithIO(strings.NewReader(run.document), &stdout, &stderr).Run(t.Context(), []string{"generate", "--no-wait", "--request", "-", "--url", server.URL, "--json"})
					var envelope map[string]any
					if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
						t.Fatal(err)
					}
					if status != 0 || enqueues != 1 || stderr.Len() != 0 {
						t.Fatalf("status=%d enqueues=%d stderr=%q envelope=%#v", status, enqueues, stderr.String(), envelope)
					}
					settings := envelope["data"].(map[string]any)["resolved_settings"].(map[string]any)
					if settings["model_key"] != tc.model || settings["width"] != float64(run.width) || settings["height"] != float64(run.height) || settings["steps"] != float64(run.steps) || settings["scheduler"] != run.scheduler || settings["guidance"] != run.guidance || settings["output_count"] != float64(run.count) {
						t.Fatalf("settings=%#v", settings)
					}
				})
			}
		})
	}
}
