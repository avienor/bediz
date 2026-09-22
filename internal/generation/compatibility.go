package generation

import (
	"fmt"
	"strings"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/operation"
)

type openAPIDocument struct {
	Components struct {
		Schemas map[string]openAPISchema `json:"schemas"`
	} `json:"components"`
}

type openAPISchema struct {
	Properties map[string]openAPIProperty `json:"properties"`
}

type openAPIProperty struct {
	Const string `json:"const"`
}

func validateAnimaOpenAPI(document openAPIDocument) error {
	for _, requirement := range capability.AnimaGenerationEntry().Invocations {
		schema, ok := document.Components.Schemas[requirement.Schema]
		if !ok {
			return operation.UnsupportedCapability(fmt.Sprintf(
				"InvokeAI does not provide required invocation schema %s for %s", requirement.Schema, requirement.Type,
			))
		}
		if schema.Properties["type"].Const != requirement.Type {
			return operation.UnsupportedCapability(fmt.Sprintf(
				"InvokeAI invocation schema %s does not identify type %s", requirement.Schema, requirement.Type,
			))
		}
		missing := make([]string, 0)
		for _, property := range requirement.Properties {
			if _, ok := schema.Properties[property]; !ok {
				missing = append(missing, property)
			}
		}
		if len(missing) > 0 {
			return operation.UnsupportedCapability(fmt.Sprintf(
				"InvokeAI invocation schema %s for %s is missing required fields: %s",
				requirement.Schema, requirement.Type, strings.Join(missing, ", "),
			))
		}
	}
	return nil
}
