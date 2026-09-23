package graphops_test

import (
	"errors"
	"testing"

	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/operation"
)

func TestResolveMainSelectsInstalledSD1ModelWithoutGenerationFamily(t *testing.T) {
	inventory := []graphops.ModelIdentifier{
		{Key: "sd15-key", Hash: "sd15-hash", Name: "Dreamshaper 8", Base: "sd-1", Type: "main"},
	}
	for _, selector := range []string{"sd15-key", "Dreamshaper 8"} {
		model, err := graphops.ResolveMain(inventory, selector)
		if err != nil {
			t.Fatalf("selector %q: %v", selector, err)
		}
		if model != inventory[0] {
			t.Fatalf("selector %q: got %#v, want %#v", selector, model, inventory[0])
		}
	}
}

func TestResolveMainRequiresExactSelectionForSharedName(t *testing.T) {
	_, err := graphops.ResolveMain([]graphops.ModelIdentifier{
		{Key: "sd15-key", Hash: "sd15-hash", Name: "Shared", Base: "sd-1", Type: "main"},
		{Key: "sdxl-key", Hash: "sdxl-hash", Name: "Shared", Base: "sdxl", Type: "main"},
	}, "Shared")
	selection, ok := errors.AsType[*operation.SelectionRequiredError](err)
	if !ok || selection.Kind != "main_model" || len(selection.Candidates) != 2 || selection.Candidates[0].Key != "sd15-key" || selection.Candidates[1].Key != "sdxl-key" {
		t.Fatalf("selection = %#v, error = %v", selection, err)
	}
}
