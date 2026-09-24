package cli_test

import (
	"bytes"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

func runProfilesJSON(t *testing.T, stdin string, args ...string) (int, map[string]any, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(stdin), &stdout, &stderr)
	status := app.Run(t.Context(), append([]string{"profiles"}, append(args, "--json")...))
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout = %q, stderr = %q: %v", stdout.String(), stderr.String(), err)
	}
	return status, envelope, stderr.String()
}

func TestProfilesCreateAndGetDocument(t *testing.T) {
	isolateUserConfigDir(t)
	document := `{"schema_version":1,"name":"portrait_1","generate":{"model":"model-key","width":768,"height":512,"steps":20,"scheduler":"euler","guidance":4.5,"output_count":2,"components":{"vae":"vae-key"}}}`
	status, created, stderr := runProfilesJSON(t, document, "create", "--request", "-")
	if status != result.ExitSuccess || stderr != "" {
		t.Fatalf("create: status=%d stderr=%q result=%#v", status, stderr, created)
	}
	status, got, stderr := runProfilesJSON(t, "", "get", "portrait_1")
	if status != result.ExitSuccess || stderr != "" {
		t.Fatalf("get: status=%d stderr=%q result=%#v", status, stderr, got)
	}
	var want map[string]any
	if err := json.Unmarshal([]byte(document), &want); err != nil {
		t.Fatal(err)
	}
	for _, envelope := range []map[string]any{created, got} {
		if !reflect.DeepEqual(envelope["data"], want) {
			t.Fatalf("data = %#v, want %#v", envelope["data"], want)
		}
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(configDir, "bediz", "profiles", "portrait_1.json")); err != nil {
		t.Fatal(err)
	}
}

func TestProfilesCreateRejectsInvalidDocuments(t *testing.T) {
	cases := []struct{ name, document string }{
		{"invalid name", `{"schema_version":1,"name":"../escape","generate":{}}`},
		{"missing section", `{"schema_version":1,"name":"valid"}`},
		{"unpaired width", `{"schema_version":1,"name":"valid","generate":{"width":512}}`},
		{"nonpositive height", `{"schema_version":1,"name":"valid","generate":{"width":512,"height":0}}`},
		{"nonpositive steps", `{"schema_version":1,"name":"valid","generate":{"steps":0}}`},
		{"nonpositive output count", `{"schema_version":1,"name":"valid","generate":{"output_count":0}}`},
		{"low guidance", `{"schema_version":1,"name":"valid","generate":{"guidance":0.5}}`},
		{"unknown scheduler", `{"schema_version":1,"name":"valid","generate":{"scheduler":"unlisted"}}`},
		{"empty model", `{"schema_version":1,"name":"valid","upscale":{"model":""}}`},
		{"empty component", `{"schema_version":1,"name":"valid","generate":{"components":{"vae":""}}}`},
		{"bad scale", `{"schema_version":1,"name":"valid","upscale":{"scale":3}}`},
		{"bad creativity", `{"schema_version":1,"name":"valid","upscale":{"creativity":11}}`},
		{"bad structure", `{"schema_version":1,"name":"valid","upscale":{"structure":-11}}`},
		{"bad tile size", `{"schema_version":1,"name":"valid","upscale":{"tile_size":513}}`},
		{"bad tile overlap", `{"schema_version":1,"name":"valid","upscale":{"tile_overlap":15,"tile_size":512}}`},
		{"top-level prompt", `{"schema_version":1,"name":"valid","positive_prompt":"secret","generate":{}}`},
		{"generate prompt", `{"schema_version":1,"name":"valid","generate":{"positive_prompt":"secret"}}`},
		{"negative prompt", `{"schema_version":1,"name":"valid","generate":{"negative_prompt":"secret"}}`},
		{"seed", `{"schema_version":1,"name":"valid","generate":{"seed":1}}`},
		{"board", `{"schema_version":1,"name":"valid","upscale":{"board_id":"board"}}`},
		{"source", `{"schema_version":1,"name":"valid","upscale":{"source":"image"}}`},
		{"token", `{"schema_version":1,"name":"valid","generate":{"components":{"token":"secret"}}}`},
		{"wrong case", `{"Schema_version":1,"name":"valid","generate":{}}`},
		{"duplicate", `{"schema_version":1,"name":"valid","name":"valid","generate":{}}`},
		{"null", `{"schema_version":1,"name":"valid","generate":{"model":null}}`},
		{"extra value", `{"schema_version":1,"name":"valid","generate":{}} {}`},
		{"array document", `[{"schema_version":1,"name":"valid","generate":{}}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			status, envelope, stderr := runProfilesJSON(t, tc.document, "create", "--request", "-")
			if status != result.ExitInvalidRequest || stderr != "" || envelope["error"].(map[string]any)["code"] != result.CodeInvalidRequest {
				t.Fatalf("status=%d stderr=%q result=%#v", status, stderr, envelope)
			}
		})
	}
}

func TestProfilesListSortsNamesAndReportsSections(t *testing.T) {
	isolateUserConfigDir(t)
	for _, document := range []string{
		`{"schema_version":1,"name":"zeta","upscale":{}}`,
		`{"schema_version":1,"name":"alpha","generate":{},"upscale":{}}`,
		`{"schema_version":1,"name":"portrait-hd","generate":{}}`,
		`{"schema_version":1,"name":"portrait","generate":{}}`,
	} {
		status, envelope, stderr := runProfilesJSON(t, document, "create", "--request", "-")
		if status != result.ExitSuccess || stderr != "" {
			t.Fatalf("create: status=%d stderr=%q result=%#v", status, stderr, envelope)
		}
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bediz", "profiles", "Bad.json"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, envelope, stderr := runProfilesJSON(t, "", "list")
	want := []any{
		map[string]any{"name": "alpha", "sections": []any{"generate", "upscale"}},
		map[string]any{"name": "portrait", "sections": []any{"generate"}},
		map[string]any{"name": "portrait-hd", "sections": []any{"generate"}},
		map[string]any{"name": "zeta", "sections": []any{"upscale"}},
	}
	if status != result.ExitSuccess || stderr != "" || !reflect.DeepEqual(envelope["data"], map[string]any{"profiles": want}) {
		t.Fatalf("list: status=%d stderr=%q result=%#v", status, stderr, envelope)
	}
}

func TestProfilesReplacementRequiresFlagsAndPreservesOldProfileOnWriteFailure(t *testing.T) {
	isolateUserConfigDir(t)
	first := `{"schema_version":1,"name":"preset","generate":{"steps":10}}`
	second := `{"schema_version":1,"name":"preset","generate":{"steps":20}}`
	if status, envelope, stderr := runProfilesJSON(t, first, "create", "--request", "-"); status != 0 || stderr != "" {
		t.Fatalf("initial create: status=%d stderr=%q result=%#v", status, stderr, envelope)
	}
	for _, flags := range [][]string{nil, {"--yes"}, {"--replace"}} {
		args := append([]string{"create", "--request", "-"}, flags...)
		status, envelope, stderr := runProfilesJSON(t, second, args...)
		if status != result.ExitInvalidRequest || stderr != "" {
			t.Fatalf("replacement flags %v: status=%d stderr=%q result=%#v", flags, status, stderr, envelope)
		}
		if len(flags) == 0 || flags[0] == "--yes" {
			if envelope["error"].(map[string]any)["details"].(map[string]any)["reason"] != "profile_exists" {
				t.Fatalf("missing profile_exists reason: %#v", envelope)
			}
		}
	}
	status, envelope, _ := runProfilesJSON(t, "", "get", "preset")
	if status != 0 || envelope["data"].(map[string]any)["generate"].(map[string]any)["steps"] != float64(10) {
		t.Fatalf("profile changed before approval: %#v", envelope)
	}
	status, envelope, stderr := runProfilesJSON(t, second, "create", "--request", "-", "--replace", "--yes")
	if status != 0 || stderr != "" || envelope["data"].(map[string]any)["generate"].(map[string]any)["steps"] != float64(20) {
		t.Fatalf("approved replacement: status=%d stderr=%q result=%#v", status, stderr, envelope)
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(configDir, "bediz", "profiles")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	// Root and Windows ignore these mode bits, so the write cannot be made to fail there.
	if probe, err := os.CreateTemp(dir, "probe-*"); err == nil {
		_ = probe.Close()
		_ = os.Remove(probe.Name())
		return
	}
	status, envelope, stderr = runProfilesJSON(t, first, "create", "--request", "-", "--replace", "--yes")
	if status != result.ExitInvalidRequest || stderr != "" || envelope["error"].(map[string]any)["code"] != result.CodeConfigurationWriteFailed {
		t.Fatalf("failed replacement: status=%d stderr=%q result=%#v", status, stderr, envelope)
	}
	status, envelope, _ = runProfilesJSON(t, "", "get", "preset")
	if status != 0 || envelope["data"].(map[string]any)["generate"].(map[string]any)["steps"] != float64(20) {
		t.Fatalf("failed write changed profile: %#v", envelope)
	}
}

func TestProfilesDeleteRequiresApprovalAndRemovesOnlyNamedProfile(t *testing.T) {
	isolateUserConfigDir(t)
	for _, name := range []string{"keep", "remove"} {
		document := `{"schema_version":1,"name":"` + name + `","generate":{}}`
		if status, envelope, stderr := runProfilesJSON(t, document, "create", "--request", "-"); status != 0 || stderr != "" {
			t.Fatalf("create: status=%d stderr=%q result=%#v", status, stderr, envelope)
		}
	}
	status, envelope, stderr := runProfilesJSON(t, "", "delete", "remove")
	if status != result.ExitInvalidRequest || stderr != "" || envelope["error"].(map[string]any)["code"] != result.CodeInvalidRequest {
		t.Fatalf("unapproved delete: status=%d stderr=%q result=%#v", status, stderr, envelope)
	}
	status, envelope, stderr = runProfilesJSON(t, "", "delete", "remove", "--yes")
	if status != 0 || stderr != "" || !reflect.DeepEqual(envelope["data"], map[string]any{"name": "remove"}) {
		t.Fatalf("approved delete: status=%d stderr=%q result=%#v", status, stderr, envelope)
	}
	status, envelope, _ = runProfilesJSON(t, "", "get", "remove")
	if status != result.ExitInvokeAIFailure || envelope["error"].(map[string]any)["code"] != result.CodeNotFound {
		t.Fatalf("deleted profile found: status=%d result=%#v", status, envelope)
	}
	status, envelope, _ = runProfilesJSON(t, "", "get", "keep")
	if status != 0 || envelope["data"].(map[string]any)["name"] != "keep" {
		t.Fatalf("other profile changed: status=%d result=%#v", status, envelope)
	}
	status, envelope, _ = runProfilesJSON(t, "", "delete", "remove", "--yes")
	if status != result.ExitInvokeAIFailure || envelope["error"].(map[string]any)["code"] != result.CodeNotFound {
		t.Fatalf("missing delete: status=%d result=%#v", status, envelope)
	}
}

func TestProfilesGetAndListReportCorruptStoredProfile(t *testing.T) {
	cases := []struct{ name, content string }{
		{"mismatched name", `{"schema_version":1,"name":"elsewhere","generate":{}}`},
		{"unknown field", `{"schema_version":1,"name":"corrupt","generate":{"seed":42}}`},
		{"duplicate", `{"schema_version":1,"name":"corrupt","name":"corrupt","generate":{}}`},
		{"null", `{"schema_version":1,"name":"corrupt","generate":{"model":null}}`},
		{"missing section", `{"schema_version":1,"name":"corrupt"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigDir(t)
			configDir, err := os.UserConfigDir()
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(configDir, "bediz", "profiles")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"get", "corrupt"}, {"list"}} {
				status, envelope, stderr := runProfilesJSON(t, "", args...)
				failure, ok := envelope["error"].(map[string]any)
				if !ok {
					t.Fatalf("%v: expected error, got status=%d result=%#v", args, status, envelope)
				}
				if status != result.ExitInvalidRequest || stderr != "" || failure["code"] != result.CodeInvalidConfiguration || !strings.Contains(failure["message"].(string), "corrupt") {
					t.Fatalf("%v: status=%d stderr=%q result=%#v", args, status, stderr, envelope)
				}
			}
		})
	}
}

func TestProfilesPreserveOptionalUpscaleValuesAndNeverContactInvokeAI(t *testing.T) {
	isolateUserConfigDir(t)
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer server.Close()
	t.Setenv("BEDIZ_URL", server.URL)
	document := `{"schema_version":1,"name":"upscale_1","upscale":{"model":"main","components":{},"scale":4,"creativity":0,"structure":0,"steps":30,"scheduler":"kdpm_2","guidance":2,"tile_size":1024,"tile_overlap":128}}`
	path := filepath.Join(t.TempDir(), "profile.json")
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	status, created, stderr := runProfilesJSON(t, "", "create", "--request", path)
	if status != 0 || stderr != "" {
		t.Fatalf("create: status=%d stderr=%q result=%#v", status, stderr, created)
	}
	status, got, stderr := runProfilesJSON(t, "", "get", "upscale_1")
	if status != 0 || stderr != "" {
		t.Fatalf("get: status=%d stderr=%q result=%#v", status, stderr, got)
	}
	var want map[string]any
	if err := json.Unmarshal([]byte(document), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(created["data"], want) || !reflect.DeepEqual(got["data"], want) {
		t.Fatalf("created=%#v got=%#v want=%#v", created["data"], got["data"], want)
	}
	if requests != 0 {
		t.Fatalf("made %d InvokeAI requests", requests)
	}
}

func TestProfilesRejectPathLikeNamesBeforeAccess(t *testing.T) {
	isolateUserConfigDir(t)
	for _, args := range [][]string{{"get", "../outside"}, {"delete", "../outside", "--yes"}} {
		status, envelope, stderr := runProfilesJSON(t, "", args...)
		if status != result.ExitInvalidRequest || stderr != "" || envelope["error"].(map[string]any)["code"] != result.CodeInvalidRequest {
			t.Fatalf("%v: status=%d stderr=%q result=%#v", args, status, stderr, envelope)
		}
	}
}

func TestProfilesGetHumanOutputShowsDocument(t *testing.T) {
	isolateUserConfigDir(t)
	document := `{"schema_version":1,"name":"human","generate":{"steps":7}}`
	if status, envelope, stderr := runProfilesJSON(t, document, "create", "--request", "-"); status != 0 || stderr != "" {
		t.Fatalf("create: status=%d stderr=%q result=%#v", status, stderr, envelope)
	}
	var stdout, stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(""), &stdout, &stderr)
	status := app.Run(t.Context(), []string{"profiles", "get", "human"})
	var got map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("human get output = %q: %v", stdout.String(), err)
	}
	var want map[string]any
	if err := json.Unmarshal([]byte(document), &want); err != nil {
		t.Fatal(err)
	}
	if status != 0 || stderr.Len() != 0 || !reflect.DeepEqual(got, want) {
		t.Fatalf("status=%d stderr=%q got=%#v want=%#v", status, stderr.String(), got, want)
	}
}
