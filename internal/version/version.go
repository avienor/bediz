package version

import "runtime/debug"

// These values are replaced with -ldflags in release builds.
var (
	Version string
	Commit  string
	Date    string
)

const develModuleVersion = "(devel)"

type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	Date    string `json:"date,omitempty"`
}

// Current reports the version this binary was built as.
func Current() Info {
	build, _ := debug.ReadBuildInfo()
	return Resolve(Info{Version: Version, Commit: Commit, Date: Date}, build)
}

// Resolve selects the reported version. A release stamp injected with ldflags
// wins; otherwise the Go build information supplies the module version and VCS
// revision and time. Missing values leave the version as "dev" and omit the
// commit and date.
func Resolve(stamp Info, build *debug.BuildInfo) Info {
	if stamp.Version != "" {
		return stamp
	}
	info := Info{Version: "dev"}
	if build == nil {
		return info
	}
	if v := build.Main.Version; v != "" && v != develModuleVersion {
		info.Version = v
	}
	for _, setting := range build.Settings {
		switch setting.Key {
		case "vcs.revision":
			info.Commit = setting.Value
		case "vcs.time":
			info.Date = setting.Value
		}
	}
	return info
}
