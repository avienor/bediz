package generation

import (
	"errors"
	"fmt"
	"math"

	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/operation"
)

func resolveLoRAs(request []LoRA, inventory []ModelIdentifier, base string) ([]ResolvedLoRA, error) {
	if len(request) == 0 {
		return nil, nil
	}
	resolved := make([]ResolvedLoRA, 0, len(request))
	seen := make(map[string]bool, len(request))
	for index, lora := range request {
		model, err := graphops.ResolveUniqueCompatible(inventory, lora.Model, graphops.ComponentRequirement{Kind: "lora", Base: base, ModelType: "lora"})
		if err != nil {
			if _, ok := errors.AsType[*operation.InvalidRequestError](err); ok {
				return nil, operation.InvalidField(fmt.Sprintf("loras.%d.model", index), err.Error())
			}
			return nil, err
		}
		if seen[model.Key] {
			return nil, operation.InvalidField("loras", "the same LoRA model cannot be applied twice")
		}
		seen[model.Key] = true
		weight := 0.75
		if lora.Weight != nil {
			weight = *lora.Weight
		} else if model.DefaultSettings != nil && model.DefaultSettings.Weight != nil {
			weight = *model.DefaultSettings.Weight
			if math.IsNaN(weight) || math.IsInf(weight, 0) || weight < -10 || weight > 10 {
				return nil, operation.InvalidField(fmt.Sprintf("loras.%d.weight", index), "recorded LoRA default weight is outside -10 to 10; provide an explicit weight")
			}
		}
		resolved = append(resolved, ResolvedLoRA{Model: model, Weight: weight})
	}
	return resolved, nil
}

func validateLoRAFamily(request []LoRA, base string) error {
	if len(request) > 0 && base != "sdxl" && base != "anima" {
		return operation.UnsupportedCapability(fmt.Sprintf("LoRAs are not supported for %s generation", base))
	}
	return nil
}
