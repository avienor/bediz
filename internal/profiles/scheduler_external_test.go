package profiles_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/profiles"
	"github.com/avienor/bediz/internal/result"
)

func TestProfileSchedulerSetMatchesRegisteredFamilies(t *testing.T) {
	// V1 §11.2's SDXL list was the save-time list for both profile sections.
	previous := []string{
		"ddim", "ddpm", "deis", "deis_k", "lms", "lms_k", "pndm", "heun", "heun_k", "euler", "euler_k", "euler_a",
		"kdpm_2", "kdpm_2_k", "kdpm_2_a", "kdpm_2_a_k", "dpmpp_2s", "dpmpp_2s_k", "dpmpp_2m", "dpmpp_2m_k",
		"dpmpp_2m_sde", "dpmpp_2m_sde_k", "dpmpp_3m", "dpmpp_3m_k", "dpmpp_sde", "dpmpp_sde_k", "er_sde",
		"unipc", "unipc_k", "lcm", "tcd",
	}
	slices.Sort(previous)
	registered := make(map[string]bool)
	for _, entry := range capability.Matrix {
		if entry.Operation != result.OperationGenerate && entry.Operation != result.OperationUpscale {
			continue
		}
		if len(entry.Schedulers) == 0 {
			t.Fatalf("%s/%s/%s has no tested schedulers", entry.Operation, entry.Family, entry.Mode)
		}
		for _, scheduler := range entry.Schedulers {
			registered[scheduler] = true
		}
	}
	if got := slices.Sorted(maps.Keys(registered)); !slices.Equal(got, previous) {
		t.Fatalf("registered scheduler union = %v, want previous save-time set %v", got, previous)
	}
	for _, section := range []string{"generate", "upscale"} {
		t.Run(section, func(t *testing.T) {
			for _, scheduler := range append(slices.Clone(previous), "", "unlisted", "Euler", " euler", "euler ") {
				doc := profiles.Document{SchemaVersion: 1, Name: "scheduler"}
				if section == "generate" {
					doc.Generate = &profiles.Generate{Scheduler: new(scheduler)}
				} else {
					doc.Upscale = &profiles.Upscale{Scheduler: new(scheduler)}
				}
				err := profiles.Validate(doc)
				if accepted := err == nil; accepted != registered[scheduler] {
					t.Errorf("save-time %s scheduler %q accepted = %t, registered = %t: %v", section, scheduler, accepted, registered[scheduler], err)
				}
			}
		})
	}
}
