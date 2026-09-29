package doctor

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/compatibility"
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
	Endpoints   []EndpointCheck   `json:"required_endpoints,omitempty"`
	Invocations []InvocationCheck `json:"required_invocations,omitempty"`
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
	Key     string `json:"key"`
	Name    string `json:"name"`
	Base    string `json:"base"`
	Type    string `json:"type"`
	Format  string `json:"format,omitempty"`
	Variant string `json:"variant,omitempty"`
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
	Mode       string   `json:"mode,omitempty"`
	Setting    string   `json:"setting,omitempty"`
	Compatible bool     `json:"compatible"`
	UISync     string   `json:"ui_sync,omitempty"`
	Failures   []string `json:"failures"`
}

type Issue struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type modelList struct {
	Models []compatibility.Model `json:"models"`
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
			report.Issues = append(report.Issues, Issue{Code: result.CodeInvalidVersionResponse, Message: "InvokeAI version response did not contain a version"})
		} else if supported, err := capability.SupportsInvokeAI(versionResponse.Version); err != nil {
			report.Issues = append(report.Issues, Issue{Code: result.CodeInvalidInvokeAIVersion, Message: err.Error()})
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

	var document compatibility.Document
	openAPIErr := client.GetJSON(ctx, "/openapi.json", &document)
	if openAPIErr == nil {
		report.OpenAPI.Available = true
		if report.InvokeAI.ConnectionStatus == "unknown" {
			report.InvokeAI.ConnectionStatus = "ok"
		}
	} else {
		appendRequestIssue(&report, "openapi", openAPIErr)
	}
	if openAPIErr == nil {
		report.OpenAPI.Endpoints, report.OpenAPI.Invocations = inspectOpenAPI(document)
		for _, check := range report.OpenAPI.Endpoints {
			if !check.Available {
				report.Issues = append(report.Issues, Issue{
					Code:    "missing_endpoint",
					Message: fmt.Sprintf("required endpoint %s %s is not available", check.Method, check.Path),
				})
			}
		}
		for _, requirement := range uniqueInvocations(false) {
			inspection := compatibility.InspectInvocation(document, requirement)
			check := InvocationCheck{Schema: requirement.Schema, Type: requirement.Type, Available: inspection.Available, MissingProperties: inspection.MissingProperties}
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
	report.Capabilities = buildCapabilities(compatibility.Snapshot{
		SupportedVersion: report.InvokeAI.SupportedVersion,
		OpenAPIAvailable: report.OpenAPI.Available,
		Document:         document,
		ModelsAvailable:  report.Models.Available,
		Models:           models.Models,
	})
	for _, entry := range report.Capabilities {
		if entry.UISync != "" {
			report.UISync[entry.Operation] = entry.UISync
		}
	}
	report.Ready = len(report.Issues) == 0 && len(report.Capabilities) > 0
	for _, entry := range report.Capabilities {
		if !entry.Compatible && entry.Setting == "" {
			report.Ready = false
		}
	}
	return report
}

func inspectOpenAPI(document compatibility.Document) ([]EndpointCheck, []InvocationCheck) {
	endpointRequirements := uniqueEndpoints()
	endpoints := make([]EndpointCheck, 0, len(endpointRequirements))
	for _, requirement := range endpointRequirements {
		endpoints = append(endpoints, EndpointCheck{Method: requirement.Method, Path: requirement.Path, Available: compatibility.EndpointAvailable(document, requirement)})
	}

	invocationRequirements := uniqueInvocations(true)
	invocations := make([]InvocationCheck, 0, len(invocationRequirements))
	for _, requirement := range invocationRequirements {
		result := compatibility.InspectInvocation(document, requirement)
		check := InvocationCheck{
			Schema:            requirement.Schema,
			Type:              requirement.Type,
			Available:         result.Available,
			MissingProperties: result.MissingProperties,
		}
		invocations = append(invocations, check)
	}
	return endpoints, invocations
}

func inspectModels(models []compatibility.Model) ([]ModelSummary, []ModelRequirement) {
	requirements := uniqueModelRequirements()
	relevant := make([]ModelSummary, 0)
	seen := make(map[string]bool)
	checks := make([]ModelRequirement, 0, len(requirements))
	for _, requirement := range requirements {
		check := compatibility.InspectModel(models, requirement)
		for _, model := range models {
			if compatibility.ModelMatches(model, requirement) {
				if !seen[model.Key] {
					relevant = append(relevant, ModelSummary{Key: model.Key, Name: model.Name, Base: model.Base, Type: model.Type, Format: model.Format, Variant: model.Variant})
					seen[model.Key] = true
				}
			}
		}
		checks = append(checks, ModelRequirement{
			Name:      requirement.Name,
			Available: check.Available,
			Required:  check.Required,
			Satisfied: check.Satisfied,
		})
	}
	slices.SortFunc(relevant, func(a, b ModelSummary) int {
		return cmp.Or(cmp.Compare(a.Type, b.Type), cmp.Compare(a.Name, b.Name))
	})
	return relevant, checks
}

func buildCapabilities(snapshot compatibility.Snapshot) []CapabilityReport {
	capabilities := make([]CapabilityReport, 0, len(capability.Matrix))
	for _, entry := range capability.Matrix {
		failures := make([]string, 0)
		for _, failure := range compatibility.Evaluate(entry, snapshot) {
			failures = append(failures, failure.Code)
		}
		capabilities = append(capabilities, CapabilityReport{
			Operation:  entry.Operation,
			Family:     entry.Family,
			Mode:       entry.Mode,
			Setting:    entry.Setting,
			Compatible: len(failures) == 0,
			Failures:   failures,
		})
	}
	recallReady := false
	for _, entry := range capabilities {
		if entry.Operation == result.OperationRecall && entry.Compatible {
			recallReady = true
		}
	}
	if recallReady {
		for i := range capabilities {
			if capabilities[i].Compatible && capability.Matrix[i].UISync != "" {
				capabilities[i].UISync = capability.Matrix[i].UISync
			}
		}
	}
	return capabilities
}

func appendRequestIssue(report *Report, check string, err error) {
	httpErr, httpErrMatched := errors.AsType[*httpclient.HTTPError](err)
	networkErr, networkErrMatched := errors.AsType[*httpclient.NetworkError](err)
	switch {
	case httpErrMatched && httpErr.AuthenticationFailure():
		report.Issues = append(report.Issues, Issue{Code: result.CodeAuthenticationFailed, Message: fmt.Sprintf("%s check was rejected by InvokeAI", check), Details: map[string]any{"status": httpErr.StatusCode}})
	case networkErrMatched:
		report.Issues = append(report.Issues, Issue{Code: result.CodeConnectionFailed, Message: fmt.Sprintf("%s check could not reach InvokeAI", check), Details: map[string]any{"error": networkErr.Err.Error()}})
	case httpErrMatched:
		report.Issues = append(report.Issues, Issue{Code: result.CodeInvokeAIHTTPError, Message: fmt.Sprintf("%s check failed", check), Details: map[string]any{"status": httpErr.StatusCode}})
	default:
		report.Issues = append(report.Issues, Issue{Code: result.CodeInvalidInvokeAIResponse, Message: fmt.Sprintf("%s check failed: %v", check, err)})
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

func uniqueInvocations(includeSettings bool) []capability.InvocationRequirement {
	seen := make(map[string]int)
	var requirements []capability.InvocationRequirement
	for _, entry := range capability.Matrix {
		if !includeSettings && entry.Setting != "" {
			continue
		}
		for _, requirement := range entry.Invocations {
			if index, ok := seen[requirement.Schema]; ok {
				requirements[index].RequiresAdditionalProperties = requirements[index].RequiresAdditionalProperties || requirement.RequiresAdditionalProperties
				for _, property := range requirement.Properties {
					if !slices.Contains(requirements[index].Properties, property) {
						requirements[index].Properties = append(requirements[index].Properties, property)
					}
				}
			} else {
				seen[requirement.Schema] = len(requirements)
				requirements = append(requirements, requirement)
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

// Failure reports the structured failure that decides doctor's outcome, or nil
// when the diagnostic run is ready. The process exit status follows from the
// error code.
func Failure(report Report) *result.Error {
	for _, issue := range report.Issues {
		if issue.Code == result.CodeConnectionFailed || issue.Code == result.CodeAuthenticationFailed {
			return &result.Error{Code: issue.Code, Message: issue.Message}
		}
	}
	for _, issue := range report.Issues {
		if issue.Code == result.CodeInvokeAIHTTPError ||
			issue.Code == result.CodeInvalidInvokeAIResponse ||
			issue.Code == result.CodeInvalidVersionResponse ||
			issue.Code == result.CodeInvalidInvokeAIVersion {
			return &result.Error{Code: issue.Code, Message: issue.Message}
		}
	}
	if !report.Ready {
		return &result.Error{Code: result.CodeUnsupportedCapability, Message: "InvokeAI is not ready for the registered Bediz capabilities"}
	}
	return nil
}

// Human writes the concise diagnostic report and returns any output failure.
func (r Report) Human(w io.Writer) error {
	status := "ready"
	if !r.Ready {
		status = "not ready"
	}
	if _, err := fmt.Fprintf(w, "Bediz %s\n", r.Bediz.Version); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "InvokeAI %s (%s, supported: %t)\n", valueOrUnknown(r.InvokeAI.Version), r.InvokeAI.URL, r.InvokeAI.SupportedVersion); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Connection: %s; authentication: %s\n", r.InvokeAI.ConnectionStatus, r.InvokeAI.AuthenticationStatus); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "OpenAPI: %t; models: %d\n", r.OpenAPI.Available, r.Models.Total); err != nil {
		return err
	}
	for _, entry := range r.Capabilities {
		name := entry.Operation
		if entry.Family != "" {
			name += "/" + entry.Family
		}
		if entry.Mode == "img2img" {
			name += "/img2img"
		}
		if entry.Setting != "" {
			name += "/" + entry.Setting
		}
		if entry.UISync != "" {
			if _, err := fmt.Fprintf(w, "%s compatible: %t (UI sync: %s)\n", name, entry.Compatible, entry.UISync); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintf(w, "%s compatible: %t\n", name, entry.Compatible); err != nil {
				return err
			}
		}
		for _, failure := range entry.Failures {
			if _, err := fmt.Fprintf(w, "  - %s\n", failure); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintf(w, "Status: %s\n", status)
	return err
}

func valueOrUnknown(value string) string {
	return cmp.Or(value, "unknown")
}
