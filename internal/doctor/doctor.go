package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/version"
)

type Report struct {
	Ready        bool               `json:"ready"`
	Bediz        version.Info       `json:"bediz"`
	InvokeAI     InvokeAIReport     `json:"invokeai"`
	OpenAPI      OpenAPIReport      `json:"openapi"`
	Models       ModelsReport       `json:"models"`
	Capabilities []CapabilityReport `json:"capabilities"`
	UISync       map[string]string  `json:"ui_sync"`
	Issues       []Issue            `json:"issues"`
}

type InvokeAIReport struct {
	URL                  string `json:"url"`
	Version              string `json:"version,omitempty"`
	SupportedVersion     bool   `json:"supported_version"`
	SupportedRange       string `json:"supported_range"`
	ConnectionStatus     string `json:"connection_status"`
	AuthenticationStatus string `json:"authentication_status"`
	TokenConfigured      bool   `json:"token_configured"`
}

type OpenAPIReport struct {
	Available   bool              `json:"available"`
	Endpoints   []EndpointCheck   `json:"required_endpoints"`
	Invocations []InvocationCheck `json:"required_invocations"`
}

type EndpointCheck struct {
	Method    string `json:"method"`
	Path      string `json:"path"`
	Available bool   `json:"available"`
}

type InvocationCheck struct {
	Schema            string   `json:"schema"`
	Type              string   `json:"type"`
	Available         bool     `json:"available"`
	MissingProperties []string `json:"missing_properties"`
}

type ModelsReport struct {
	Available    bool               `json:"available"`
	Total        int                `json:"total"`
	Relevant     []ModelSummary     `json:"relevant"`
	Requirements []ModelRequirement `json:"requirements"`
}

type ModelSummary struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Base   string `json:"base"`
	Type   string `json:"type"`
	Format string `json:"format,omitempty"`
}

type ModelRequirement struct {
	Name      string `json:"name"`
	Available int    `json:"available"`
	Required  int    `json:"required"`
	Satisfied bool   `json:"satisfied"`
}

type CapabilityReport struct {
	Operation  string   `json:"operation"`
	Family     string   `json:"family,omitempty"`
	Compatible bool     `json:"compatible"`
	UISync     string   `json:"ui_sync,omitempty"`
	Failures   []string `json:"failures"`
}

type Issue struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type openAPIDocument struct {
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
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

type modelList struct {
	Models []ModelSummary `json:"models"`
}

type appVersion struct {
	Version string `json:"version"`
}

func Run(ctx context.Context, client *httpclient.Client, bedizVersion version.Info) Report {
	report := Report{
		Bediz: bedizVersion,
		InvokeAI: InvokeAIReport{
			URL:                  client.BaseURL(),
			SupportedRange:       capability.SupportedInvokeAIRange,
			ConnectionStatus:     "unknown",
			AuthenticationStatus: "unknown",
			TokenConfigured:      client.HasToken(),
		},
		OpenAPI: OpenAPIReport{
			Endpoints:   []EndpointCheck{},
			Invocations: []InvocationCheck{},
		},
		Models: ModelsReport{
			Relevant:     []ModelSummary{},
			Requirements: []ModelRequirement{},
		},
		Capabilities: []CapabilityReport{},
		UISync:       map[string]string{},
		Issues:       []Issue{},
	}

	var versionResponse appVersion
	versionErr := client.GetJSON(ctx, "/api/v1/app/version", &versionResponse)
	if versionErr == nil {
		report.InvokeAI.ConnectionStatus = "ok"
		report.InvokeAI.Version = versionResponse.Version
		if versionResponse.Version == "" {
			report.Issues = append(report.Issues, Issue{Code: "invalid_version_response", Message: "InvokeAI version response did not contain a version"})
		} else if supported, err := capability.SupportsInvokeAI(versionResponse.Version); err != nil {
			report.Issues = append(report.Issues, Issue{Code: "invalid_invokeai_version", Message: err.Error()})
		} else {
			report.InvokeAI.SupportedVersion = supported
			if !supported {
				report.Issues = append(report.Issues, Issue{
					Code:    "unsupported_invokeai_version",
					Message: fmt.Sprintf("InvokeAI %s is outside the supported range %s", versionResponse.Version, capability.SupportedInvokeAIRange),
				})
			}
		}
	} else {
		appendRequestIssue(&report, "version", versionErr)
	}

	var document openAPIDocument
	openAPIErr := client.GetJSON(ctx, "/openapi.json", &document)
	if openAPIErr == nil {
		report.OpenAPI.Available = true
		if report.InvokeAI.ConnectionStatus == "unknown" {
			report.InvokeAI.ConnectionStatus = "ok"
		}
	} else {
		appendRequestIssue(&report, "openapi", openAPIErr)
	}
	report.OpenAPI.Endpoints, report.OpenAPI.Invocations = inspectOpenAPI(document)
	if openAPIErr == nil {
		for _, check := range report.OpenAPI.Endpoints {
			if !check.Available {
				report.Issues = append(report.Issues, Issue{
					Code:    "missing_endpoint",
					Message: fmt.Sprintf("required endpoint %s %s is not available", check.Method, check.Path),
				})
			}
		}
		for _, check := range report.OpenAPI.Invocations {
			if !check.Available || len(check.MissingProperties) > 0 {
				report.Issues = append(report.Issues, Issue{
					Code:    "incompatible_invocation",
					Message: fmt.Sprintf("required invocation %s is unavailable or incompatible", check.Type),
					Details: map[string]any{"schema": check.Schema, "missing_properties": check.MissingProperties},
				})
			}
		}
	}

	var models modelList
	modelsErr := client.GetJSON(ctx, "/api/v2/models/", &models)
	if modelsErr == nil {
		report.Models.Available = true
		report.Models.Total = len(models.Models)
		if client.HasToken() {
			report.InvokeAI.AuthenticationStatus = "accepted"
		} else {
			report.InvokeAI.AuthenticationStatus = "not_required"
		}
		if report.InvokeAI.ConnectionStatus == "unknown" {
			report.InvokeAI.ConnectionStatus = "ok"
		}
	} else {
		if httpErr, ok := errors.AsType[*httpclient.HTTPError](modelsErr); ok && httpErr.AuthenticationFailure() {
			report.InvokeAI.AuthenticationStatus = "rejected"
		} else {
			report.InvokeAI.AuthenticationStatus = "unknown"
		}
		appendRequestIssue(&report, "models", modelsErr)
	}
	report.Models.Relevant, report.Models.Requirements = inspectModels(models.Models)
	if modelsErr == nil {
		for _, requirement := range report.Models.Requirements {
			if !requirement.Satisfied {
				report.Issues = append(report.Issues, Issue{
					Code:    "missing_component",
					Message: fmt.Sprintf("%s is required", requirement.Name),
					Details: map[string]any{"available": requirement.Available, "required": requirement.Required},
				})
			}
		}
	}

	if report.InvokeAI.ConnectionStatus == "unknown" {
		report.InvokeAI.ConnectionStatus = "failed"
	}
	report.Capabilities = buildCapabilities(report)
	for _, entry := range capability.Matrix {
		if entry.UISync != "" {
			report.UISync[entry.Operation] = entry.UISync
		}
	}
	report.Ready = len(report.Capabilities) > 0
	for _, entry := range report.Capabilities {
		if !entry.Compatible {
			report.Ready = false
		}
	}
	return report
}

func inspectOpenAPI(document openAPIDocument) ([]EndpointCheck, []InvocationCheck) {
	endpointRequirements := uniqueEndpoints()
	endpoints := make([]EndpointCheck, 0, len(endpointRequirements))
	for _, requirement := range endpointRequirements {
		methods := document.Paths[requirement.Path]
		_, available := methods[strings.ToLower(requirement.Method)]
		endpoints = append(endpoints, EndpointCheck{Method: requirement.Method, Path: requirement.Path, Available: available})
	}

	invocationRequirements := uniqueInvocations()
	invocations := make([]InvocationCheck, 0, len(invocationRequirements))
	for _, requirement := range invocationRequirements {
		schema, exists := document.Components.Schemas[requirement.Schema]
		check := InvocationCheck{
			Schema:            requirement.Schema,
			Type:              requirement.Type,
			Available:         exists && schema.Properties["type"].Const == requirement.Type,
			MissingProperties: []string{},
		}
		for _, property := range requirement.Properties {
			if _, ok := schema.Properties[property]; !ok {
				check.MissingProperties = append(check.MissingProperties, property)
			}
		}
		invocations = append(invocations, check)
	}
	return endpoints, invocations
}

func inspectModels(models []ModelSummary) ([]ModelSummary, []ModelRequirement) {
	requirements := uniqueModelRequirements()
	relevant := make([]ModelSummary, 0)
	seen := make(map[string]bool)
	checks := make([]ModelRequirement, 0, len(requirements))
	for _, requirement := range requirements {
		count := 0
		for _, model := range models {
			if modelMatches(model, requirement) {
				count++
				if !seen[model.Key] {
					relevant = append(relevant, model)
					seen[model.Key] = true
				}
			}
		}
		checks = append(checks, ModelRequirement{
			Name:      requirement.Name,
			Available: count,
			Required:  requirement.MinimumCount,
			Satisfied: count >= requirement.MinimumCount,
		})
	}
	sort.Slice(relevant, func(i, j int) bool {
		if relevant[i].Type == relevant[j].Type {
			return relevant[i].Name < relevant[j].Name
		}
		return relevant[i].Type < relevant[j].Type
	})
	return relevant, checks
}

func buildCapabilities(report Report) []CapabilityReport {
	capabilities := make([]CapabilityReport, 0, len(capability.Matrix))
	for _, entry := range capability.Matrix {
		failures := make([]string, 0)
		if entry.VersionPolicy == capability.VersionPolicySupportedRange && !report.InvokeAI.SupportedVersion {
			failures = append(failures, "unsupported_version")
		}
		if !report.OpenAPI.Available {
			failures = append(failures, "openapi_unavailable")
		} else {
			for _, requirement := range entry.Endpoints {
				if !endpointAvailable(report.OpenAPI.Endpoints, requirement) {
					failures = append(failures, "missing_endpoint:"+requirement.Method+" "+requirement.Path)
				}
			}
			for _, requirement := range entry.Invocations {
				if !invocationAvailable(report.OpenAPI.Invocations, requirement.Schema) {
					failures = append(failures, "incompatible_invocation:"+requirement.Type)
				}
			}
		}
		if len(entry.Models) > 0 {
			if !report.Models.Available {
				failures = append(failures, "models_unavailable")
			} else {
				for _, requirement := range entry.Models {
					if !modelRequirementSatisfied(report.Models.Requirements, requirement.Name) {
						failures = append(failures, "missing_component:"+requirement.Name)
					}
				}
			}
		}
		capabilities = append(capabilities, CapabilityReport{
			Operation:  entry.Operation,
			Family:     entry.Family,
			Compatible: len(failures) == 0,
			UISync:     entry.UISync,
			Failures:   failures,
		})
	}
	return capabilities
}

func appendRequestIssue(report *Report, check string, err error) {
	httpErr, httpErrMatched := errors.AsType[*httpclient.HTTPError](err)
	networkErr, networkErrMatched := errors.AsType[*httpclient.NetworkError](err)
	switch {
	case httpErrMatched && httpErr.AuthenticationFailure():
		report.Issues = append(report.Issues, Issue{Code: "authentication_failed", Message: fmt.Sprintf("%s check was rejected by InvokeAI", check), Details: map[string]any{"status": httpErr.StatusCode}})
	case networkErrMatched:
		report.Issues = append(report.Issues, Issue{Code: "connection_failed", Message: fmt.Sprintf("%s check could not reach InvokeAI", check), Details: map[string]any{"error": networkErr.Err.Error()}})
	case httpErrMatched:
		report.Issues = append(report.Issues, Issue{Code: "invokeai_http_error", Message: fmt.Sprintf("%s check failed", check), Details: map[string]any{"status": httpErr.StatusCode}})
	default:
		report.Issues = append(report.Issues, Issue{Code: "invalid_invokeai_response", Message: fmt.Sprintf("%s check failed: %v", check, err)})
	}
}

func uniqueEndpoints() []capability.EndpointRequirement {
	seen := make(map[string]bool)
	var requirements []capability.EndpointRequirement
	for _, entry := range capability.Matrix {
		for _, requirement := range entry.Endpoints {
			key := requirement.Method + " " + requirement.Path
			if !seen[key] {
				requirements = append(requirements, requirement)
				seen[key] = true
			}
		}
	}
	return requirements
}

func uniqueInvocations() []capability.InvocationRequirement {
	seen := make(map[string]bool)
	var requirements []capability.InvocationRequirement
	for _, entry := range capability.Matrix {
		for _, requirement := range entry.Invocations {
			if !seen[requirement.Schema] {
				requirements = append(requirements, requirement)
				seen[requirement.Schema] = true
			}
		}
	}
	return requirements
}

func uniqueModelRequirements() []capability.ModelRequirement {
	seen := make(map[string]bool)
	var requirements []capability.ModelRequirement
	for _, entry := range capability.Matrix {
		for _, requirement := range entry.Models {
			if !seen[requirement.Name] {
				requirements = append(requirements, requirement)
				seen[requirement.Name] = true
			}
		}
	}
	return requirements
}

func modelMatches(model ModelSummary, requirement capability.ModelRequirement) bool {
	return contains(requirement.Types, model.Type) && contains(requirement.Bases, model.Base)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func endpointAvailable(checks []EndpointCheck, requirement capability.EndpointRequirement) bool {
	for _, check := range checks {
		if check.Method == requirement.Method && check.Path == requirement.Path {
			return check.Available
		}
	}
	return false
}

func invocationAvailable(checks []InvocationCheck, schema string) bool {
	for _, check := range checks {
		if check.Schema == schema {
			return check.Available && len(check.MissingProperties) == 0
		}
	}
	return false
}

func modelRequirementSatisfied(checks []ModelRequirement, name string) bool {
	for _, check := range checks {
		if check.Name == name {
			return check.Satisfied
		}
	}
	return false
}

func Failure(report Report) (int, string, string) {
	for _, issue := range report.Issues {
		if issue.Code == "connection_failed" || issue.Code == "authentication_failed" {
			return result.ExitConnection, issue.Code, issue.Message
		}
	}
	for _, issue := range report.Issues {
		if issue.Code == "invokeai_http_error" ||
			issue.Code == "invalid_invokeai_response" ||
			issue.Code == "invalid_version_response" ||
			issue.Code == "invalid_invokeai_version" {
			return result.ExitInvokeAIFailure, issue.Code, issue.Message
		}
	}
	if !report.Ready {
		return result.ExitUnsupportedCapability, "unsupported_capability", "InvokeAI is not ready for the registered Bediz capabilities"
	}
	return result.ExitSuccess, "", ""
}

func (r Report) Human(w io.Writer) {
	status := "ready"
	if !r.Ready {
		status = "not ready"
	}
	fmt.Fprintf(w, "Bediz %s\n", r.Bediz.Version)
	fmt.Fprintf(w, "InvokeAI %s (%s, supported: %t)\n", valueOrUnknown(r.InvokeAI.Version), r.InvokeAI.URL, r.InvokeAI.SupportedVersion)
	fmt.Fprintf(w, "Connection: %s; authentication: %s\n", r.InvokeAI.ConnectionStatus, r.InvokeAI.AuthenticationStatus)
	fmt.Fprintf(w, "OpenAPI: %t; models: %d\n", r.OpenAPI.Available, r.Models.Total)
	for _, entry := range r.Capabilities {
		name := entry.Operation
		if entry.Family != "" {
			name += "/" + entry.Family
		}
		if entry.UISync != "" {
			fmt.Fprintf(w, "%s compatible: %t (UI sync: %s)\n", name, entry.Compatible, entry.UISync)
		} else {
			fmt.Fprintf(w, "%s compatible: %t\n", name, entry.Compatible)
		}
		for _, failure := range entry.Failures {
			fmt.Fprintf(w, "  - %s\n", failure)
		}
	}
	fmt.Fprintf(w, "Status: %s\n", status)
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}
