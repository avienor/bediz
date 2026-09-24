// Package upscale resolves and executes the tested InvokeAI generative upscale flow.
package upscale

import (
	"encoding/binary"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
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
	Model          string      `json:"model,omitempty"`
	Profile        string      `json:"profile,omitempty"`
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

// UnmarshalJSON decodes with v2 semantics, which also reject invalid UTF-8
// strings. Member names are matched exactly, and duplicate or unknown members
// are rejected at every level.
func (request *Request) UnmarshalJSON(data []byte) error {
	type plainRequest Request
	var decoded plainRequest
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return err
	}
	*request = Request(decoded)
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

// ValidateRequest performs all checks that must complete without network access.
func ValidateRequest(request Request) error {
	return validateRequest(request, true)
}

func validateRequest(request Request, requireModel bool) error {
	if request.SchemaVersion != 1 {
		return operation.InvalidRequest(fmt.Sprintf("unsupported request schema version %d", request.SchemaVersion))
	}
	switch request.Source.Type {
	case "image":
		if request.Source.Reference == "" {
			return operation.InvalidRequest("image source must name an existing InvokeAI image")
		}
	case "path":
		if !filepath.IsAbs(request.Source.Reference) {
			return operation.InvalidRequest("path source must be an absolute local image path")
		}
	default:
		return operation.InvalidRequest("source requires an existing InvokeAI image or an absolute local image path")
	}
	if requireModel && request.Model == "" {
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
	if request.Scheduler != nil && !graphops.IsSDXLScheduler(*request.Scheduler) {
		return operation.InvalidRequest("scheduler is not supported for upscale")
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

// ResolveMain selects an installed normal main model of a registered upscale family.
func ResolveMain(inventory []graphops.ModelIdentifier, selector string) (graphops.ModelIdentifier, error) {
	main, err := graphops.ResolveMain(inventory, selector)
	if err != nil {
		return graphops.ModelIdentifier{}, err
	}
	main, err = graphops.CompleteModelIdentifier(main)
	if err != nil {
		return graphops.ModelIdentifier{}, err
	}
	if _, ok := familyFor(main.Base); !ok || main.Variant != "normal" {
		return graphops.ModelIdentifier{}, operation.UnsupportedCapability(fmt.Sprintf("model %q must be a normal %s main model for upscale", main.Key, familyLabels()))
	}
	return main, nil
}

// Resolve accepts a main model of a registered upscale family and selects its
// complete Upscale Component Set after local validation.
func Resolve(request Request, inventory []graphops.ModelIdentifier, random io.Reader) (Resolution, error) {
	if err := ValidateRequest(request); err != nil {
		return Resolution{}, err
	}
	main, err := ResolveMain(inventory, request.Model)
	if err != nil {
		return Resolution{}, err
	}
	family, _ := familyFor(main.Base)
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
		tileModel, err = graphops.ResolveUniqueCompatible(inventory, tileSelector, graphops.ComponentRequirement{Kind: "tile_controlnet", Base: family.base, ModelType: "controlnet"})
	} else {
		candidates := make([]graphops.ModelIdentifier, 0)
		for _, model := range inventory {
			if model.Base == family.base && model.Type == "controlnet" {
				candidates = append(candidates, model)
			}
		}
		if len(candidates) == 0 {
			err = operation.MissingComponent("tile_controlnet", family.base, "controlnet", "install the "+family.tileStarter+" starter")
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
		models.VAE, err = graphops.ResolveUniqueCompatible(inventory, vaeSelector, graphops.ComponentRequirement{Kind: "vae", Base: family.base, ModelType: "vae"})
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
