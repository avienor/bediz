// Package profileexecution applies the shared parts of Generation Profile
// loading and component preferences for graph-producing operations.
package profileexecution

import (
	"errors"
	"fmt"

	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/operation"
	"github.com/avienor/bediz/internal/profiles"
	"github.com/avienor/bediz/internal/result"
)

// Load performs common validation and loading while preserving each
// operation's distinct error type for Structured Error precedence.
func Load(name string, wrap func(string, error) error) (profiles.Document, error) {
	if !profiles.ValidName(name) {
		return profiles.Document{}, operation.InvalidRequest("invalid profile name")
	}
	profile, err := profiles.Get(name)
	if err != nil {
		return profiles.Document{}, wrap(name, err)
	}
	return profile, nil
}

type Preference struct {
	Kind        string
	Selector    *string
	Selected    **string
	Requirement graphops.ComponentRequirement
}

// ApplyPreferences fills absent explicit selectors from compatible profile
// preferences and returns one warning for each preference that was skipped.
func ApplyPreferences(name string, inventory []graphops.ModelIdentifier, continuation string, preferences []Preference) []result.Warning {
	var warnings []result.Warning
	for _, preference := range preferences {
		if preference.Selector == nil || *preference.Selected != nil {
			continue
		}
		component, err := graphops.ResolveUniqueCompatible(inventory, *preference.Selector, preference.Requirement)
		if err == nil {
			*preference.Selected = new(component.Key)
			continue
		}
		reason := "not_found"
		if _, ok := errors.AsType[*operation.SelectionRequiredError](err); ok {
			reason = "ambiguous"
		} else if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); ok {
			reason = "incompatible"
		}
		warnings = append(warnings, result.Warning{
			Code:    "profile_preference_skipped",
			Message: fmt.Sprintf("profile %q %s preference could not be used; %s", name, preference.Kind, continuation),
			Details: map[string]any{"profile": name, "component": preference.Kind, "reason": reason},
		})
	}
	return warnings
}
