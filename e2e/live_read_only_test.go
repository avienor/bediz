package e2e_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/avienor/bediz/internal/capability"
)

const secretSentinel = "bediz-live-e2e-secret-sentinel"

type resultEnvelope struct {
	SchemaVersion int            `json:"schema_version"`
	OK            bool           `json:"ok"`
	Operation     string         `json:"operation"`
	Data          jsontext.Value `json:"data"`
	Error         jsontext.Value `json:"error"`
	Warnings      jsontext.Value `json:"warnings"`
}

type modelSummaryData struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Base    string `json:"base"`
	Type    string `json:"type"`
	Format  string `json:"format"`
	Variant string `json:"variant"`
}

type liveAnimaModelKeys struct {
	Main         string
	VAE          string
	Qwen3Encoder string
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
		Available    *bool              `json:"available"`
		Total        *int               `json:"total"`
		Relevant     []modelSummaryData `json:"relevant"`
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

type imageReference struct {
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
}

type imageResultData struct {
	Image imageReference `json:"image"`
}

type uploadFixture struct {
	Path   string
	SHA256 [sha256.Size]byte
}

type backendResponse struct {
	StatusCode int
	Status     string
	Body       []byte
}

type imagesListData struct {
	pageMetadata
	Items []imageReference `json:"items"`
}

type boardSummary struct {
	BoardID        string  `json:"board_id"`
	BoardName      string  `json:"board_name"`
	ImageCount     *int    `json:"image_count"`
	Archived       *bool   `json:"archived"`
	CoverImageName *string `json:"cover_image_name"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

type boardsListData struct {
	pageMetadata
	Items []boardSummary `json:"items"`
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

type generationComponentsData struct {
	VAE          string `json:"vae"`
	Qwen3Encoder string `json:"qwen3_encoder"`
	T5Encoder    string `json:"t5_encoder"`
	CLIPEmbed    string `json:"clip_embed"`
}

type generationRequestData struct {
	SchemaVersion  int                       `json:"schema_version"`
	Model          string                    `json:"model"`
	PositivePrompt string                    `json:"positive_prompt"`
	NegativePrompt *string                   `json:"negative_prompt"`
	Width          *int                      `json:"width"`
	Height         *int                      `json:"height"`
	Steps          *int                      `json:"steps"`
	Scheduler      *string                   `json:"scheduler"`
	Guidance       *float64                  `json:"guidance"`
	Seed           *uint32                   `json:"seed"`
	OutputCount    *int                      `json:"output_count"`
	BoardID        *string                   `json:"board_id"`
	Components     *generationComponentsData `json:"components"`
}

type resolvedGenerationSettingsData struct {
	PositivePrompt string            `json:"positive_prompt"`
	NegativePrompt *string           `json:"negative_prompt"`
	Width          int               `json:"width"`
	Height         int               `json:"height"`
	Steps          int               `json:"steps"`
	Scheduler      string            `json:"scheduler"`
	Guidance       float64           `json:"guidance"`
	OutputCount    int               `json:"output_count"`
	BoardID        *string           `json:"board_id"`
	ModelKey       string            `json:"model_key"`
	ComponentKeys  map[string]string `json:"component_keys"`
	Seeds          []uint32          `json:"seeds"`
}

type executionReceiptData struct {
	SubmittedRequest generationRequestData          `json:"submitted_request"`
	ResolvedSettings resolvedGenerationSettingsData `json:"resolved_settings"`
	Queue            struct {
		QueueID string `json:"queue_id"`
		BatchID string `json:"batch_id"`
		ItemIDs []int  `json:"item_ids"`
	} `json:"queue"`
	Outputs []struct {
		ItemID int            `json:"item_id"`
		Seed   uint32         `json:"seed"`
		Image  imageReference `json:"image"`
	} `json:"outputs"`
	Warnings []uiSyncWarning `json:"warnings"`
}

type upscaleExecutionReceiptData struct {
	SubmittedRequest jsontext.Value `json:"submitted_request"`
	SourceImage      imageReference `json:"source_image"`
	SourceUploaded   bool           `json:"source_uploaded"`
	ResolvedSettings struct {
		PositivePrompt string            `json:"positive_prompt"`
		NegativePrompt string            `json:"negative_prompt"`
		Scale          int               `json:"scale"`
		Creativity     int               `json:"creativity"`
		Structure      int               `json:"structure"`
		Steps          int               `json:"steps"`
		Scheduler      string            `json:"scheduler"`
		Guidance       float64           `json:"guidance"`
		TileSize       int               `json:"tile_size"`
		TileOverlap    int               `json:"tile_overlap"`
		OutputWidth    int               `json:"output_width"`
		OutputHeight   int               `json:"output_height"`
		BoardID        *string           `json:"board_id"`
		ModelKey       string            `json:"model_key"`
		ComponentKeys  map[string]string `json:"component_keys"`
		Seeds          []uint32          `json:"seeds"`
	} `json:"resolved_settings"`
	Queue struct {
		QueueID string `json:"queue_id"`
		BatchID string `json:"batch_id"`
		ItemIDs []int  `json:"item_ids"`
	} `json:"queue"`
	Outputs []struct {
		ItemID int            `json:"item_id"`
		Seed   uint32         `json:"seed"`
		Image  imageReference `json:"image"`
	} `json:"outputs"`
	Warnings []uiSyncWarning `json:"warnings"`
}

type uiSyncWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details struct {
		NotRestored []string `json:"not_restored"`
	} `json:"details"`
}

func TestLiveGate(t *testing.T) {
	target := strings.TrimSpace(os.Getenv("BEDIZ_E2E_URL"))
	if target == "" {
		t.Skip("live E2E: NOT REQUESTED (BEDIZ_E2E_URL is unset)")
	}
	validateTarget(t, target)
	binary := buildBinary(t)
	var animaModels liveAnimaModelKeys
	var sdxlMain string
	var upscaleModel string
	var tileControlNet string
	var sd1Main string
	var sd1TileControlNet string
	var fluxSchnell string

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
			switch {
			case model.Base == "anima" && model.Type == "main":
				animaModels.Main = firstKey(animaModels.Main, model.Key)
			case model.Base == "anima" && model.Type == "vae":
				animaModels.VAE = firstKey(animaModels.VAE, model.Key)
			case model.Base == "any" && model.Type == "qwen3_encoder":
				animaModels.Qwen3Encoder = firstKey(animaModels.Qwen3Encoder, model.Key)
			case model.Base == "sdxl" && model.Type == "main":
				sdxlMain = firstKey(sdxlMain, model.Key)
			case model.Base == "any" && model.Type == "spandrel_image_to_image":
				upscaleModel = firstKey(upscaleModel, model.Key)
			case model.Base == "sdxl" && model.Type == "controlnet":
				tileControlNet = firstKey(tileControlNet, model.Key)
			case model.Base == "sd-1" && model.Type == "main" && model.Variant == "normal":
				sd1Main = firstKey(sd1Main, model.Key)
			case model.Base == "sd-1" && model.Type == "controlnet":
				sd1TileControlNet = firstKey(sd1TileControlNet, model.Key)
			case model.Base == "flux" && model.Type == "main" && model.Variant == "schnell" && capability.SupportsFLUXMain(model.Variant, model.Format):
				fluxSchnell = firstKey(fluxSchnell, model.Key)
			}
		}
		if animaModels.Main == "" || animaModels.VAE == "" || animaModels.Qwen3Encoder == "" {
			t.Fatalf("doctor did not report exact keys for the required Anima models: %#v", animaModels)
		}
		if sdxlMain == "" || upscaleModel == "" || tileControlNet == "" {
			t.Fatalf("doctor did not report exact SDXL upscale model keys: main=%q spandrel=%q controlnet=%q", sdxlMain, upscaleModel, tileControlNet)
		}
		if sd1Main == "" || sd1TileControlNet == "" {
			t.Fatalf("doctor did not report exact SD1.5 upscale model keys: main=%q controlnet=%q", sd1Main, sd1TileControlNet)
		}
		if fluxSchnell == "" {
			t.Fatal("doctor did not report an exact FLUX.1 schnell main model key")
		}
		for _, requirement := range data.Models.Requirements {
			if requirement.Name == "" || requirement.Available == nil || requirement.Required == nil || requirement.Satisfied == nil || !*requirement.Satisfied {
				t.Errorf("doctor returned an unsatisfied or incomplete model requirement: %#v", requirement)
			}
		}
		wantOperations := []string{"auth.huggingface.login", "auth.huggingface.logout", "auth.huggingface.status", "boards.create", "boards.get", "boards.list", "generate", "generate", "generate", "images.get", "images.list", "images.upload", "models.install", "models.install", "models.list", "models.status", "queue.get", "queue.list", "recall", "upscale", "upscale"}
		operations := make([]string, 0, len(data.Capabilities))
		generateFamilies := map[string]bool{}
		upscaleFamilies := map[string]bool{}
		for _, capability := range data.Capabilities {
			if capability.Compatible == nil || !*capability.Compatible || capability.Failures == nil || len(capability.Failures) != 0 {
				t.Errorf("doctor reported an incompatible capability: %#v", capability)
			}
			operations = append(operations, capability.Operation)
			if capability.Operation == "generate" {
				generateFamilies[capability.Family] = true
				if capability.UISync != "partial" {
					t.Errorf("generate ui_sync = %q, want partial", capability.UISync)
				}
			}
			if capability.Operation == "upscale" {
				upscaleFamilies[capability.Family] = true
				if capability.UISync != "partial" {
					t.Errorf("upscale ui_sync = %q, want partial", capability.UISync)
				}
			}
		}
		if !upscaleFamilies["sdxl"] || !upscaleFamilies["sd-1"] || len(upscaleFamilies) != 2 {
			t.Errorf("doctor upscale families = %#v, want SDXL and SD1.5", upscaleFamilies)
		}
		if !generateFamilies["anima"] || !generateFamilies["sdxl"] || !generateFamilies["flux"] || len(generateFamilies) != 3 {
			t.Errorf("doctor generate families = %#v, want Anima, SDXL, and FLUX.1", generateFamilies)
		}
		if data.UISync["generate"] != "partial" || data.UISync["upscale"] != "partial" {
			t.Errorf("doctor UI synchronization = %#v, want partial generation and upscale", data.UISync)
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
		assertPageBounds(t, data.pageMetadata, len(data.Items), 0, 1)
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
		assertPageBounds(t, data.pageMetadata, len(data.Items), 0, 1)
		for index, item := range data.Items {
			if item.ItemID < 1 || item.QueueID == "" || item.Status == "" || item.BatchID == "" || item.CreatedAt == "" {
				t.Errorf("queue item %d is not a normalized summary: %#v", index, item)
			}
		}
	}) {
		return
	}

	if !t.Run("board listing and lookup are normalized", func(t *testing.T) {
		envelope := runJSONCommand(t, binary, target, "boards", "list", "--limit", "1", "--include-archived")
		assertSuccessEnvelope(t, envelope, "boards.list")
		var data boardsListData
		unmarshalData(t, envelope.Data, &data)
		if data.Items == nil {
			t.Fatal("boards list is null, want an array")
		}
		assertPageBounds(t, data.pageMetadata, len(data.Items), 0, 1)
		for index, board := range data.Items {
			if board.BoardID == "" || board.ImageCount == nil || board.Archived == nil || board.CreatedAt == "" || board.UpdatedAt == "" {
				t.Errorf("board %d is not a normalized summary: %#v", index, board)
			}
		}
		if len(data.Items) == 1 {
			envelope := runJSONCommand(t, binary, target, "boards", "get", data.Items[0].BoardID)
			assertSuccessEnvelope(t, envelope, "boards.get")
			var got struct {
				Board boardSummary `json:"board"`
			}
			unmarshalData(t, envelope.Data, &got)
			if !reflect.DeepEqual(got.Board, data.Items[0]) {
				t.Errorf("boards get = %#v, want listed summary %#v", got.Board, data.Items[0])
			}
		}
		absent := "bediz-e2e-absent-board-" + uuid.New().String()
		missing, exitCode := executeJSONCommandContext(t, t.Context(), binary, target, "boards", "get", absent)
		var failure struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(missing.Error, &failure); err != nil || exitCode != 6 || missing.Operation != "boards.get" || failure.Code != "not_found" {
			t.Errorf("boards get %q = exit %d, envelope %#v; want not_found", absent, exitCode, missing)
		}
	}) {
		return
	}

	if !t.Run("image upload round trip self-cleans", func(t *testing.T) {
		fixture := writeUniquePNG(t)
		t.Logf("upload fixture cleanup evidence: local_file=%q sha256=%x", filepath.Base(fixture.Path), fixture.SHA256)

		envelope := runJSONCommand(t, binary, target, "images", "upload", fixture.Path)
		var cleanupHint struct {
			Image struct {
				ImageName string `json:"image_name"`
			} `json:"image"`
		}
		if err := json.Unmarshal(envelope.Data, &cleanupHint); err != nil || cleanupHint.Image.ImageName == "" {
			t.Fatalf("upload of fixture %q did not return a stable image identifier: %v; data: %s", filepath.Base(fixture.Path), err, envelope.Data)
		}
		imageName := cleanupHint.Image.ImageName
		t.Logf("uploaded fixture cleanup evidence: image_name=%q", imageName)
		t.Cleanup(func() {
			deleteBackendImage(t, target, imageName)
			assertImageRemoved(t, binary, target, imageName)
		})

		assertSuccessEnvelope(t, envelope, "images.upload")
		var uploaded imageResultData
		unmarshalData(t, envelope.Data, &uploaded)
		assertUploadedImageReference(t, uploaded.Image)
		if uploaded.Image.ImageName != imageName {
			t.Fatalf("upload returned inconsistent image identifiers: cleanup=%q normalized=%q", imageName, uploaded.Image.ImageName)
		}
		assertNoImageMetadata(t, target, imageName)

		getEnvelope := runJSONCommand(t, binary, target, "images", "get", imageName)
		assertSuccessEnvelope(t, getEnvelope, "images.get")
		var got imageResultData
		unmarshalData(t, getEnvelope.Data, &got)
		if !reflect.DeepEqual(got.Image, uploaded.Image) {
			t.Fatalf("images get reference differs from upload: upload=%#v get=%#v", uploaded.Image, got.Image)
		}

		const pageLimit = 100
		var listedImage *imageReference
		for offset := 0; ; {
			listEnvelope := runJSONCommand(t, binary, target, "images", "list", "--board", "none", "--offset", strconv.Itoa(offset), "--limit", strconv.Itoa(pageLimit))
			assertSuccessEnvelope(t, listEnvelope, "images.list")
			var listed imagesListData
			unmarshalData(t, listEnvelope.Data, &listed)
			assertPageBounds(t, listed.pageMetadata, len(listed.Items), offset, pageLimit)
			index := slices.IndexFunc(listed.Items, func(item imageReference) bool {
				return item.ImageName == imageName
			})
			if index >= 0 {
				listedImage = &listed.Items[index]
				break
			}
			nextOffset := offset + len(listed.Items)
			if nextOffset >= *listed.Total {
				break
			}
			if len(listed.Items) == 0 {
				t.Fatalf("images list returned an empty page before reported total %d while searching for %q", *listed.Total, imageName)
			}
			offset = nextOffset
		}
		if listedImage == nil {
			t.Fatalf("images list did not contain uploaded fixture %q", imageName)
		}
		if !reflect.DeepEqual(*listedImage, uploaded.Image) {
			t.Fatalf("images list reference differs from upload for %q: upload=%#v list=%#v", imageName, uploaded.Image, *listedImage)
		}
	}) {
		return
	}

	if !t.Run("anima direct execution produces a completed receipt and self-cleans", func(t *testing.T) {
		const testSeed uint32 = 42
		envelope := runJSONCommand(
			t, binary, target, "generate",
			"--model", animaModels.Main,
			"--vae", animaModels.VAE,
			"--qwen3-encoder", animaModels.Qwen3Encoder,
			"--prompt", "a tiny green apple on white background",
			"--steps", "1",
			"--width", "1024",
			"--height", "1024",
			"--seed", strconv.FormatUint(uint64(testSeed), 10),
		)
		registerGeneratedImageCleanup(t, binary, target, envelope.Data)
		assertSuccessEnvelope(t, envelope, "generate", "ui_sync_partial")

		var receipt executionReceiptData
		unmarshalData(t, envelope.Data, &receipt)
		var warnings []uiSyncWarning
		if err := json.Unmarshal(envelope.Warnings, &warnings, json.RejectUnknownMembers(true)); err != nil {
			t.Fatalf("invalid generation warnings: %v", err)
		}
		wantNotRestored := []string{"scheduler", "guidance", "vae", "qwen3_encoder", "output_count", "board_id"}
		if !reflect.DeepEqual(receipt.Warnings, warnings) || len(warnings) != 1 ||
			!slices.Equal(warnings[0].Details.NotRestored, wantNotRestored) {
			t.Fatalf("incomplete partial Handoff warning: receipt=%#v envelope=%#v", receipt.Warnings, warnings)
		}

		wantSubmittedRequest := generationRequestData{
			SchemaVersion:  1,
			Model:          animaModels.Main,
			PositivePrompt: "a tiny green apple on white background",
			Width:          new(1024),
			Height:         new(1024),
			Steps:          new(1),
			Seed:           new(testSeed),
			Components: &generationComponentsData{
				VAE:          animaModels.VAE,
				Qwen3Encoder: animaModels.Qwen3Encoder,
			},
		}
		if !reflect.DeepEqual(receipt.SubmittedRequest, wantSubmittedRequest) {
			t.Fatalf("submitted request = %#v, want exact canonical request %#v", receipt.SubmittedRequest, wantSubmittedRequest)
		}

		wantResolvedSettings := resolvedGenerationSettingsData{
			PositivePrompt: "a tiny green apple on white background",
			NegativePrompt: new(""),
			Width:          1024,
			Height:         1024,
			Steps:          1,
			Scheduler:      "euler",
			Guidance:       4.5,
			OutputCount:    1,
			ModelKey:       animaModels.Main,
			ComponentKeys: map[string]string{
				"vae":           animaModels.VAE,
				"qwen3_encoder": animaModels.Qwen3Encoder,
			},
			Seeds: []uint32{testSeed},
		}
		if !reflect.DeepEqual(receipt.ResolvedSettings, wantResolvedSettings) {
			t.Fatalf("resolved settings = %#v, want exact settings %#v", receipt.ResolvedSettings, wantResolvedSettings)
		}

		if receipt.Queue.QueueID != "default" || receipt.Queue.BatchID == "" || len(receipt.Queue.ItemIDs) != 1 {
			t.Fatalf("unexpected queue in receipt: %#v", receipt.Queue)
		}

		if len(receipt.Outputs) != 1 {
			t.Fatalf("outputs count = %d, want 1", len(receipt.Outputs))
		}
		output := receipt.Outputs[0]
		if output.ItemID != receipt.Queue.ItemIDs[0] || output.Seed != testSeed {
			t.Fatalf("output mismatch: %#v, want item_id=%d seed=%d", output, receipt.Queue.ItemIDs[0], testSeed)
		}

		image := output.Image
		assertGeneratedImageReference(t, target, image, 1024, 1024)

		getEnvelope := runJSONCommand(t, binary, target, "images", "get", image.ImageName)
		assertSuccessEnvelope(t, getEnvelope, "images.get")
		var got imageResultData
		unmarshalData(t, getEnvelope.Data, &got)
		if !reflect.DeepEqual(got.Image, image) {
			t.Fatalf("images get reference differs from execution receipt: receipt=%#v get=%#v", image, got.Image)
		}
	}) {
		return
	}

	if !t.Run("SDXL direct execution produces a completed receipt and self-cleans", func(t *testing.T) {
		const testSeed uint32 = 43
		envelope := runJSONCommand(t, binary, target, "generate", "--model", sdxlMain,
			"--prompt", "a tiny blue teacup on white background", "--width", "768", "--height", "768",
			"--steps", "2", "--seed", strconv.FormatUint(uint64(testSeed), 10))
		registerGeneratedImageCleanup(t, binary, target, envelope.Data)
		assertSuccessEnvelope(t, envelope, "generate", "ui_sync_partial")
		var receipt executionReceiptData
		unmarshalData(t, envelope.Data, &receipt)
		if receipt.ResolvedSettings.ModelKey != sdxlMain || len(receipt.ResolvedSettings.ComponentKeys) != 0 ||
			receipt.ResolvedSettings.Scheduler != "dpmpp_3m_k" || receipt.ResolvedSettings.Guidance != 7 ||
			!slices.Equal(receipt.ResolvedSettings.Seeds, []uint32{testSeed}) || len(receipt.Outputs) != 1 ||
			receipt.Outputs[0].Seed != testSeed || receipt.Outputs[0].ItemID != receipt.Queue.ItemIDs[0] {
			t.Fatalf("unexpected SDXL execution receipt: %#v", receipt)
		}
		if len(receipt.Warnings) != 1 || !slices.Equal(receipt.Warnings[0].Details.NotRestored, []string{"scheduler", "vae", "output_count", "board_id"}) {
			t.Fatalf("SDXL Handoff warning = %#v", receipt.Warnings)
		}
		assertGeneratedImageReference(t, target, receipt.Outputs[0].Image, 768, 768)
	}) {
		return
	}
	if !t.Run("SDXL upscale produces a verified receipt and self-cleans", func(t *testing.T) {
		runLiveUpscale(t, binary, target, sdxlMain, upscaleModel, tileControlNet, 45)
	}) {
		return
	}
	if !t.Run("SD1.5 upscale produces a verified receipt and self-cleans", func(t *testing.T) {
		runLiveUpscale(t, binary, target, sd1Main, upscaleModel, sd1TileControlNet, 46)
	}) {
		return
	}
	if !t.Run("FLUX.1 schnell direct execution produces a completed receipt and self-cleans", func(t *testing.T) {
		const testSeed uint32 = 44
		envelope := runJSONCommand(t, binary, target, "generate", "--model", fluxSchnell,
			"--prompt", "a tiny red teacup on white background", "--width", "768", "--height", "768",
			"--seed", strconv.FormatUint(uint64(testSeed), 10))
		registerGeneratedImageCleanup(t, binary, target, envelope.Data)
		assertSuccessEnvelope(t, envelope, "generate", "ui_sync_partial")
		var receipt executionReceiptData
		unmarshalData(t, envelope.Data, &receipt)
		if receipt.ResolvedSettings.ModelKey != fluxSchnell || receipt.ResolvedSettings.Steps != 4 || receipt.ResolvedSettings.Scheduler != "euler" ||
			len(receipt.ResolvedSettings.ComponentKeys) != 3 || receipt.ResolvedSettings.ComponentKeys["vae"] == "" ||
			receipt.ResolvedSettings.ComponentKeys["t5_encoder"] == "" || receipt.ResolvedSettings.ComponentKeys["clip_embed"] == "" ||
			!slices.Equal(receipt.ResolvedSettings.Seeds, []uint32{testSeed}) || len(receipt.Outputs) != 1 ||
			receipt.Outputs[0].Seed != testSeed || receipt.Outputs[0].ItemID != receipt.Queue.ItemIDs[0] {
			t.Fatalf("unexpected FLUX.1 receipt: %#v", receipt)
		}
		var raw map[string]any
		if err := json.Unmarshal(envelope.Data, &raw); err != nil {
			t.Fatal(err)
		}
		if _, exists := raw["resolved_settings"].(map[string]any)["guidance"]; exists {
			t.Fatal("schnell receipt contains guidance")
		}
		if len(receipt.Warnings) != 1 || !slices.Equal(receipt.Warnings[0].Details.NotRestored, []string{"scheduler", "vae", "t5_encoder", "clip_embed", "output_count", "board_id"}) {
			t.Fatalf("FLUX.1 Handoff warning = %#v", receipt.Warnings)
		}
		assertGeneratedImageReference(t, target, receipt.Outputs[0].Image, 768, 768)
	}) {
		return
	}
	t.Log("live E2E: VERIFIED")
}

func firstKey(current, candidate string) string {
	if current == "" || candidate < current {
		return candidate
	}
	return current
}

func registerGeneratedImageCleanup(t *testing.T, binary, target string, raw jsontext.Value) {
	t.Helper()
	var hint struct {
		Outputs []struct {
			Image struct {
				ImageName string `json:"image_name"`
			} `json:"image"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal(raw, &hint); err != nil {
		return
	}
	seen := make(map[string]bool)
	for _, output := range hint.Outputs {
		imageName := output.Image.ImageName
		if imageName == "" || seen[imageName] {
			continue
		}
		seen[imageName] = true
		t.Logf("generated fixture cleanup evidence: image_name=%q", imageName)
		t.Cleanup(func() {
			deleteBackendImage(t, target, imageName)
			assertImageRemoved(t, binary, target, imageName)
		})
	}
}

func assertGeneratedImageReference(t *testing.T, target string, image imageReference, width, height int) {
	t.Helper()
	if image.ImageName == "" || image.ImageOrigin != "internal" || image.ImageCategory != "general" ||
		image.Width != width || image.Height != height || image.CreatedAt == "" || image.UpdatedAt == "" ||
		image.IsIntermediate == nil || *image.IsIntermediate || image.SessionID == nil || *image.SessionID == "" ||
		image.NodeID == nil || *image.NodeID == "" || image.Starred == nil || *image.Starred ||
		image.HasWorkflow == nil || image.BoardID != nil {
		t.Fatalf("output image reference is incomplete or incorrect: %#v", image)
	}
	for field, imageURL := range map[string]string{
		"image_url":     image.ImageURL,
		"thumbnail_url": image.ThumbnailURL,
	} {
		assertAccessibleImageURL(t, target, field, imageURL)
	}
}

func assertAccessibleImageURL(t *testing.T, target, field, rawURL string) {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		t.Fatalf("generated image %s = %q, want an absolute URL", field, rawURL)
	}
	targetURL, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != targetURL.Scheme || parsed.Host != targetURL.Host {
		t.Fatalf("generated image %s points outside the tested InvokeAI target: %q", field, rawURL)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+secretSentinel)
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		t.Fatalf("fetch generated image %s: %v", field, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("generated image %s returned %s", field, response.Status)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, 1))
	if err != nil {
		t.Fatalf("read generated image %s: %v", field, err)
	}
	if len(content) == 0 {
		t.Fatalf("generated image %s returned an empty body", field)
	}
}

// runLiveUpscale upscales a unique uploaded 512 × 512 source at scale 2 and
// verifies the receipt and output before removing both images.
func runLiveUpscale(t *testing.T, binary, target, mainModel, upscaleModel, tileControlNet string, testSeed uint32) {
	t.Helper()
	fixture := writeUniquePNGSize(t, 512)
	upload := runJSONCommand(t, binary, target, "images", "upload", fixture.Path)
	assertSuccessEnvelope(t, upload, "images.upload")
	var source imageResultData
	unmarshalData(t, upload.Data, &source)
	if source.Image.ImageName == "" || source.Image.Width != 512 || source.Image.Height != 512 {
		t.Fatalf("upscale source upload = %#v", source)
	}
	t.Logf("upscale source cleanup evidence: image_name=%q", source.Image.ImageName)
	t.Cleanup(func() {
		deleteBackendImage(t, target, source.Image.ImageName)
		assertImageRemoved(t, binary, target, source.Image.ImageName)
	})
	envelope := runJSONCommand(t, binary, target, "upscale", "--image", source.Image.ImageName,
		"--model", mainModel, "--upscale-model", upscaleModel, "--tile-controlnet", tileControlNet,
		"--scale", "2", "--steps", "4", "--tile-size", "512", "--seed", strconv.FormatUint(uint64(testSeed), 10), "--timeout", "10m")
	registerGeneratedImageCleanup(t, binary, target, envelope.Data)
	assertSuccessEnvelope(t, envelope, "upscale", "ui_sync_partial")
	var receipt upscaleExecutionReceiptData
	unmarshalData(t, envelope.Data, &receipt)
	if receipt.SourceUploaded || receipt.SourceImage.ImageName != source.Image.ImageName ||
		receipt.ResolvedSettings.Scale != 2 || receipt.ResolvedSettings.OutputWidth != 1024 || receipt.ResolvedSettings.OutputHeight != 1024 ||
		receipt.ResolvedSettings.ModelKey != mainModel || !reflect.DeepEqual(receipt.ResolvedSettings.ComponentKeys, map[string]string{"upscale_model": upscaleModel, "tile_controlnet": tileControlNet}) ||
		!slices.Equal(receipt.ResolvedSettings.Seeds, []uint32{testSeed}) || receipt.Queue.QueueID != "default" || receipt.Queue.BatchID == "" || len(receipt.Queue.ItemIDs) != 1 ||
		len(receipt.Outputs) != 1 || receipt.Outputs[0].ItemID != receipt.Queue.ItemIDs[0] || receipt.Outputs[0].Seed != testSeed || len(receipt.Warnings) != 1 ||
		!slices.Equal(receipt.Warnings[0].Details.NotRestored, []string{"source_image", "upscale_model", "scale", "creativity", "structure", "tile_controlnet", "tile_size", "tile_overlap", "scheduler", "guidance", "vae", "board_id"}) {
		t.Fatalf("upscale receipt = %#v", receipt)
	}
	assertGeneratedImageReference(t, target, receipt.Outputs[0].Image, 1024, 1024)
}

func writeUniquePNG(t *testing.T) uploadFixture {
	return writeUniquePNGSize(t, 2)
}

func writeUniquePNGSize(t *testing.T, side int) uploadFixture {
	t.Helper()
	identifier := uuid.New()
	path := filepath.Join(t.TempDir(), "bediz-e2e-upload-"+identifier.String()+".png")
	var encoded bytes.Buffer
	fixture := image.NewNRGBA(image.Rect(0, 0, side, side))
	for index := range side * side {
		fixture.SetNRGBA(index%side, index/side, color.NRGBA{
			R: identifier[index%len(identifier)],
			G: identifier[(index+1)%len(identifier)],
			B: identifier[(index+2)%len(identifier)],
			A: 0xff,
		})
	}
	if err := png.Encode(&encoded, fixture); err != nil {
		t.Fatalf("encode unique PNG fixture: %v", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatalf("create unique PNG fixture: %v", err)
	}
	_, writeErr := file.Write(encoded.Bytes())
	closeErr := file.Close()
	if writeErr != nil {
		t.Fatalf("write unique PNG fixture: %v", writeErr)
	}
	if closeErr != nil {
		t.Fatalf("close unique PNG fixture: %v", closeErr)
	}
	return uploadFixture{Path: path, SHA256: sha256.Sum256(encoded.Bytes())}
}

func assertUploadedImageReference(t *testing.T, image imageReference) {
	t.Helper()
	if image.ImageName == "" || image.ImageOrigin != "external" || image.ImageCategory != "user" || image.Width != 2 || image.Height != 2 || image.CreatedAt == "" || image.UpdatedAt == "" || image.IsIntermediate == nil || *image.IsIntermediate || image.Starred == nil || *image.Starred || image.HasWorkflow == nil || *image.HasWorkflow || image.BoardID != nil || image.SessionID != nil || image.NodeID != nil {
		t.Fatalf("uploaded image does not match the unmodified user-image contract: %#v", image)
	}
	for field, value := range map[string]string{"image_url": image.ImageURL, "thumbnail_url": image.ThumbnailURL} {
		parsed, err := url.Parse(value)
		if err != nil || !parsed.IsAbs() || parsed.Host == "" {
			t.Fatalf("uploaded image %s = %q, want an absolute URL", field, value)
		}
	}
}

func assertNoImageMetadata(t *testing.T, target, imageName string) {
	t.Helper()
	response := requestImageBackend(t, t.Context(), http.MethodGet, target, imageName, "metadata")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("inspect metadata for image %q: GET returned %s: %s", imageName, response.Status, response.Body)
	}
	var metadata jsontext.Value
	if err := json.Unmarshal(response.Body, &metadata); err != nil {
		t.Fatalf("inspect metadata for image %q: invalid response: %v; body: %s", imageName, err, response.Body)
	}
	if !bytes.Equal(bytes.TrimSpace(metadata), []byte("null")) {
		t.Fatalf("image %q has injected metadata: %s", imageName, metadata)
	}
}

func deleteBackendImage(t *testing.T, target, imageName string) {
	t.Helper()
	response := requestImageBackend(t, context.WithoutCancel(t.Context()), http.MethodDelete, target, imageName, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("cleanup image %q: DELETE returned %s: %s", imageName, response.Status, response.Body)
	}
	var result struct {
		AffectedBoards []string `json:"affected_boards"`
		DeletedImages  []string `json:"deleted_images"`
		FailedImages   []string `json:"failed_images"`
	}
	if err := json.Unmarshal(response.Body, &result, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("cleanup image %q: invalid response: %v; body: %s", imageName, err, response.Body)
	}
	if !slices.Equal(result.AffectedBoards, []string{"none"}) || !slices.Equal(result.DeletedImages, []string{imageName}) || len(result.FailedImages) != 0 {
		t.Fatalf("cleanup image %q was not targeted and conclusive: %#v", imageName, result)
	}
}

func requestImageBackend(t *testing.T, ctx context.Context, method, target, imageName, resource string) backendResponse {
	t.Helper()
	endpoint, err := url.JoinPath(target, "/api/v1/images/i/", imageName, resource)
	if err != nil {
		t.Fatalf("%s image %q: build backend endpoint: %v", method, imageName, err)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		t.Fatalf("%s image %q: build backend request: %v", method, imageName, err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+secretSentinel)
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		t.Fatalf("%s image %q: backend request failed: %v", method, imageName, err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatalf("%s image %q: read backend response: %v", method, imageName, err)
	}
	return backendResponse{StatusCode: response.StatusCode, Status: response.Status, Body: body}
}

func assertImageRemoved(t *testing.T, binary, target, imageName string) {
	t.Helper()
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 30*time.Second)
	defer cancel()
	envelope, exitCode := executeJSONCommandContext(t, cleanupContext, binary, target, "images", "get", imageName)
	if exitCode != 6 {
		t.Fatalf("cleanup image %q: final images get exit code = %d, want 6", imageName, exitCode)
	}
	var structuredError struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details jsontext.Value `json:"details"`
	}
	if err := json.Unmarshal(envelope.Error, &structuredError, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("cleanup image %q: final lookup error is not normalized: %v; error: %s", imageName, err, envelope.Error)
	}
	var warnings []jsontext.Value
	warningsErr := json.Unmarshal(envelope.Warnings, &warnings)
	if envelope.SchemaVersion != 1 || envelope.OK || envelope.Operation != "images.get" || len(envelope.Data) != 0 || structuredError.Code != "not_found" || structuredError.Message == "" || warningsErr != nil || warnings == nil || len(warnings) != 0 {
		t.Fatalf("cleanup image %q: final lookup did not return the expected not_found envelope: %#v", imageName, envelope)
	}
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

func runJSONCommand(t *testing.T, binary, target string, args ...string) resultEnvelope {
	t.Helper()
	envelope, exitCode := executeJSONCommandContext(t, t.Context(), binary, target, args...)
	if exitCode != 0 {
		t.Fatalf("%s exit code = %d, want 0; envelope: %#v", strings.Join(args, " "), exitCode, envelope)
	}
	return envelope
}

func executeJSONCommandContext(t *testing.T, ctx context.Context, binary, target string, args ...string) (resultEnvelope, int) {
	t.Helper()
	commandArgs := append(slices.Clone(args), "--url", target, "--token", secretSentinel, "--json")
	command := exec.CommandContext(ctx, binary, commandArgs...)
	command.Env = isolatedEnvironment(t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	exitCode := 0
	if err := command.Run(); err != nil {
		exitError, ok := errors.AsType[*exec.ExitError](err)
		if !ok {
			t.Fatalf("%s could not execute: %v\nstdout: %s\nstderr: %s", strings.Join(args, " "), err, stdout.Bytes(), stderr.Bytes())
		}
		exitCode = exitError.ExitCode()
	}
	if bytes.Contains(stdout.Bytes(), []byte(secretSentinel)) || bytes.Contains(stderr.Bytes(), []byte(secretSentinel)) {
		t.Fatalf("%s exposed the secret sentinel", strings.Join(args, " "))
	}
	if stderr.Len() != 0 {
		t.Fatalf("%s wrote diagnostics on successful JSON execution: %q", strings.Join(args, " "), stderr.String())
	}
	var envelope resultEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("%s stdout is not exactly one V1 result envelope: %v\nstdout: %s", strings.Join(args, " "), err, stdout.Bytes())
	}
	return envelope, exitCode
}

func isolatedEnvironment(configDirectory string) []string {
	configVariable := "XDG_CONFIG_HOME"
	switch runtime.GOOS {
	case "windows":
		configVariable = "AppData"
	case "darwin":
		configVariable = "HOME"
	}
	environment := make([]string, 0, len(os.Environ())+1)
	for _, variable := range os.Environ() {
		name, _, _ := strings.Cut(variable, "=")
		if !strings.EqualFold(name, "BEDIZ_URL") && !strings.EqualFold(name, "BEDIZ_TOKEN") && !strings.EqualFold(name, configVariable) {
			environment = append(environment, variable)
		}
	}
	return append(environment, configVariable+"="+configDirectory)
}

func assertSuccessEnvelope(t *testing.T, envelope resultEnvelope, operation string, expectedWarningCodes ...string) {
	t.Helper()
	var warnings []struct {
		Code string `json:"code"`
	}
	warningsErr := json.Unmarshal(envelope.Warnings, &warnings)
	codes := make([]string, len(warnings))
	for i, warning := range warnings {
		codes[i] = warning.Code
	}
	if envelope.SchemaVersion != 1 || !envelope.OK || envelope.Operation != operation || len(envelope.Data) == 0 || len(envelope.Error) != 0 || warningsErr != nil || warnings == nil || !slices.Equal(codes, expectedWarningCodes) {
		t.Fatalf("unexpected %s result envelope: schema=%d ok=%t operation=%q data=%s error=%s warnings=%s", operation, envelope.SchemaVersion, envelope.OK, envelope.Operation, envelope.Data, envelope.Error, envelope.Warnings)
	}
}

func unmarshalData(t *testing.T, raw jsontext.Value, target any) {
	t.Helper()
	if err := json.Unmarshal(raw, target, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("result data is not normalized to the public contract: %v\ndata: %s", err, raw)
	}
}

func assertPageBounds(t *testing.T, metadata pageMetadata, itemCount, wantOffset, wantLimit int) {
	t.Helper()
	if metadata.Offset == nil || metadata.Limit == nil || metadata.Total == nil {
		t.Fatalf("list result is missing page metadata: %#v", metadata)
	}
	if *metadata.Offset != wantOffset || *metadata.Limit != wantLimit || itemCount > *metadata.Limit || *metadata.Total < wantOffset+itemCount {
		t.Fatalf("unbounded or inconsistent list result: offset=%d limit=%d total=%d items=%d; want offset=%d limit=%d", *metadata.Offset, *metadata.Limit, *metadata.Total, itemCount, wantOffset, wantLimit)
	}
}
