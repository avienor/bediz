// Package upscale resolves and executes the tested InvokeAI generative upscale flow.
package upscale

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"

	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/operation"
)

type Source struct {
	Type      string `json:"type"`
	Reference string `json:"reference"`
}

type Components struct {
	UpscaleModel   *string `json:"upscale_model,omitempty"`
	TileControlNet *string `json:"tile_controlnet,omitempty"`
	VAE            *string `json:"vae,omitempty"`
}

type Request struct {
	SchemaVersion  int         `json:"schema_version"`
	Source         Source      `json:"source"`
	Model          string      `json:"model"`
	PositivePrompt string      `json:"positive_prompt,omitempty"`
	NegativePrompt string      `json:"negative_prompt,omitempty"`
	Scale          *int        `json:"scale,omitempty"`
	Creativity     *int        `json:"creativity,omitempty"`
	Structure      *int        `json:"structure,omitempty"`
	Steps          *int        `json:"steps,omitempty"`
	Scheduler      *string     `json:"scheduler,omitempty"`
	Guidance       *float64    `json:"guidance,omitempty"`
	Seed           *uint32     `json:"seed,omitempty"`
	TileSize       *int        `json:"tile_size,omitempty"`
	TileOverlap    *int        `json:"tile_overlap,omitempty"`
	BoardID        string      `json:"board_id,omitempty"`
	Components     *Components `json:"components,omitempty"`
}

// UnmarshalJSON rejects explicit nulls, which otherwise become absent optional
// fields and silently acquire defaults. The inner decoder retains strict
// unknown-field checking for nested source and component objects.
func (request *Request) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if err := rejectNullFields(fields); err != nil {
		return err
	}
	for _, nested := range []string{"source", "components"} {
		raw, ok := fields[nested]
		if !ok {
			continue
		}
		var children map[string]json.RawMessage
		if err := json.Unmarshal(raw, &children); err != nil {
			return fmt.Errorf("%s: %w", nested, err)
		}
		if err := rejectNullFields(children); err != nil {
			return fmt.Errorf("%s: %w", nested, err)
		}
	}
	type plainRequest Request
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var decoded plainRequest
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	*request = Request(decoded)
	return nil
}

func rejectNullFields(fields map[string]json.RawMessage) error {
	for name, raw := range fields {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("field %q cannot be null", name)
		}
	}
	return nil
}

type Models struct {
	Main           graphops.ModelIdentifier
	UpscaleModel   graphops.ModelIdentifier
	TileControlNet graphops.ModelIdentifier
	VAE            graphops.ModelIdentifier
}

type Resolution struct {
	Request Request
	Models  Models
	Seed    uint32
}

var schedulers = []string{
	"ddim", "ddpm", "deis", "deis_k", "lms", "lms_k", "pndm", "heun", "heun_k", "euler", "euler_k", "euler_a",
	"kdpm_2", "kdpm_2_k", "kdpm_2_a", "kdpm_2_a_k", "dpmpp_2s", "dpmpp_2s_k", "dpmpp_2m", "dpmpp_2m_k",
	"dpmpp_2m_sde", "dpmpp_2m_sde_k", "dpmpp_3m", "dpmpp_3m_k", "dpmpp_sde", "dpmpp_sde_k", "er_sde",
	"unipc", "unipc_k", "lcm", "tcd",
}

// ValidateRequest performs all checks that must complete without network access.
func ValidateRequest(request Request) error {
	if request.SchemaVersion != 1 {
		return operation.InvalidRequest(fmt.Sprintf("unsupported request schema version %d", request.SchemaVersion))
	}
	if request.Source.Type == "path" {
		return operation.UnsupportedCapability("local source paths are not supported for upscale yet")
	}
	if request.Source.Type != "image" || request.Source.Reference == "" {
		return operation.InvalidRequest("source must name an existing InvokeAI image")
	}
	if request.Model == "" {
		return operation.InvalidRequest("model is required")
	}
	if request.Scale != nil && !slices.Contains([]int{2, 4, 8}, *request.Scale) {
		return operation.InvalidRequest("scale must be 2, 4, or 8")
	}
	if request.Creativity != nil && (*request.Creativity < -10 || *request.Creativity > 10) {
		return operation.InvalidRequest("creativity must be from -10 to 10")
	}
	if request.Structure != nil && (*request.Structure < -10 || *request.Structure > 10) {
		return operation.InvalidRequest("structure must be from -10 to 10")
	}
	if request.Steps != nil && *request.Steps < 1 {
		return operation.InvalidRequest("steps must be positive")
	}
	if request.Scheduler != nil && !slices.Contains(schedulers, *request.Scheduler) {
		return operation.InvalidRequest("scheduler is not supported for SDXL upscale")
	}
	if request.Guidance != nil && (math.IsNaN(*request.Guidance) || math.IsInf(*request.Guidance, 0) || *request.Guidance < 1) {
		return operation.InvalidRequest("guidance must be finite and at least 1")
	}
	if request.TileSize != nil && (*request.TileSize < 512 || *request.TileSize > 1536 || *request.TileSize%64 != 0) {
		return operation.InvalidRequest("tile size must be a multiple of 64 from 512 to 1536")
	}
	if request.TileOverlap != nil && (*request.TileOverlap < 16 || *request.TileOverlap > 512 || *request.TileOverlap%8 != 0) {
		return operation.InvalidRequest("tile overlap must be a multiple of 8 from 16 to 512")
	}
	tileSize, overlap := 1024, 128
	if request.TileSize != nil {
		tileSize = *request.TileSize
	}
	if request.TileOverlap != nil {
		overlap = *request.TileOverlap
	}
	if overlap >= tileSize {
		return operation.InvalidRequest("tile overlap must be less than tile size")
	}
	if request.Components != nil {
		for _, selector := range []*string{request.Components.UpscaleModel, request.Components.TileControlNet, request.Components.VAE} {
			if selector != nil && *selector == "" {
				return operation.InvalidRequest("component selector must be a model key or unique name")
			}
		}
	}
	return nil
}

// ResolveSDXL selects the complete Upscale Component Set after local validation.
func ResolveSDXL(request Request, inventory []graphops.ModelIdentifier, random io.Reader) (Resolution, error) {
	if err := ValidateRequest(request); err != nil {
		return Resolution{}, err
	}
	main, err := graphops.ResolveMain(inventory, request.Model)
	if err != nil {
		return Resolution{}, err
	}
	main, err = graphops.CompleteModelIdentifier(main)
	if err != nil {
		return Resolution{}, err
	}
	if main.Base != "sdxl" || main.Variant != "normal" {
		return Resolution{}, operation.UnsupportedCapability(fmt.Sprintf("model %q must be an SDXL normal main model for upscale", main.Key))
	}
	upscaleSelector, tileSelector, vaeSelector := "", "", ""
	if request.Components != nil {
		if request.Components.UpscaleModel != nil {
			upscaleSelector = *request.Components.UpscaleModel
		}
		if request.Components.TileControlNet != nil {
			tileSelector = *request.Components.TileControlNet
		}
		if request.Components.VAE != nil {
			vaeSelector = *request.Components.VAE
		}
	}
	upscaleModel, err := graphops.ResolveComponent(inventory, upscaleSelector, graphops.ComponentRequirement{Kind: "upscale_model", Base: "any", ModelType: "spandrel_image_to_image"})
	if missing, ok := errors.AsType[*operation.MissingComponentError](err); ok {
		missing.InstallationGuidance = "install the RealESRGAN_x4plus starter"
	}
	if err != nil {
		return Resolution{}, err
	}
	var tileModel graphops.ModelIdentifier
	if tileSelector != "" {
		tileModel, err = graphops.ResolveUniqueCompatible(inventory, tileSelector, graphops.ComponentRequirement{Kind: "tile_controlnet", Base: "sdxl", ModelType: "controlnet"})
	} else {
		candidates := make([]graphops.ModelIdentifier, 0)
		for _, model := range inventory {
			if model.Base == "sdxl" && model.Type == "controlnet" {
				candidates = append(candidates, model)
			}
		}
		if len(candidates) == 0 {
			err = operation.MissingComponent("tile_controlnet", "sdxl", "controlnet", "install the xinsir/controlNet-tile-sdxl-1.0 starter")
		} else {
			var choices []operation.SelectionCandidate
			choices, err = graphops.SelectionCandidates(candidates)
			if err == nil {
				err = operation.SelectionRequired("tile_controlnet", "", choices)
			}
		}
	}
	if err != nil {
		return Resolution{}, err
	}
	models := Models{Main: main, UpscaleModel: upscaleModel, TileControlNet: tileModel}
	if vaeSelector != "" {
		models.VAE, err = graphops.ResolveUniqueCompatible(inventory, vaeSelector, graphops.ComponentRequirement{Kind: "vae", Base: "sdxl", ModelType: "vae"})
		if err != nil {
			return Resolution{}, err
		}
	}
	resolved := request
	if resolved.Scale == nil {
		resolved.Scale = new(4)
	}
	if resolved.Creativity == nil {
		resolved.Creativity = new(0)
	}
	if resolved.Structure == nil {
		resolved.Structure = new(0)
	}
	if resolved.Steps == nil {
		resolved.Steps = new(30)
	}
	if resolved.Scheduler == nil {
		resolved.Scheduler = new("kdpm_2")
	}
	if resolved.Guidance == nil {
		resolved.Guidance = new(2.0)
	}
	if resolved.TileSize == nil {
		resolved.TileSize = new(1024)
	}
	if resolved.TileOverlap == nil {
		resolved.TileOverlap = new(128)
	}
	if resolved.Seed == nil {
		var encoded [4]byte
		if _, err := io.ReadFull(random, encoded[:]); err != nil {
			return Resolution{}, fmt.Errorf("assign random upscale seed: %w", err)
		}
		resolved.Seed = new(binary.LittleEndian.Uint32(encoded[:]))
	}
	return Resolution{Request: resolved, Models: models, Seed: *resolved.Seed}, nil
}
