package version_test

import (
	"runtime/debug"
	"testing"

	"github.com/avienor/bediz/internal/version"
)

func TestResolvePrefersReleaseStamp(t *testing.T) {
	stamp := version.Info{Version: "v1.2.3", Commit: "0123456789abcdef0123456789abcdef01234567", Date: "2026-09-24T10:00:00Z"}
	build := &debug.BuildInfo{
		Main: debug.Module{Path: "github.com/avienor/bediz", Version: "v9.9.9"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "ffffffffffffffffffffffffffffffffffffffff"},
			{Key: "vcs.time", Value: "2020-01-01T00:00:00Z"},
		},
	}

	got := version.Resolve(stamp, build)

	if got != stamp {
		t.Fatalf("Resolve() = %+v, want release stamp %+v", got, stamp)
	}
}

func TestResolveReadsModuleVersionFromGoInstall(t *testing.T) {
	build := &debug.BuildInfo{
		Main: debug.Module{Path: "github.com/avienor/bediz", Version: "v1.0.0"},
	}

	got := version.Resolve(version.Info{}, build)

	if want := (version.Info{Version: "v1.0.0"}); got != want {
		t.Fatalf("Resolve() = %+v, want %+v", got, want)
	}
}

func TestResolveReadsVCSRevisionAndTime(t *testing.T) {
	build := &debug.BuildInfo{
		Main: debug.Module{Path: "github.com/avienor/bediz", Version: "v1.0.0-rc.1"},
		Settings: []debug.BuildSetting{
			{Key: "vcs", Value: "git"},
			{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"},
			{Key: "vcs.time", Value: "2026-09-24T10:00:00Z"},
			{Key: "vcs.modified", Value: "false"},
		},
	}

	got := version.Resolve(version.Info{}, build)

	want := version.Info{Version: "v1.0.0-rc.1", Commit: "0123456789abcdef0123456789abcdef01234567", Date: "2026-09-24T10:00:00Z"}
	if got != want {
		t.Fatalf("Resolve() = %+v, want %+v", got, want)
	}
}

func TestResolveTreatsDevelModuleVersionAsAbsent(t *testing.T) {
	build := &debug.BuildInfo{
		Main: debug.Module{Path: "github.com/avienor/bediz", Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"},
			{Key: "vcs.time", Value: "2026-09-24T10:00:00Z"},
		},
	}

	got := version.Resolve(version.Info{}, build)

	want := version.Info{Version: "dev", Commit: "0123456789abcdef0123456789abcdef01234567", Date: "2026-09-24T10:00:00Z"}
	if got != want {
		t.Fatalf("Resolve() = %+v, want %+v", got, want)
	}
}

func TestResolveWithoutSourcesReportsDevOnly(t *testing.T) {
	for name, build := range map[string]*debug.BuildInfo{
		"no build information": nil,
		"devel without VCS":    {Main: debug.Module{Path: "github.com/avienor/bediz", Version: "(devel)"}},
		"empty module version": {Main: debug.Module{Path: "github.com/avienor/bediz"}},
	} {
		t.Run(name, func(t *testing.T) {
			got := version.Resolve(version.Info{}, build)

			if want := (version.Info{Version: "dev"}); got != want {
				t.Fatalf("Resolve() = %+v, want %+v", got, want)
			}
		})
	}
}
