// Package compatibility evaluates tested capabilities against an InvokeAI installation snapshot.
package compatibility

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"slices"
	"strings"

	"github.com/avienor/bediz/internal/capability"
)

type Document struct {
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components struct {
		Schemas map[string]Schema `json:"schemas"`
	} `json:"components"`
}

type Schema struct {
	Properties           map[string]Property `json:"properties"`
	Required             []string            `json:"required"`
	AdditionalProperties bool                `json:"additionalProperties"`
}

type Property struct {
	Const string                               `json:"const"`
	Type  string                               `json:"type"`
	AnyOf []capability.RecallSchemaAlternative `json:"anyOf"`
}

type Model struct {
	Key     string `json:"key"`
	Hash    string `json:"hash"`
	Name    string `json:"name"`
	Base    string `json:"base"`
	Type    string `json:"type"`
	Format  string `json:"format"`
	Variant string `json:"variant"`
}

type Snapshot struct {
	SupportedVersion bool
	OpenAPIAvailable bool
	Document         Document
	ModelsAvailable  bool
	Models           []Model
}

type Failure struct {
	Code    string
	Message string
}

type InvocationCheck struct {
	Available         bool
	MissingProperties []string
	Message           string
}

type ModelCheck struct {
	Available int
	Required  int
	Satisfied bool
}

func EndpointAvailable(document Document, requirement capability.EndpointRequirement) bool {
	_, ok := document.Paths[requirement.Path][strings.ToLower(requirement.Method)]
	return ok
}

func InspectInvocation(document Document, requirement capability.InvocationRequirement) InvocationCheck {
	schema, exists := document.Components.Schemas[requirement.Schema]
	check := InvocationCheck{Available: exists && schema.Properties["type"].Const == requirement.Type, MissingProperties: []string{}}
	if !exists {
		check.Message = fmt.Sprintf("InvokeAI does not provide required invocation schema %s for %s", requirement.Schema, requirement.Type)
	} else if !check.Available {
		check.Message = fmt.Sprintf("InvokeAI invocation schema %s does not identify type %s", requirement.Schema, requirement.Type)
	} else if requirement.RequiresAdditionalProperties && !schema.AdditionalProperties {
		check.Message = fmt.Sprintf("InvokeAI invocation schema %s does not allow required upscale metadata fields", requirement.Schema)
	}
	for _, property := range requirement.Properties {
		if _, ok := schema.Properties[property]; !ok {
			check.MissingProperties = append(check.MissingProperties, property)
		}
	}
	if requirement.RequiresAdditionalProperties && !schema.AdditionalProperties {
		check.MissingProperties = append(check.MissingProperties, "additionalProperties")
	}
	if check.Message == "" && len(check.MissingProperties) > 0 {
		check.Message = fmt.Sprintf("InvokeAI invocation schema %s for %s is missing required fields: %s", requirement.Schema, requirement.Type, strings.Join(check.MissingProperties, ", "))
	}
	return check
}

func InspectModel(models []Model, requirement capability.ModelRequirement) ModelCheck {
	count := 0
	for _, model := range models {
		if ModelMatches(model, requirement) {
			count++
		}
	}
	return ModelCheck{Available: count, Required: requirement.MinimumCount, Satisfied: count >= requirement.MinimumCount}
}

func ModelMatches(model Model, requirement capability.ModelRequirement) bool {
	return model.Key != "" && model.Hash != "" && model.Name != "" && model.Base != "" && model.Type != "" &&
		slices.Contains(requirement.Types, model.Type) && slices.Contains(requirement.Bases, model.Base) &&
		(len(requirement.Variants) == 0 || slices.Contains(requirement.Variants, model.Variant)) &&
		(len(requirement.Formats) == 0 || slices.Contains(requirement.Formats, model.Format))
}

// Evaluate returns all failures in the order used by doctor. One decoded
// document can be evaluated against every registered capability entry.
func Evaluate(entry capability.Entry, snapshot Snapshot) []Failure {
	failures := make([]Failure, 0)
	add := func(code, message string) { failures = append(failures, Failure{Code: code, Message: message}) }
	if entry.VersionPolicy == capability.VersionPolicySupportedRange && !snapshot.SupportedVersion {
		add("unsupported_version", "")
	}
	if !snapshot.OpenAPIAvailable {
		add("openapi_unavailable", "")
	} else {
		for _, requirement := range entry.Endpoints {
			if !EndpointAvailable(snapshot.Document, requirement) {
				add("missing_endpoint:"+requirement.Method+" "+requirement.Path, fmt.Sprintf("InvokeAI does not provide required endpoint %s %s", requirement.Method, requirement.Path))
			}
		}
		for _, requirement := range entry.Invocations {
			check := InspectInvocation(snapshot.Document, requirement)
			if check.Message != "" {
				add("incompatible_invocation:"+requirement.Type, check.Message)
			}
		}
	}
	if len(entry.Models) > 0 {
		if !snapshot.ModelsAvailable {
			add("models_unavailable", "")
		} else {
			for _, requirement := range entry.Models {
				if !InspectModel(snapshot.Models, requirement).Satisfied {
					add("missing_component:"+requirement.Name, "")
				}
			}
		}
	}
	if snapshot.OpenAPIAvailable {
		for _, predicate := range entry.Special {
			for _, code := range specialFailures(snapshot.Document, predicate) {
				add(code, "")
			}
		}
	}
	return failures
}

func specialFailures(document Document, predicate capability.SpecialPredicate) []string {
	switch predicate {
	case capability.RecallPatchSchema:
		if EndpointAvailable(document, capability.EndpointRequirement{Method: "POST", Path: capability.RecallEndpoint}) {
			return recallSchemaFailures(document)
		}
	case capability.InstallSchema:
		if EndpointAvailable(document, capability.EndpointRequirement{Method: "POST", Path: "/api/v2/models/install"}) {
			endpoint := installEndpoint(document.Paths["/api/v2/models/install"]["post"])
			var failures []string
			if !endpoint.HasRequiredSource() {
				failures = append(failures, "incompatible_install_schema:source")
			}
			if !endpoint.HasJobResponse() {
				failures = append(failures, "incompatible_install_schema:job_response")
			}
			return failures
		}
	case capability.StarterCatalogResponse:
		if EndpointAvailable(document, capability.EndpointRequirement{Method: "GET", Path: "/api/v2/models/starter_models"}) &&
			!installEndpoint(document.Paths["/api/v2/models/starter_models"]["get"]).HasStarterCatalogResponse() {
			return []string{"incompatible_starter_catalog_response"}
		}
	case capability.HuggingFaceLoginBody:
		if EndpointAvailable(document, capability.EndpointRequirement{Method: "POST", Path: capability.HuggingFaceAuthEndpoint}) {
			schema := document.Components.Schemas["Body_do_hf_login"]
			if !capability.HasHuggingFaceTokenBody(document.Paths[capability.HuggingFaceAuthEndpoint]["post"], schema.Properties["token"].Type, schema.Required) {
				return []string{"incompatible_hf_login_schema:token"}
			}
		}
	}
	return nil
}

func installEndpoint(raw json.RawMessage) capability.InstallEndpoint {
	var endpoint capability.InstallEndpoint
	if err := jsonv2.Unmarshal(raw, &endpoint); err != nil {
		return capability.InstallEndpoint{}
	}
	return endpoint
}

func recallSchemaFailures(document Document) []string {
	var body struct {
		RequestBody struct {
			Content map[string]struct {
				Schema struct {
					Ref string `json:"$ref"`
				} `json:"schema"`
			} `json:"content"`
		} `json:"requestBody"`
	}
	post := document.Paths[capability.RecallEndpoint]["post"]
	if err := jsonv2.Unmarshal(post, &body); err != nil || body.RequestBody.Content["application/json"].Schema.Ref != capability.RecallSchemaRef {
		return []string{"incompatible_recall_schema:request_body"}
	}
	properties := document.Components.Schemas["RecallParameter"].Properties
	var failures []string
	for _, field := range append(slices.Clone(capability.RecallPatchFields), capability.SDXLCFGRecallField) {
		property, ok := properties[field.Name]
		if !ok || !field.MatchesNullableAlternatives(property.AnyOf) {
			failures = append(failures, "incompatible_recall_schema:"+field.Name)
		}
	}
	return failures
}
