// Package graphops contains the InvokeAI plumbing shared by graph-producing operations.
// It does not choose which model families or components an operation accepts.
package graphops

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
)

type ModelIdentifier struct {
	Key     string `json:"key"`
	Hash    string `json:"hash"`
	Name    string `json:"name"`
	Base    string `json:"base"`
	Type    string `json:"type"`
	Variant string `json:"variant,omitempty"`
	Format  string `json:"format,omitempty"`
}

// Inventory reads the installed models without applying an operation's family registry.
func Inventory(ctx context.Context, client *httpclient.Client) ([]ModelIdentifier, error) {
	var response struct {
		Models []ModelIdentifier `json:"models"`
	}
	if err := client.GetJSON(ctx, "/api/v2/models/", &response); err != nil {
		return nil, err
	}
	return response.Models, nil
}

// ResolveMain selects an installed main model by exact key or unique name.
func ResolveMain(inventory []ModelIdentifier, selector string) (ModelIdentifier, error) {
	for _, model := range inventory {
		if model.Key == selector {
			if model.Type != "main" {
				return ModelIdentifier{}, operation.UnsupportedCapability(fmt.Sprintf("model %q has type %q; expected type %q", model.Key, model.Type, "main"))
			}
			return model, nil
		}
	}
	var matches []ModelIdentifier
	for _, model := range inventory {
		if model.Type == "main" && model.Name == selector {
			matches = append(matches, model)
		}
	}
	if len(matches) > 1 {
		candidates, err := SelectionCandidates(matches)
		if err != nil {
			return ModelIdentifier{}, err
		}
		return ModelIdentifier{}, operation.SelectionRequired("main_model", selector, candidates)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return ModelIdentifier{}, operation.InvalidRequest(fmt.Sprintf("model selector %q did not resolve to an installed model", selector))
}

type ComponentRequirement struct {
	Kind      string
	Base      string
	ModelType string
}

// ResolveComponent follows the exact selector and compatible component rules.
func ResolveComponent(inventory []ModelIdentifier, selector string, requirement ComponentRequirement) (ModelIdentifier, error) {
	if selector != "" {
		return ResolveUniqueCompatible(inventory, selector, requirement)
	}
	return ResolveOnlyCompatible(inventory, requirement)
}

func ResolveUniqueCompatible(inventory []ModelIdentifier, selector string, requirement ComponentRequirement) (ModelIdentifier, error) {
	for _, model := range inventory {
		if model.Key != selector {
			continue
		}
		if err := ValidateModelCompatibility(model, requirement); err != nil {
			return ModelIdentifier{}, err
		}
		return CompleteModelIdentifier(model)
	}
	var matches []ModelIdentifier
	for _, model := range inventory {
		if model.Name == selector {
			matches = append(matches, model)
		}
	}
	if len(matches) == 1 {
		if err := ValidateModelCompatibility(matches[0], requirement); err != nil {
			return ModelIdentifier{}, err
		}
		return CompleteModelIdentifier(matches[0])
	}
	if len(matches) > 1 {
		candidates, err := SelectionCandidates(matches)
		if err != nil {
			return ModelIdentifier{}, err
		}
		return ModelIdentifier{}, operation.SelectionRequired(requirement.Kind, selector, candidates)
	}
	return ModelIdentifier{}, operation.InvalidRequest(fmt.Sprintf("model selector %q did not resolve to an installed model", selector))
}

func ResolveOnlyCompatible(inventory []ModelIdentifier, requirement ComponentRequirement) (ModelIdentifier, error) {
	var matches []ModelIdentifier
	for _, model := range inventory {
		if model.Base == requirement.Base && model.Type == requirement.ModelType {
			matches = append(matches, model)
		}
	}
	if len(matches) == 1 {
		return CompleteModelIdentifier(matches[0])
	}
	if len(matches) > 1 {
		candidates, err := SelectionCandidates(matches)
		if err != nil {
			return ModelIdentifier{}, err
		}
		return ModelIdentifier{}, operation.SelectionRequired(requirement.Kind, "", candidates)
	}
	guidance := fmt.Sprintf("install an InvokeAI model with base %q and type %q", requirement.Base, requirement.ModelType)
	return ModelIdentifier{}, operation.MissingComponent(requirement.Kind, requirement.Base, requirement.ModelType, guidance)
}

func ValidateModelCompatibility(model ModelIdentifier, requirement ComponentRequirement) error {
	if model.Base == requirement.Base && model.Type == requirement.ModelType {
		return nil
	}
	return operation.UnsupportedCapability(fmt.Sprintf(
		"model %q has base %q and type %q; expected base %q and type %q",
		model.Key, model.Base, model.Type, requirement.Base, requirement.ModelType,
	))
}

func CompleteModelIdentifier(model ModelIdentifier) (ModelIdentifier, error) {
	if model.Key == "" || model.Hash == "" || model.Name == "" || model.Base == "" || model.Type == "" {
		return ModelIdentifier{}, operation.UnsupportedCapability(fmt.Sprintf("installed model %q does not provide a complete InvokeAI model identifier", model.Key))
	}
	return model, nil
}

func SelectionCandidates(models []ModelIdentifier) ([]operation.SelectionCandidate, error) {
	for _, model := range models {
		if _, err := CompleteModelIdentifier(model); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(models, func(a, b ModelIdentifier) int { return cmp.Compare(a.Key, b.Key) })
	candidates := make([]operation.SelectionCandidate, 0, len(models))
	for _, model := range models {
		candidates = append(candidates, operation.SelectionCandidate{Key: model.Key, Name: model.Name, Base: model.Base, Type: model.Type})
	}
	return candidates, nil
}
