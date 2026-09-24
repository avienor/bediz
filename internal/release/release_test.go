package release_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/avienor/bediz/internal/release"
)

const commitTime = "2026-09-24T10:11:13Z"

func TestBuildProducesArchivesAndChecksums(t *testing.T) {
	repo := newReleaseRepo(t, runtime.Version())
	tag(t, repo, "v1.0.0")
	out := filepath.Join(t.TempDir(), "out")

	if err := release.Build(t.Context(), release.Options{Root: repo, Version: "v1.0.0", OutDir: out}); err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	want := []string{
		"SHA256SUMS",
		"bediz_v1.0.0_darwin_arm64.tar.gz",
		"bediz_v1.0.0_linux_amd64.tar.gz",
		"bediz_v1.0.0_windows_amd64.zip",
	}
	if got := dirNames(t, out); !slices.Equal(got, want) {
		t.Fatalf("release directory = %v, want %v", got, want)
	}
	var wantSums strings.Builder
	for _, name := range want[1:] {
		sum := sha256.Sum256(readFile(t, filepath.Join(out, name)))
		fmt.Fprintf(&wantSums, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	if got := string(readFile(t, filepath.Join(out, "SHA256SUMS"))); got != wantSums.String() {
		t.Fatalf("SHA256SUMS = %q, want sha256sum -c format %q", got, wantSums.String())
	}
}

func TestBuildWritesFixedArchiveEntries(t *testing.T) {
	repo := newReleaseRepo(t, runtime.Version())
	tag(t, repo, "v1.0.0")
	out := filepath.Join(t.TempDir(), "out")
	if err := release.Build(t.Context(), release.Options{Root: repo, Version: "v1.0.0", OutDir: out}); err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	wantTime, _ := time.Parse(time.RFC3339, commitTime)

	for _, name := range []string{"bediz_v1.0.0_linux_amd64.tar.gz", "bediz_v1.0.0_darwin_arm64.tar.gz"} {
		t.Run(name, func(t *testing.T) {
			file, err := os.Open(filepath.Join(out, name))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			gz, err := gzip.NewReader(file)
			if err != nil {
				t.Fatal(err)
			}
			if gz.Name != "" || !gz.ModTime.IsZero() || gz.OS != 255 || gz.Comment != "" || gz.Extra != nil {
				t.Fatalf("gzip header = %+v, want no name, zero time, OS 255", gz.Header)
			}
			reader := tar.NewReader(gz)
			var names []string
			for {
				header, err := reader.Next()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				names = append(names, header.Name)
				wantMode := int64(0o644)
				if header.Name == "bediz" {
					wantMode = 0o755
				}
				if header.Typeflag != tar.TypeReg || header.Mode != wantMode || header.Uid != 0 || header.Gid != 0 ||
					header.Uname != "" || header.Gname != "" || !header.ModTime.Equal(wantTime) ||
					header.Format != tar.FormatUSTAR || len(header.PAXRecords) != 0 {
					t.Fatalf("tar header %q = %+v", header.Name, header)
				}
			}
			if want := []string{"bediz", "LICENSE", "README.md"}; !slices.Equal(names, want) {
				t.Fatalf("tar entries = %v, want %v", names, want)
			}
		})
	}

	t.Run("windows zip", func(t *testing.T) {
		// MS-DOS time has two-second precision; the extended-timestamp extra
		// field that could carry odd seconds is deliberately omitted.
		wantZipTime := wantTime.Add(-time.Second)
		archive, err := zip.OpenReader(filepath.Join(out, "bediz_v1.0.0_windows_amd64.zip"))
		if err != nil {
			t.Fatal(err)
		}
		defer archive.Close()
		var names []string
		for _, entry := range archive.File {
			names = append(names, entry.Name)
			wantMode := os.FileMode(0o644)
			if entry.Name == "bediz.exe" {
				wantMode = 0o755
			}
			if entry.Mode() != wantMode || len(entry.Extra) != 0 || entry.Comment != "" ||
				!entry.Modified.Equal(wantZipTime) || entry.Method != zip.Deflate {
				t.Fatalf("zip entry %q = %+v, mode %v", entry.Name, entry.FileHeader, entry.Mode())
			}
		}
		if want := []string{"bediz.exe", "LICENSE", "README.md"}; !slices.Equal(names, want) {
			t.Fatalf("zip entries = %v, want %v", names, want)
		}
	})
}

func TestBuildIsReproducibleAcrossCheckouts(t *testing.T) {
	repo := newReleaseRepo(t, runtime.Version())
	writeSkill(t, repo, "v1.0.0-rc.1")
	commit(t, repo, "declare prerelease")
	tag(t, repo, "v1.0.0-rc.1")
	clone := filepath.Join(t.TempDir(), "elsewhere", "clone")
	git(t, "", "clone", "--quiet", repo, clone)
	git(t, clone, "checkout", "--quiet", "v1.0.0-rc.1")

	if err := release.Check(t.Context(), repo, "v1.0.0-rc.1"); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	if err := release.Build(t.Context(), release.Options{Root: repo, Version: "v1.0.0-rc.1", OutDir: first}); err != nil {
		t.Fatalf("first Build() error = %v", err)
	}
	if err := release.Build(t.Context(), release.Options{Root: clone, Version: "v1.0.0-rc.1", OutDir: second}); err != nil {
		t.Fatalf("second Build() error = %v", err)
	}

	names := dirNames(t, first)
	if got := dirNames(t, second); !slices.Equal(got, names) {
		t.Fatalf("second release directory = %v, want %v", got, names)
	}
	for _, name := range names {
		a := readFile(t, filepath.Join(first, name))
		b := readFile(t, filepath.Join(second, name))
		if !bytes.Equal(a, b) {
			t.Errorf("%s differs between checkouts", name)
		}
	}
}

func TestBuildRefusesUnreleasableState(t *testing.T) {
	tests := []struct {
		name    string
		version string
		prepare func(t *testing.T, repo string)
		want    error
	}{
		{name: "missing v prefix", version: "1.0.0", prepare: func(t *testing.T, repo string) { tag(t, repo, "1.0.0") }, want: release.ErrInvalidVersion},
		{name: "partial version", version: "v1.0", prepare: func(t *testing.T, repo string) { tag(t, repo, "v1.0") }, want: release.ErrInvalidVersion},
		{name: "build metadata", version: "v1.0.0+meta", prepare: func(t *testing.T, repo string) { tag(t, repo, "v1.0.0+meta") }, want: release.ErrInvalidVersion},
		{name: "leading zero", version: "v1.02.0", prepare: func(t *testing.T, repo string) { tag(t, repo, "v1.02.0") }, want: release.ErrInvalidVersion},
		{name: "missing tag", version: "v1.0.0", prepare: func(*testing.T, string) {}, want: release.ErrTagNotOnCommit},
		{name: "tag points elsewhere", version: "v1.0.0", prepare: func(t *testing.T, repo string) {
			tag(t, repo, "v1.0.0")
			writeFile(t, repo, "README.md", "changed\n")
			commit(t, repo, "change readme")
		}, want: release.ErrTagNotOnCommit},
		{name: "modified file", version: "v1.0.0", prepare: func(t *testing.T, repo string) {
			tag(t, repo, "v1.0.0")
			writeFile(t, repo, "README.md", "changed\n")
		}, want: release.ErrDirtyTree},
		{name: "untracked file", version: "v1.0.0", prepare: func(t *testing.T, repo string) {
			tag(t, repo, "v1.0.0")
			writeFile(t, repo, "notes.txt", "scratch\n")
		}, want: release.ErrDirtyTree},
		{name: "skill declares another version", version: "v1.0.1", prepare: func(t *testing.T, repo string) { tag(t, repo, "v1.0.1") }, want: release.ErrSkillVersionMismatch},
		{name: "skill declares the release without its prerelease suffix", version: "v1.0.0-rc.1", prepare: func(t *testing.T, repo string) { tag(t, repo, "v1.0.0-rc.1") }, want: release.ErrSkillVersionMismatch},
		{name: "skill declares no version", version: "v1.0.0", prepare: func(t *testing.T, repo string) {
			writeFile(t, repo, "skills/bediz/SKILL.md", "---\nname: bediz\ndescription: Drive InvokeAI.\n---\n\n# Bediz\n")
			commit(t, repo, "drop skill version")
			tag(t, repo, "v1.0.0")
		}, want: release.ErrSkillVersionMismatch},
		{name: "skill version outside metadata", version: "v1.0.0", prepare: func(t *testing.T, repo string) {
			writeFile(t, repo, "skills/bediz/SKILL.md", "---\nname: bediz\nbediz-version: v1.0.0\n---\n\n# Bediz\n")
			commit(t, repo, "move skill version")
			tag(t, repo, "v1.0.0")
		}, want: release.ErrSkillVersionMismatch},
		{name: "missing skill", version: "v1.0.0", prepare: func(t *testing.T, repo string) {
			git(t, repo, "rm", "--quiet", "-r", "skills")
			commit(t, repo, "remove skill")
			tag(t, repo, "v1.0.0")
		}, want: release.ErrSkillVersionMismatch},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newReleaseRepo(t, runtime.Version())
			test.prepare(t, repo)
			out := filepath.Join(t.TempDir(), "out")

			if err := release.Check(t.Context(), repo, test.version); !errors.Is(err, test.want) {
				t.Fatalf("Check() error = %v, want %v", err, test.want)
			}
			err := release.Build(t.Context(), release.Options{Root: repo, Version: test.version, OutDir: out})

			if !errors.Is(err, test.want) {
				t.Fatalf("Build() error = %v, want %v", err, test.want)
			}
			if _, statErr := os.Stat(out); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("refused build created %s (stat error %v)", out, statErr)
			}
		})
	}
}

func TestBuildRefusesDifferentToolchain(t *testing.T) {
	repo := newReleaseRepo(t, "go1.99.0")
	tag(t, repo, "v1.0.0")

	if err := release.Check(t.Context(), repo, "v1.0.0"); !errors.Is(err, release.ErrToolchainMismatch) {
		t.Fatalf("Check() error = %v, want %v", err, release.ErrToolchainMismatch)
	}
	err := release.Build(t.Context(), release.Options{Root: repo, Version: "v1.0.0", OutDir: filepath.Join(t.TempDir(), "out")})

	if !errors.Is(err, release.ErrToolchainMismatch) {
		t.Fatalf("Build() error = %v, want %v", err, release.ErrToolchainMismatch)
	}
}

func TestBuildRefusesExistingOutputDirectory(t *testing.T) {
	repo := newReleaseRepo(t, runtime.Version())
	tag(t, repo, "v1.0.0")
	out := t.TempDir()

	err := release.Build(t.Context(), release.Options{Root: repo, Version: "v1.0.0", OutDir: out})

	if !errors.Is(err, release.ErrOutputExists) {
		t.Fatalf("Build() error = %v, want %v", err, release.ErrOutputExists)
	}
}

// newReleaseRepo creates a committed repository with the Bediz module path, the
// real version package, and a minimal command that prints the version result
// envelope. GOTOOLCHAIN=local keeps the go command from downloading toolchains.
func newReleaseRepo(t *testing.T, toolchain string) string {
	t.Helper()
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Release Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "release@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Release Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "release@example.invalid")
	t.Setenv("GIT_AUTHOR_DATE", commitTime)
	t.Setenv("GIT_COMMITTER_DATE", commitTime)

	repo := filepath.Join(t.TempDir(), "repo")
	versionSource := readFile(t, filepath.Join("..", "version", "version.go"))
	writeFile(t, repo, "go.mod", "module github.com/avienor/bediz\n\ngo 1.21\n\ntoolchain "+toolchain+"\n")
	writeFile(t, repo, "internal/version/version.go", string(versionSource))
	writeFile(t, repo, "cmd/bediz/main.go", `package main

import (
	"encoding/json"
	"os"

	"github.com/avienor/bediz/internal/version"
)

func main() {
	if len(os.Args) != 3 || os.Args[1] != "version" || os.Args[2] != "--json" {
		os.Exit(2)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"schema_version": 1, "ok": true, "operation": "version", "data": version.Current()})
}
`)
	writeFile(t, repo, "LICENSE", "license text\n")
	writeFile(t, repo, "README.md", "readme text\n")
	writeFile(t, repo, ".gitignore", "/dist/\n")
	writeSkill(t, repo, "v1.0.0")
	git(t, "", "init", "--quiet", "--initial-branch=master", repo)
	commit(t, repo, "initial")
	return repo
}

// writeSkill writes an agent skill whose metadata declares the given Bediz version.
func writeSkill(t *testing.T, repo, version string) {
	t.Helper()
	writeFile(t, repo, "skills/bediz/SKILL.md", "---\nname: bediz\ndescription: Drive InvokeAI.\nmetadata:\n  bediz-version: "+version+"\n---\n\n# Bediz\n")
}

func commit(t *testing.T, repo, message string) {
	t.Helper()
	git(t, repo, "add", "--all")
	git(t, repo, "commit", "--quiet", "--message", message)
}

func tag(t *testing.T, repo, name string) {
	t.Helper()
	git(t, repo, "tag", name)
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
