package e2e_test

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The revision is pinned so the live gate always submits the same small,
// public SD1 LoRA that InvokeAI 6.14.1 recognizes.
const modelInstallFixtureURL = "https://huggingface.co/KuroiMatoO/pruned_loras_test/resolve/d6b11228d2d4ce64ce1ee4cd822b00f33f65d674/abmono4_minify.safetensors"

type installJobData struct {
	JobID      *int   `json:"job_id"`
	Status     string `json:"status"`
	SourceType string `json:"source_type"`
	Role       string `json:"role"`
}

type installResultData struct {
	Jobs []installJobData `json:"jobs"`
}

type installStatusData struct {
	JobID      *int   `json:"job_id"`
	Status     string `json:"status"`
	Bytes      *int64 `json:"bytes"`
	TotalBytes *int64 `json:"total_bytes"`
	ModelKey   string `json:"model_key"`
}

// TestLiveURLModelInstall is a separate opt-in gate because it temporarily
// installs a model in the configured InvokeAI instance.
func TestLiveURLModelInstall(t *testing.T) {
	target := strings.TrimSpace(os.Getenv("BEDIZ_E2E_URL"))
	if target == "" || os.Getenv("BEDIZ_E2E_MODEL_INSTALL") != "1" {
		t.Skip("live model install: NOT REQUESTED (set BEDIZ_E2E_URL and BEDIZ_E2E_MODEL_INSTALL=1)")
	}
	validateTarget(t, target)
	binary := buildBinary(t)
	doctor := runJSONCommand(t, binary, target, "doctor")
	assertSuccessEnvelope(t, doctor, "doctor")
	var readiness doctorData
	unmarshalData(t, doctor.Data, &readiness)
	if readiness.InvokeAI.Version != "6.14.1" {
		t.Fatalf("InvokeAI version = %q, want tested baseline 6.14.1", readiness.InvokeAI.Version)
	}
	for _, operation := range []string{"models.install", "models.status"} {
		compatible := false
		for _, capability := range readiness.Capabilities {
			if capability.Operation == operation && capability.Compatible != nil && *capability.Compatible {
				compatible = true
			}
		}
		if !compatible {
			t.Fatalf("doctor did not verify %s capability", operation)
		}
	}

	source := modelInstallFixtureURL
	assertNoModelWithSource(t, target, source)
	previousJobs := fixtureJobIDs(t, target, source)
	t.Cleanup(func() { cleanupInstalledFixture(t, target, source, previousJobs) })

	accepted := runJSONCommand(t, binary, target, "models", "install", "--source-type", "url", "--source", source)
	assertSuccessEnvelope(t, accepted, "models.install")
	var install installResultData
	unmarshalData(t, accepted.Data, &install)
	if len(install.Jobs) != 1 || install.Jobs[0].JobID == nil || *install.Jobs[0].JobID < 0 || install.Jobs[0].SourceType != "url" || install.Jobs[0].Role != "requested" || install.Jobs[0].Status == "" {
		t.Fatalf("install did not return one observable URL job: %#v", install)
	}
	if bytes.Contains(accepted.Data, []byte(source)) || bytes.Contains(accepted.Data, []byte(secretSentinel)) {
		t.Fatal("install result exposed its source or connection token")
	}
	jobID := *install.Jobs[0].JobID
	t.Logf("submitted test model install job %d", jobID)
	deadline := time.Now().Add(90 * time.Second)
	for {
		statusEnvelope := runJSONCommand(t, binary, target, "models", "status", "--job-id", strconv.Itoa(jobID))
		assertSuccessEnvelope(t, statusEnvelope, "models.status")
		var state installStatusData
		unmarshalData(t, statusEnvelope.Data, &state)
		if state.JobID == nil || *state.JobID != jobID || bytes.Contains(statusEnvelope.Data, []byte(source)) || bytes.Contains(statusEnvelope.Data, []byte(secretSentinel)) {
			t.Fatalf("status did not safely identify current job %d: %#v", jobID, state)
		}
		switch state.Status {
		case "completed":
			if state.ModelKey == "" {
				t.Fatalf("completed job lacked an exact model key: %#v", state)
			}
			assertInstalledFixture(t, binary, target, source, state.ModelKey)
			t.Logf("live model installation verified: job_id=%d model_key=%s", jobID, state.ModelKey)
			return
		case "error", "cancelled":
			t.Fatalf("test model install ended with status %q", state.Status)
		case "waiting", "downloading", "downloads_done", "running", "paused":
			if time.Now().After(deadline) {
				t.Fatalf("test model install job %d did not complete within 90 seconds; last status %q", jobID, state.Status)
			}
			time.Sleep(250 * time.Millisecond)
		default:
			t.Fatalf("unrecognized test model install status %q", state.Status)
		}
	}
}

type backendModelRecord struct {
	Key        string `json:"key"`
	Name       string `json:"name"`
	Source     string `json:"source"`
	SourceType string `json:"source_type"`
}

func fixtureModels(t *testing.T, target, source string) []backendModelRecord {
	t.Helper()
	response := requestModelBackend(t, context.WithoutCancel(t.Context()), http.MethodGet, target, "/api/v2/models/")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("inspect model inventory: GET returned %s", response.Status)
	}
	var inventory struct {
		Models []backendModelRecord `json:"models"`
	}
	if err := json.Unmarshal(response.Body, &inventory); err != nil {
		t.Fatalf("decode model inventory: %v", err)
	}
	var matches []backendModelRecord
	for _, model := range inventory.Models {
		if model.Source == source {
			matches = append(matches, model)
		}
	}
	return matches
}

func assertNoModelWithSource(t *testing.T, target, source string) {
	t.Helper()
	if models := fixtureModels(t, target, source); len(models) != 0 {
		t.Fatalf("test source is already installed: %#v", models)
	}
}

func assertInstalledFixture(t *testing.T, binaryPath, target, source, key string) {
	t.Helper()
	models := fixtureModels(t, target, source)
	if len(models) != 1 || models[0].Key != key || models[0].Name == "" || models[0].SourceType != "url" {
		t.Fatalf("completed job does not identify the expected installed fixture: %#v", models)
	}
	envelope := runJSONCommand(t, binaryPath, target, "models", "list")
	assertSuccessEnvelope(t, envelope, "models.list")
	var listed modelsListData
	unmarshalData(t, envelope.Data, &listed)
	found := false
	for _, model := range listed.Models {
		if model.Key == key && model.Name == models[0].Name && model.Base == "sd-1" && model.Type == "lora" {
			found = true
		}
	}
	if !found {
		t.Fatalf("models list did not contain completed SD1 LoRA %q", key)
	}
}

type backendInstallJob struct {
	ID        int    `json:"id"`
	Status    string `json:"status"`
	ConfigOut *struct {
		Key string `json:"key"`
	} `json:"config_out"`
	Source struct {
		URL string `json:"url"`
	} `json:"source"`
}

func fixtureJobIDs(t *testing.T, target, source string) map[int]bool {
	t.Helper()
	jobs := fixtureJobs(t, t.Context(), target, source)
	ids := make(map[int]bool, len(jobs))
	for _, job := range jobs {
		if job.Status != "completed" && job.Status != "error" && job.Status != "cancelled" {
			t.Fatalf("another install job is already active for the test source: %d", job.ID)
		}
		ids[job.ID] = true
	}
	return ids
}

func fixtureJobs(t *testing.T, ctx context.Context, target, source string) []backendInstallJob {
	t.Helper()
	response := requestModelBackend(t, ctx, http.MethodGet, target, "/api/v2/models/install")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("inspect install jobs: GET returned %s", response.Status)
	}
	var jobs []backendInstallJob
	if err := json.Unmarshal(response.Body, &jobs); err != nil {
		t.Fatalf("decode install job list: %v", err)
	}
	var matches []backendInstallJob
	for _, job := range jobs {
		if job.Source.URL == source {
			matches = append(matches, job)
		}
	}
	return matches
}

func cleanupInstalledFixture(t *testing.T, target, source string, previousJobs map[int]bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 30*time.Second)
	defer cancel()
	installedKeys := make(map[string]bool)
	for _, job := range fixtureJobs(t, ctx, target, source) {
		if previousJobs[job.ID] {
			continue
		}
		if job.Status != "completed" && job.Status != "error" && job.Status != "cancelled" {
			cancelResponse := requestModelBackend(t, ctx, http.MethodDelete, target, "/api/v2/models/install/"+strconv.Itoa(job.ID))
			if cancelResponse.StatusCode != http.StatusCreated {
				t.Errorf("cleanup: cancelling test job %d returned %s", job.ID, cancelResponse.Status)
				return
			}
			for {
				current := requestModelBackend(t, ctx, http.MethodGet, target, "/api/v2/models/install/"+strconv.Itoa(job.ID))
				if current.StatusCode != http.StatusOK {
					t.Errorf("cleanup: inspecting test job %d returned %s", job.ID, current.Status)
					return
				}
				var state backendInstallJob
				if err := json.Unmarshal(current.Body, &state); err != nil {
					t.Errorf("cleanup: decoding test job %d: %v", job.ID, err)
					return
				}
				if state.Status == "completed" || state.Status == "error" || state.Status == "cancelled" {
					job = state
					break
				}
				if ctx.Err() != nil {
					t.Errorf("cleanup: test job %d did not stop: %v", job.ID, ctx.Err())
					return
				}
				time.Sleep(250 * time.Millisecond)
			}
		}
		if job.ConfigOut != nil && job.ConfigOut.Key != "" {
			installedKeys[job.ConfigOut.Key] = true
		}
	}
	models := fixtureModels(t, target, source)
	for _, model := range models {
		if !installedKeys[model.Key] {
			t.Errorf("cleanup: model %q matches the test source but is not tied to a test-created job", model.Key)
			continue
		}
		endpoint := "/api/v2/models/i/" + url.PathEscape(model.Key)
		deleted := requestModelBackend(t, ctx, http.MethodDelete, target, endpoint)
		if deleted.StatusCode != http.StatusNoContent {
			t.Errorf("cleanup: deleting test model %q returned %s", model.Key, deleted.Status)
			return
		}
		t.Logf("removed test model %s", model.Key)
	}
	assertNoModelWithSource(t, target, source)
}

func requestModelBackend(t *testing.T, ctx context.Context, method, target, path string) backendResponse {
	t.Helper()
	endpoint, err := url.JoinPath(target, path)
	if err != nil {
		t.Fatalf("build model backend URL: %v", err)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		t.Fatalf("build model backend request: %v", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+secretSentinel)
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		t.Fatalf("model backend request failed: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil || len(body) > 1<<20 {
		t.Fatalf("read model backend response: %v", err)
	}
	return backendResponse{StatusCode: response.StatusCode, Status: response.Status, Body: body}
}
