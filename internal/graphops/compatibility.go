package graphops

import (
	"context"
	"fmt"
	"strings"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
)

// CheckVersion restricts graph-producing operations to the tested range.
func CheckVersion(ctx context.Context, client *httpclient.Client) error {
	var response struct {
		Version string `json:"version"`
	}
	if err := client.GetJSON(ctx, "/api/v1/app/version", &response); err != nil {
		return err
	}
	supported, err := capability.SupportsInvokeAI(response.Version)
	if err != nil {
		return fmt.Errorf("validate InvokeAI version: %w", err)
	}
	if !supported {
		return operation.UnsupportedCapability(fmt.Sprintf("InvokeAI %s is outside the supported range %s", response.Version, capability.SupportedInvokeAIRange))
	}
	return nil
}

type openAPIDocument struct {
	Components struct {
		Schemas map[string]struct {
			AdditionalProperties bool `json:"additionalProperties"`
			Properties           map[string]struct {
				Const string `json:"const"`
			} `json:"properties"`
		} `json:"schemas"`
	} `json:"components"`
}

// CheckInvocations verifies the operation's invocation vocabulary in live OpenAPI.
func CheckInvocations(ctx context.Context, client *httpclient.Client, requirements []capability.InvocationRequirement) error {
	var document openAPIDocument
	if err := client.GetJSON(ctx, "/openapi.json", &document); err != nil {
		return err
	}
	for _, requirement := range requirements {
		schema, ok := document.Components.Schemas[requirement.Schema]
		if !ok {
			return operation.UnsupportedCapability(fmt.Sprintf("InvokeAI does not provide required invocation schema %s for %s", requirement.Schema, requirement.Type))
		}
		if schema.Properties["type"].Const != requirement.Type {
			return operation.UnsupportedCapability(fmt.Sprintf("InvokeAI invocation schema %s does not identify type %s", requirement.Schema, requirement.Type))
		}
		if requirement.RequiresAdditionalProperties && !schema.AdditionalProperties {
			return operation.UnsupportedCapability(fmt.Sprintf("InvokeAI invocation schema %s does not allow required upscale metadata fields", requirement.Schema))
		}
		missing := make([]string, 0)
		for _, property := range requirement.Properties {
			if _, ok := schema.Properties[property]; !ok {
				missing = append(missing, property)
			}
		}
		if len(missing) > 0 {
			return operation.UnsupportedCapability(fmt.Sprintf("InvokeAI invocation schema %s for %s is missing required fields: %s", requirement.Schema, requirement.Type, strings.Join(missing, ", ")))
		}
	}
	return nil
}
