package e2e_test

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

const secretSentinel = "bediz-live-e2e-secret-sentinel"

type successEnvelope struct {
	SchemaVersion int            `json:"schema_version"`
	OK            bool           `json:"ok"`
	Operation     string         `json:"operation"`
	Data          jsontext.Value `json:"data"`
	Error         jsontext.Value `json:"error"`
	Warnings      jsontext.Value `json:"warnings"`
}

type doctorData struct {
	Ready *bool `json:"ready"`
	Bediz struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
		Date    string `json:"date"`
	} `json:"bediz"`
	InvokeAI struct {
		URL                  string `json:"url"`
		Version              string `json:"version"`
		SupportedVersion     *bool  `json:"supported_version"`
		SupportedRange       string `json:"supported_range"`
		ConnectionStatus     string `json:"connection_status"`
		AuthenticationStatus string `json:"authentication_status"`
		TokenConfigured      *bool  `json:"token_configured"`
	} `json:"invokeai"`
	OpenAPI struct {
		Available         *bool `json:"available"`
		RequiredEndpoints []struct {
			Method    string `json:"method"`
			Path      string `json:"path"`
			Available *bool  `json:"available"`
		} `json:"required_endpoints"`
		RequiredInvocations []struct {
			Schema            string   `json:"schema"`
			Type              string   `json:"type"`
			Available         *bool    `json:"available"`
			MissingProperties []string `json:"missing_properties"`
		} `json:"required_invocations"`
	} `json:"openapi"`
	Models struct {
		Available *bool `json:"available"`
		Total     *int  `json:"total"`
		Relevant  []struct {
			Key    string `json:"key"`
			Name   string `json:"name"`
			Base   string `json:"base"`
			Type   string `json:"type"`
			Format string `json:"format"`
		} `json:"relevant"`
		Requirements []struct {
			Name      string `json:"name"`
			Available *int   `json:"available"`
			Required  *int   `json:"required"`
			Satisfied *bool  `json:"satisfied"`
		} `json:"requirements"`
	} `json:"models"`
	Capabilities []struct {
		Operation  string   `json:"operation"`
		Family     string   `json:"family"`
		Compatible *bool    `json:"compatible"`
		UISync     string   `json:"ui_sync"`
		Failures   []string `json:"failures"`
	} `json:"capabilities"`
	UISync map[string]string `json:"ui_sync"`
	Issues []struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details jsontext.Value `json:"details"`
	} `json:"issues"`
}

type modelsListData struct {
	Models []struct {
		Key         string  `json:"key"`
		Name        string  `json:"name"`
		Base        string  `json:"base"`
		Type        string  `json:"type"`
		Format      string  `json:"format"`
		SizeBytes   *int64  `json:"size_bytes"`
		Description *string `json:"description"`
	} `json:"models"`
}

type pageMetadata struct {
	Offset *int `json:"offset"`
	Limit  *int `json:"limit"`
	Total  *int `json:"total"`
}

type imagesListData struct {
	pageMetadata
	Items []struct {
		ImageName      string  `json:"image_name"`
		ImageURL       string  `json:"image_url"`
		ThumbnailURL   string  `json:"thumbnail_url"`
		ImageOrigin    string  `json:"image_origin"`
		ImageCategory  string  `json:"image_category"`
		Width          int     `json:"width"`
		Height         int     `json:"height"`
		CreatedAt      string  `json:"created_at"`
		UpdatedAt      string  `json:"updated_at"`
		IsIntermediate *bool   `json:"is_intermediate"`
		SessionID      *string `json:"session_id"`
		NodeID         *string `json:"node_id"`
		Starred        *bool   `json:"starred"`
		HasWorkflow    *bool   `json:"has_workflow"`
		BoardID        *string `json:"board_id"`
	} `json:"items"`
}

type queueListData struct {
	pageMetadata
	Items []struct {
		ItemID       int     `json:"item_id"`
		QueueID      string  `json:"queue_id"`
		Status       string  `json:"status"`
		BatchID      string  `json:"batch_id"`
		Origin       *string `json:"origin"`
		Destination  *string `json:"destination"`
		CreatedAt    string  `json:"created_at"`
		StartedAt    *string `json:"started_at"`
		CompletedAt  *string `json:"completed_at"`
		Device       *string `json:"device"`
		ParentItemID *int    `json:"parent_item_id"`
	} `json:"items"`
}

func TestLiveReadOnlyGate(t *testing.T) {
	target := strings.TrimSpace(os.Getenv("BEDIZ_E2E_URL"))
	if target == "" {
		t.Skip("live read-only E2E: NOT REQUESTED (BEDIZ_E2E_URL is unset)")
	}
	validateTarget(t, target)
	binary := buildBinary(t)

	if !t.Run("doctor verifies the supported baseline", func(t *testing.T) {
		envelope := runJSONCommand(t, binary, target, "doctor")
		assertSuccessEnvelope(t, envelope, "doctor")
		var data doctorData
		unmarshalData(t, envelope.Data, &data)
		if data.Ready == nil || !*data.Ready || data.InvokeAI.SupportedVersion == nil || !*data.InvokeAI.SupportedVersion || data.InvokeAI.TokenConfigured == nil || !*data.InvokeAI.TokenConfigured {
			t.Fatalf("doctor did not verify a supported ready target: %#v", data.InvokeAI)
		}
		if data.InvokeAI.Version != "6.14.1" {
			t.Fatalf("InvokeAI version = %q, want supported baseline %q", data.InvokeAI.Version, "6.14.1")
		}
		if data.InvokeAI.SupportedRange != ">= 6.14.1, < 6.15.0" {
			t.Fatalf("supported range = %q, want %q", data.InvokeAI.SupportedRange, ">= 6.14.1, < 6.15.0")
		}
		if data.OpenAPI.Available == nil || !*data.OpenAPI.Available || data.Models.Available == nil || !*data.Models.Available || data.Models.Total == nil || len(data.OpenAPI.RequiredEndpoints) == 0 || data.OpenAPI.RequiredInvocations == nil || data.Models.Relevant == nil || data.Models.Requirements == nil || data.Capabilities == nil || data.UISync == nil || data.Issues == nil || len(data.Issues) != 0 {
			t.Fatalf("doctor readiness details are incomplete: %#v", data)
		}
		if data.Bediz.Version == "" || data.Bediz.Commit == "" || data.Bediz.Date == "" || data.InvokeAI.URL == "" || data.InvokeAI.ConnectionStatus != "ok" || data.InvokeAI.AuthenticationStatus != "accepted" || *data.Models.Total < 0 {
			t.Fatalf("doctor returned incomplete normalized values: %#v", data)
		}
		for _, endpoint := range data.OpenAPI.RequiredEndpoints {
			if endpoint.Method == "" || endpoint.Path == "" || endpoint.Available == nil || !*endpoint.Available {
				t.Errorf("doctor returned an unavailable or incomplete endpoint check: %#v", endpoint)
			}
		}
		for _, invocation := range data.OpenAPI.RequiredInvocations {
			if invocation.Schema == "" || invocation.Type == "" || invocation.Available == nil || !*invocation.Available || invocation.MissingProperties == nil || len(invocation.MissingProperties) != 0 {
				t.Errorf("doctor returned an unavailable or incomplete invocation check: %#v", invocation)
			}
		}
		for _, model := range data.Models.Relevant {
			if model.Key == "" || model.Name == "" || model.Base == "" || model.Type == "" {
				t.Errorf("doctor returned an incomplete relevant model: %#v", model)
			}
		}
		for _, requirement := range data.Models.Requirements {
			if requirement.Name == "" || requirement.Available == nil || requirement.Required == nil || requirement.Satisfied == nil || !*requirement.Satisfied {
				t.Errorf("doctor returned an unsatisfied or incomplete model requirement: %#v", requirement)
			}
		}
		wantOperations := []string{"images.get", "images.list", "images.upload", "models.list", "queue.get", "queue.list"}
		operations := make([]string, 0, len(data.Capabilities))
		for _, capability := range data.Capabilities {
			if capability.Compatible == nil || !*capability.Compatible || capability.Failures == nil || len(capability.Failures) != 0 {
				t.Errorf("doctor reported an incompatible capability: %#v", capability)
			}
			operations = append(operations, capability.Operation)
		}
		slices.Sort(operations)
		if !slices.Equal(operations, wantOperations) {
			t.Errorf("doctor operations = %v, want exact implemented surface %v", operations, wantOperations)
		}
	}) {
		return
	}

	if !t.Run("model listing is normalized", func(t *testing.T) {
		envelope := runJSONCommand(t, binary, target, "models", "list")
		assertSuccessEnvelope(t, envelope, "models.list")
		var data modelsListData
		unmarshalData(t, envelope.Data, &data)
		if data.Models == nil {
			t.Fatal("models list is null, want an array")
		}
		for index, model := range data.Models {
			if model.Key == "" || model.Name == "" || model.Base == "" || model.Type == "" || model.Format == "" {
				t.Errorf("model %d is not a normalized summary: %#v", index, model)
			}
			if model.SizeBytes != nil && *model.SizeBytes < 0 {
				t.Errorf("model %d has negative size: %d", index, *model.SizeBytes)
			}
		}
	}) {
		return
	}

	if !t.Run("image listing is normalized and bounded", func(t *testing.T) {
		envelope := runJSONCommand(t, binary, target, "images", "list", "--limit", "1")
		assertSuccessEnvelope(t, envelope, "images.list")
		var data imagesListData
		unmarshalData(t, envelope.Data, &data)
		if data.Items == nil {
			t.Fatal("images list is null, want an array")
		}
		assertPageBounds(t, data.pageMetadata, len(data.Items))
		for index, image := range data.Items {
			if image.ImageName == "" || image.ImageURL == "" || image.ThumbnailURL == "" || image.ImageOrigin == "" || image.ImageCategory == "" || image.Width < 1 || image.Height < 1 || image.CreatedAt == "" || image.UpdatedAt == "" || image.IsIntermediate == nil || image.Starred == nil || image.HasWorkflow == nil {
				t.Errorf("image %d is not a normalized reference: %#v", index, image)
			}
		}
	}) {
		return
	}

	if !t.Run("queue listing is normalized and bounded", func(t *testing.T) {
		envelope := runJSONCommand(t, binary, target, "queue", "list", "--limit", "1")
		assertSuccessEnvelope(t, envelope, "queue.list")
		var data queueListData
		unmarshalData(t, envelope.Data, &data)
		if data.Items == nil {
			t.Fatal("queue list is null, want an array")
		}
		assertPageBounds(t, data.pageMetadata, len(data.Items))
		for index, item := range data.Items {
			if item.ItemID < 1 || item.QueueID == "" || item.Status == "" || item.BatchID == "" || item.CreatedAt == "" {
				t.Errorf("queue item %d is not a normalized summary: %#v", index, item)
			}
		}
	}) {
		return
	}
	t.Log("live read-only E2E: VERIFIED")
}

func validateTarget(t *testing.T, target string) {
	t.Helper()
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		t.Fatal("BEDIZ_E2E_URL must be an absolute URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		t.Fatal("BEDIZ_E2E_URL must not contain credentials, a query, or a fragment")
	}
}

func buildBinary(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get E2E working directory: %v", err)
	}
	repositoryRoot := filepath.Clean(filepath.Join(workingDirectory, ".."))
	binaryName := "bediz"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binary := filepath.Join(t.TempDir(), binaryName)
	command := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "./cmd/bediz")
	command.Dir = repositoryRoot
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build real Bediz binary: %v\n%s", err, output)
	}
	return binary
}

func runJSONCommand(t *testing.T, binary, target string, args ...string) successEnvelope {
	t.Helper()
	commandArgs := append(slices.Clone(args), "--url", target, "--token", secretSentinel, "--json")
	command := exec.CommandContext(t.Context(), binary, commandArgs...)
	command.Env = isolatedEnvironment(t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("%s exited unsuccessfully: %v\nstdout: %s\nstderr: %s", strings.Join(args, " "), err, stdout.Bytes(), stderr.Bytes())
	}
	if bytes.Contains(stdout.Bytes(), []byte(secretSentinel)) || bytes.Contains(stderr.Bytes(), []byte(secretSentinel)) {
		t.Fatalf("%s exposed the secret sentinel", strings.Join(args, " "))
	}
	if stderr.Len() != 0 {
		t.Fatalf("%s wrote diagnostics on successful JSON execution: %q", strings.Join(args, " "), stderr.String())
	}
	var envelope successEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("%s stdout is not exactly one V1 result envelope: %v\nstdout: %s", strings.Join(args, " "), err, stdout.Bytes())
	}
	return envelope
}

func isolatedEnvironment(configDirectory string) []string {
	environment := make([]string, 0, len(os.Environ())+1)
	for _, variable := range os.Environ() {
		name, _, _ := strings.Cut(variable, "=")
		if name != "BEDIZ_URL" && name != "BEDIZ_TOKEN" && name != "XDG_CONFIG_HOME" {
			environment = append(environment, variable)
		}
	}
	return append(environment, "XDG_CONFIG_HOME="+configDirectory)
}

func assertSuccessEnvelope(t *testing.T, envelope successEnvelope, operation string) {
	t.Helper()
	var warnings []jsontext.Value
	warningsErr := json.Unmarshal(envelope.Warnings, &warnings)
	if envelope.SchemaVersion != 1 || !envelope.OK || envelope.Operation != operation || len(envelope.Data) == 0 || len(envelope.Error) != 0 || warningsErr != nil || warnings == nil || len(warnings) != 0 {
		t.Fatalf("unexpected %s result envelope: schema=%d ok=%t operation=%q data=%s error=%s warnings=%s", operation, envelope.SchemaVersion, envelope.OK, envelope.Operation, envelope.Data, envelope.Error, envelope.Warnings)
	}
}

func unmarshalData(t *testing.T, raw jsontext.Value, target any) {
	t.Helper()
	if err := json.Unmarshal(raw, target, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("result data is not normalized to the public contract: %v\ndata: %s", err, raw)
	}
}

func assertPageBounds(t *testing.T, metadata pageMetadata, itemCount int) {
	t.Helper()
	if metadata.Offset == nil || metadata.Limit == nil || metadata.Total == nil {
		t.Fatalf("list result is missing page metadata: %#v", metadata)
	}
	if *metadata.Offset != 0 || *metadata.Limit != 1 || itemCount > *metadata.Limit || *metadata.Total < itemCount {
		t.Fatalf("unbounded or inconsistent list result: offset=%d limit=%d total=%d items=%d", *metadata.Offset, *metadata.Limit, *metadata.Total, itemCount)
	}
}
