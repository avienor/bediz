// Package profiles stores local technical Generation Profiles.
package profiles

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/avienor/bediz/internal/document"
	"github.com/avienor/bediz/internal/graphops"
)

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type GenerateComponents struct {
	VAE          *string `json:"vae,omitempty"`
	Qwen3Encoder *string `json:"qwen3_encoder,omitempty"`
	T5Encoder    *string `json:"t5_encoder,omitempty"`
	CLIPEmbed    *string `json:"clip_embed,omitempty"`
}

type Generate struct {
	Model       *string             `json:"model,omitempty"`
	Components  *GenerateComponents `json:"components,omitzero"`
	Width       *int                `json:"width,omitempty"`
	Height      *int                `json:"height,omitempty"`
	Steps       *int                `json:"steps,omitempty"`
	Scheduler   *string             `json:"scheduler,omitempty"`
	Guidance    *float64            `json:"guidance,omitempty"`
	OutputCount *int                `json:"output_count,omitempty"`
}

type UpscaleComponents struct {
	UpscaleModel   *string `json:"upscale_model,omitempty"`
	TileControlNet *string `json:"tile_controlnet,omitempty"`
	VAE            *string `json:"vae,omitempty"`
}

type Upscale struct {
	Model       *string            `json:"model,omitempty"`
	Components  *UpscaleComponents `json:"components,omitzero"`
	Scale       *int               `json:"scale,omitempty"`
	Creativity  *int               `json:"creativity,omitempty"`
	Structure   *int               `json:"structure,omitempty"`
	Steps       *int               `json:"steps,omitempty"`
	Scheduler   *string            `json:"scheduler,omitempty"`
	Guidance    *float64           `json:"guidance,omitempty"`
	TileSize    *int               `json:"tile_size,omitempty"`
	TileOverlap *int               `json:"tile_overlap,omitempty"`
}

type Document struct {
	SchemaVersion int       `json:"schema_version"`
	Name          string    `json:"name"`
	Generate      *Generate `json:"generate,omitzero"`
	Upscale       *Upscale  `json:"upscale,omitzero"`
}

func ValidName(name string) bool { return namePattern.MatchString(name) }

func Validate(doc Document) error {
	if doc.SchemaVersion != 1 {
		return fmt.Errorf("unsupported profile schema version %d", doc.SchemaVersion)
	}
	if !ValidName(doc.Name) {
		return errors.New("profile name must match ^[a-z0-9][a-z0-9_-]{0,63}$")
	}
	if doc.Generate == nil && doc.Upscale == nil {
		return errors.New("profile requires generate or upscale section")
	}
	if doc.Generate != nil {
		if err := doc.Generate.validate(); err != nil {
			return fmt.Errorf("generate: %w", err)
		}
	}
	if doc.Upscale != nil {
		if err := doc.Upscale.validate(); err != nil {
			return fmt.Errorf("upscale: %w", err)
		}
	}
	return nil
}

func validSelector(value *string) bool { return value == nil || strings.TrimSpace(*value) != "" }

func validPositive(value *int) bool { return value == nil || *value > 0 }

func validGuidance(value *float64) bool {
	return value == nil || (!math.IsNaN(*value) && !math.IsInf(*value, 0) && *value >= 1)
}

func validScheduler(value *string) bool { return value == nil || graphops.IsSDXLScheduler(*value) }

func (g Generate) validate() error {
	if !validSelector(g.Model) {
		return errors.New("model selector cannot be empty")
	}
	if (g.Width == nil) != (g.Height == nil) || !validPositive(g.Width) || !validPositive(g.Height) {
		return errors.New("width and height must be positive and supplied together")
	}
	if !validPositive(g.Steps) || !validPositive(g.OutputCount) {
		return errors.New("steps and output_count must be positive")
	}
	if !validGuidance(g.Guidance) {
		return errors.New("guidance must be finite and at least 1")
	}
	if !validScheduler(g.Scheduler) {
		return errors.New("scheduler is not supported")
	}
	if g.Components != nil && (!validSelector(g.Components.VAE) || !validSelector(g.Components.Qwen3Encoder) ||
		!validSelector(g.Components.T5Encoder) || !validSelector(g.Components.CLIPEmbed)) {
		return errors.New("component selector cannot be empty")
	}
	return nil
}

func (u Upscale) validate() error {
	if !validSelector(u.Model) {
		return errors.New("model selector cannot be empty")
	}
	if u.Scale != nil && !slices.Contains([]int{2, 4, 8}, *u.Scale) {
		return errors.New("scale must be 2, 4, or 8")
	}
	if u.Creativity != nil && (*u.Creativity < -10 || *u.Creativity > 10) {
		return errors.New("creativity must be from -10 to 10")
	}
	if u.Structure != nil && (*u.Structure < -10 || *u.Structure > 10) {
		return errors.New("structure must be from -10 to 10")
	}
	if !validPositive(u.Steps) {
		return errors.New("steps must be positive")
	}
	if !validScheduler(u.Scheduler) {
		return errors.New("scheduler is not supported")
	}
	if !validGuidance(u.Guidance) {
		return errors.New("guidance must be finite and at least 1")
	}
	if u.TileSize != nil && (*u.TileSize < 512 || *u.TileSize > 1536 || *u.TileSize%64 != 0) {
		return errors.New("tile_size must be a multiple of 64 from 512 to 1536")
	}
	if u.TileOverlap != nil && (*u.TileOverlap < 16 || *u.TileOverlap > 512 || *u.TileOverlap%8 != 0) {
		return errors.New("tile_overlap must be a multiple of 8 from 16 to 512")
	}
	tileSize, overlap := 1024, 128
	if u.TileSize != nil {
		tileSize = *u.TileSize
	}
	if u.TileOverlap != nil {
		overlap = *u.TileOverlap
	}
	if overlap >= tileSize {
		return errors.New("tile_overlap must be less than tile_size")
	}
	if u.Components != nil && (!validSelector(u.Components.UpscaleModel) || !validSelector(u.Components.TileControlNet) || !validSelector(u.Components.VAE)) {
		return errors.New("component selector cannot be empty")
	}
	return nil
}

func directory() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user configuration directory: %w", err)
	}
	return filepath.Join(base, "bediz", "profiles"), nil
}

func path(name string) (string, error) {
	if !ValidName(name) {
		return "", errors.New("invalid profile name")
	}
	dir, err := directory()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".json"), nil
}

type ExistsError struct{ Name string }

func (e *ExistsError) Error() string { return fmt.Sprintf("profile %q already exists", e.Name) }

func Create(doc Document, replace bool) error {
	if err := Validate(doc); err != nil {
		return err
	}
	filename, err := path(doc.Name)
	if err != nil {
		return err
	}
	if !replace {
		if _, err := os.Lstat(filename); err == nil {
			return &ExistsError{Name: doc.Name}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(filename), ".profile-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if err := json.MarshalWrite(file, doc, json.Deterministic(true)); err != nil {
		file.Close()
		return err
	}
	if _, err := io.WriteString(file, "\n"); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if !replace {
		err := os.Link(file.Name(), filename)
		if errors.Is(err, os.ErrExist) {
			return &ExistsError{Name: doc.Name}
		}
		if err != nil {
			return err
		}
		return nil
	}
	return os.Rename(file.Name(), filename)
}

func Get(name string) (Document, error) {
	filename, err := path(name)
	if err != nil {
		return Document{}, err
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return Document{}, err
	}
	var doc Document
	if err := document.Decode("profile document", bytes.NewReader(data), &doc); err != nil {
		return Document{}, fmt.Errorf("invalid profile %q: %w", name, err)
	}
	if doc.Name != name {
		return Document{}, fmt.Errorf("invalid profile %q: stored name %q does not match", name, doc.Name)
	}
	if err := Validate(doc); err != nil {
		return Document{}, fmt.Errorf("invalid profile %q: %w", name, err)
	}
	return doc, nil
}

type Summary struct {
	Name     string   `json:"name"`
	Sections []string `json:"sections"`
}

func List() ([]Summary, error) {
	dir, err := directory()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Summary{}, nil
	}
	if err != nil {
		return nil, err
	}
	summaries := make([]Summary, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		if !ValidName(name) {
			continue
		}
		doc, err := Get(name)
		if err != nil {
			return nil, fmt.Errorf("profile %q: %w", name, err)
		}
		summary := Summary{Name: name, Sections: []string{}}
		if doc.Generate != nil {
			summary.Sections = append(summary.Sections, "generate")
		}
		if doc.Upscale != nil {
			summary.Sections = append(summary.Sections, "upscale")
		}
		summaries = append(summaries, summary)
	}
	// Filename order puts "a-b.json" before "a.json", so sort by the name itself.
	slices.SortFunc(summaries, func(a, b Summary) int { return strings.Compare(a.Name, b.Name) })
	return summaries, nil
}

func Delete(name string) error {
	filename, err := path(name)
	if err != nil {
		return err
	}
	return os.Remove(filename)
}
