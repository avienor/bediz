package cli_test

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

func TestGenerateProfileApplicabilityPreservesErrorContract(t *testing.T) {
	inventory := append(animaModelInventory(), sdxlInventory()...)
	inventory = append(inventory, fluxCLIInventory()...)
	for _, tc := range []struct {
		name, settings, model, field string
	}{
		{"Anima T5", `{"components":{"t5_encoder":"encoder"}}`, "main-key", "t5_encoder"},
		{"Anima CLIP", `{"components":{"clip_embed":"encoder"}}`, "main-key", "clip_embed"},
		{"Anima width", `{"width":1020,"height":1024}`, "main-key", "width"},
		{"Anima height", `{"width":1024,"height":1020}`, "main-key", "height"},
		{"Anima scheduler", `{"scheduler":"ddim"}`, "main-key", "scheduler"},
		{"SDXL Qwen3", `{"components":{"qwen3_encoder":"encoder"}}`, "sdxl-main", "qwen3_encoder"},
		{"SDXL T5", `{"components":{"t5_encoder":"encoder"}}`, "sdxl-main", "t5_encoder"},
		{"SDXL CLIP", `{"components":{"clip_embed":"encoder"}}`, "sdxl-main", "clip_embed"},
		{"SDXL width", `{"width":1020,"height":1024}`, "sdxl-main", "width"},
		{"SDXL height", `{"width":1024,"height":1020}`, "sdxl-main", "height"},
		{"FLUX Qwen3", `{"components":{"qwen3_encoder":"encoder"}}`, "flux-dev", "qwen3_encoder"},
		{"FLUX width", `{"width":520,"height":512}`, "flux-dev", "width"},
		{"FLUX height", `{"width":512,"height":520}`, "flux-dev", "height"},
		{"FLUX dev scheduler", `{"scheduler":"dpmpp_2m"}`, "flux-dev", "scheduler"},
		{"FLUX schnell scheduler", `{"scheduler":"dpmpp_2m"}`, "flux-schnell", "scheduler"},
		{"FLUX schnell guidance", `{"guidance":4}`, "flux-schnell", "guidance"},
		{"component precedes dimensions", `{"components":{"t5_encoder":"encoder"},"width":1020,"height":1020}`, "sdxl-main", "t5_encoder"},
		{"width precedes height", `{"width":520,"height":520}`, "flux-dev", "width"},
		{"guidance precedes scheduler", `{"guidance":4,"scheduler":"ddim"}`, "flux-schnell", "guidance"},
	} {
		for _, mode := range []string{"txt2img", "img2img"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				isolateUserConfigDir(t)
				createGenerateProfile(t, fmt.Sprintf(`{"schema_version":1,"name":"preset","generate":%s}`, tc.settings))
				var mutations int
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if serveAnimaPreflight(w, r, "6.14.1", sdxlOpenAPIFixture(t), inventory) {
						return
					}
					if r.Method == http.MethodPost {
						mutations++
					}
					http.NotFound(w, r)
				}))
				defer server.Close()
				// Explicit values cannot make an inapplicable saved setting valid.
				args := []string{"generate", "--no-wait", "--profile", "preset", "--model", tc.model, "--prompt", "test",
					"--width", "1024", "--height", "1024", "--scheduler", "lcm", "--url", server.URL, "--json"}
				if mode == "img2img" {
					args = append(args, "--image", "source.png")
				}
				var stdout, stderr bytes.Buffer
				status := cli.New(&stdout, &stderr).Run(t.Context(), args)
				var envelope result.Envelope
				if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				want := &result.Error{
					Code:    result.CodeInvalidRequest,
					Message: fmt.Sprintf("profile %q setting %q is not applicable to the selected model", "preset", tc.field),
					Details: map[string]any{"source": "profile", "profile": "preset", "field": tc.field},
				}
				if status != result.ExitInvalidRequest || envelope.OK || envelope.Operation != result.OperationGenerate ||
					!reflect.DeepEqual(envelope.Error, want) || stderr.Len() != 0 || mutations != 0 {
					t.Fatalf("status=%d result=%#v error=%#v stderr=%q mutations=%d, want error %#v", status, envelope, envelope.Error, stderr.String(), mutations, want)
				}
			})
		}
	}
}
