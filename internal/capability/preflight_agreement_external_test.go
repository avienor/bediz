package capability_test

import (
	"bytes"
	"reflect"
	"slices"
	"testing"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/generation"
	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/sourceimage"
	"github.com/avienor/bediz/internal/upscale"
)

func TestEveryCommandPreflightRequirementBelongsToItsDoctorRows(t *testing.T) {
	check := func(name string, preflight []capability.Entry, rows []capability.Entry) {
		t.Helper()
		for _, entry := range preflight {
			if entry.VersionPolicy == capability.VersionPolicySupportedRange && !slices.ContainsFunc(rows, func(row capability.Entry) bool {
				return row.VersionPolicy == capability.VersionPolicySupportedRange
			}) {
				t.Errorf("%s: supported-version policy has no doctor row", name)
			}
			contains := func(requirement any, values func(capability.Entry) []any) bool {
				return slices.ContainsFunc(rows, func(row capability.Entry) bool {
					return slices.ContainsFunc(values(row), func(value any) bool { return reflect.DeepEqual(value, requirement) })
				})
			}
			for _, endpoint := range entry.Endpoints {
				if !contains(endpoint, func(row capability.Entry) []any {
					return boxed(row.Endpoints)
				}) {
					t.Errorf("%s: endpoint %#v has no doctor row", name, endpoint)
				}
			}
			for _, invocation := range entry.Invocations {
				if !contains(invocation, func(row capability.Entry) []any { return boxed(row.Invocations) }) {
					t.Errorf("%s: invocation %#v has no doctor row", name, invocation)
				}
			}
			for _, model := range entry.Models {
				if !contains(model, func(row capability.Entry) []any { return boxed(row.Models) }) {
					t.Errorf("%s: model %#v has no doctor row", name, model)
				}
			}
			for _, special := range entry.Special {
				if !contains(special, func(row capability.Entry) []any { return boxed(row.Special) }) {
					t.Errorf("%s: special predicate %s has no doctor row", name, special)
				}
			}
			for _, field := range entry.RecallFields {
				if !contains(field, func(row capability.Entry) []any { return boxed(row.RecallFields) }) {
					t.Errorf("%s: Recall field %s has no doctor row", name, field.Name)
				}
			}
		}
	}
	row := func(operation, family, mode string) capability.Entry {
		t.Helper()
		entry, found := capability.Find(operation, family, mode)
		if !found {
			t.Fatalf("missing doctor row %s/%s/%s", operation, family, mode)
		}
		return entry
	}

	for _, family := range []string{"anima", "sdxl", "flux"} {
		for _, mode := range []string{"txt2img", "img2img"} {
			resolved := generation.Resolution{Models: generation.ResolvedModels{Main: generation.ModelIdentifier{Base: family}}}
			if mode == "img2img" {
				resolved.Request.Source = &sourceimage.Source{}
			}
			entry, err := generation.CapabilityEntry(resolved)
			if err != nil {
				t.Fatal(err)
			}
			check("generate/"+family+"/"+mode, []capability.Entry{entry}, []capability.Entry{row(result.OperationGenerate, family, mode)})
		}
		additional := []capability.RecallFieldRequirement(nil)
		if family == "sdxl" {
			additional = []capability.RecallFieldRequirement{capability.SDXLCFGRecallField}
		}
		check("generate UI Synchronization/"+family, []capability.Entry{capability.RecallEntry(additional)}, []capability.Entry{row(result.OperationRecall, "", "")})
	}
	for _, family := range []string{"sdxl", "sd-1"} {
		request := upscale.Request{SchemaVersion: 1, Source: upscale.Source{Type: "image", Reference: "source.png"}, Model: "main", Seed: new(uint32(1)), Components: &upscale.Components{TileControlNet: new("tile")}}
		prepared, err := upscale.Prepare(request)
		if err != nil {
			t.Fatal(err)
		}
		inventory := []graphops.ModelIdentifier{
			{Key: "main", Hash: "main-hash", Name: "Main", Base: family, Type: "main", Variant: "normal"},
			{Key: "spandrel", Hash: "upscale-hash", Name: "Upscale", Base: "any", Type: "spandrel_image_to_image"},
			{Key: "tile", Hash: "tile-hash", Name: "Tile", Base: family, Type: "controlnet"},
		}
		entry, _, err := prepared.Resolve(inventory, bytes.NewReader(nil))
		if err != nil {
			t.Fatal(err)
		}
		check("upscale/"+family, []capability.Entry{entry}, []capability.Entry{row(result.OperationUpscale, family, "")})
	}
	check("upscale UI Synchronization", []capability.Entry{capability.RecallEntry(nil)}, []capability.Entry{row(result.OperationRecall, "", "")})
	check("standalone recall", []capability.Entry{capability.RecallEntry(nil)}, []capability.Entry{row(result.OperationRecall, "", "")})
	for _, test := range []struct {
		sourceType string
		token      bool
		families   []string
	}{
		{"url", false, []string{""}},
		{"civitai", true, []string{"", "source_token"}},
		{"starter", false, []string{"", "starter"}},
		{"starter", true, []string{"", "starter", "source_token"}},
		{"huggingface", false, []string{"", "huggingface"}},
		{"huggingface", true, []string{"", "huggingface", "source_token"}},
		{"path", false, []string{"", "path"}},
	} {
		rows := make([]capability.Entry, 0, len(test.families))
		for _, family := range test.families {
			rows = append(rows, row(result.OperationModelsInstall, family, ""))
		}
		preflight, found := capability.InstallPreflightEntries(test.sourceType, test.token)
		if !found {
			t.Fatalf("models install/%s preflight entries missing", test.sourceType)
		}
		families := make([]string, len(preflight))
		for i, entry := range preflight {
			families[i] = entry.Family
		}
		if !slices.Equal(families, test.families) {
			t.Errorf("models install/%s preflight rows = %q, want %q", test.sourceType, families, test.families)
		}
		check("models install/"+test.sourceType, preflight, rows)
	}
	check("auth huggingface login", []capability.Entry{{
		VersionPolicy: capability.VersionPolicySupportedRange,
		Endpoints:     []capability.EndpointRequirement{{Method: "POST", Path: capability.HuggingFaceAuthEndpoint}},
		Special:       []capability.SpecialPredicate{capability.HuggingFaceLoginBody},
	}}, []capability.Entry{row(result.OperationAuthHFLogin, "", "")})
}

func boxed[T any](values []T) []any {
	boxed := make([]any, len(values))
	for i, value := range values {
		boxed[i] = value
	}
	return boxed
}
