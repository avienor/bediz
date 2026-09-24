// Package release builds reproducible Bediz release archives from a tagged,
// clean checkout with the Go toolchain pinned in go.mod.
package release

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/avienor/bediz/internal/version"
)

var (
	ErrInvalidVersion    = errors.New("invalid release version")
	ErrTagNotOnCommit    = errors.New("release tag does not point to the checked-out commit")
	ErrDirtyTree         = errors.New("working tree is not clean")
	ErrToolchainMismatch = errors.New("active Go toolchain differs from the go.mod toolchain")
	ErrOutputExists      = errors.New("release directory already exists")
)

const versionPackage = "github.com/avienor/bediz/internal/version"

// versionPattern accepts vX.Y.Z and vX.Y.Z-<prerelease> with semantic-version
// identifiers and no build metadata.
var versionPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?$`)

type target struct {
	goos, goarch string
	// microarchitecture fixes GOAMD64 or GOARM64 so the environment cannot change code generation.
	archLevelKey, archLevel string
}

var targets = []target{
	{goos: "linux", goarch: "amd64", archLevelKey: "GOAMD64", archLevel: "v1"},
	{goos: "darwin", goarch: "arm64", archLevelKey: "GOARM64", archLevel: "v8.0"},
	{goos: "windows", goarch: "amd64", archLevelKey: "GOAMD64", archLevel: "v1"},
}

func (t target) binaryName() string {
	if t.goos == "windows" {
		return "bediz.exe"
	}
	return "bediz"
}

func (t target) archiveName(version string) string {
	if t.goos == "windows" {
		return fmt.Sprintf("bediz_%s_%s_%s.zip", version, t.goos, t.goarch)
	}
	return fmt.Sprintf("bediz_%s_%s_%s.tar.gz", version, t.goos, t.goarch)
}

type Options struct {
	// Root is the repository root holding go.mod.
	Root string
	// Version is the release version and the name of its tag.
	Version string
	// OutDir is the release directory. It must not exist yet.
	OutDir string
	// Log receives progress lines. Nil discards them.
	Log io.Writer
}

type source struct {
	root      string
	version   string
	commit    string
	time      time.Time
	date      string // time in RFC 3339, as injected and as recorded in vcs.time
	toolchain string
}

// Build verifies the checkout and writes the release archives and SHA256SUMS
// to OutDir. It creates OutDir only after every archive is built and the
// native binary reports the requested version and commit.
func Build(ctx context.Context, opts Options) error {
	log := opts.Log
	if log == nil {
		log = io.Discard
	}
	src, err := inspect(ctx, opts.Root, opts.Version)
	if err != nil {
		return err
	}
	out, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(out); err == nil {
		return fmt.Errorf("%w: %s", ErrOutputExists, out)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	work, err := os.MkdirTemp("", "bediz-release-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	// The archives are staged next to OutDir so the final rename stays on one
	// file system and never replaces a directory created in the meantime.
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(filepath.Dir(out), ".bediz-release-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := os.Chmod(staging, 0o755); err != nil {
		return err
	}
	docs := []archiveFile{
		{name: "LICENSE", path: filepath.Join(src.root, "LICENSE"), mode: 0o644},
		{name: "README.md", path: filepath.Join(src.root, "README.md"), mode: 0o644},
	}

	for _, t := range targets {
		fmt.Fprintf(log, "building %s/%s\n", t.goos, t.goarch)
		binary := filepath.Join(work, t.goos+"_"+t.goarch, t.binaryName())
		if err := buildBinary(ctx, src, t, binary); err != nil {
			return err
		}
		files := append([]archiveFile{{name: t.binaryName(), path: binary, mode: 0o755}}, docs...)
		archive := filepath.Join(staging, t.archiveName(src.version))
		if t.goos == "windows" {
			err = writeZip(archive, files, src.time)
		} else {
			err = writeTarGz(archive, files, src.time)
		}
		if err != nil {
			return fmt.Errorf("write %s: %w", filepath.Base(archive), err)
		}
	}
	if err := writeChecksums(staging); err != nil {
		return err
	}
	if err := verifyNative(ctx, src, staging, filepath.Join(work, "native")); err != nil {
		return err
	}

	if err := os.Rename(staging, out); err != nil {
		return err
	}
	fmt.Fprintf(log, "wrote %s\n", out)
	return nil
}

// inspect enforces the release preconditions in order: version name, clean
// tree, tag on the checked-out commit, and the pinned toolchain.
func inspect(ctx context.Context, root, version string) (source, error) {
	if !versionPattern.MatchString(version) {
		return source{}, fmt.Errorf("%w %q: want vX.Y.Z or vX.Y.Z-<prerelease>", ErrInvalidVersion, version)
	}
	status, err := run(ctx, root, nil, "git", "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return source{}, err
	}
	if status != "" {
		return source{}, fmt.Errorf("%w:\n%s", ErrDirtyTree, status)
	}
	head, err := run(ctx, root, nil, "git", "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return source{}, err
	}
	tagged, err := run(ctx, root, nil, "git", "rev-parse", "--verify", "--quiet", "refs/tags/"+version+"^{commit}")
	if err != nil {
		return source{}, fmt.Errorf("%w: tag %s not found: %w", ErrTagNotOnCommit, version, err)
	}
	if tagged != head {
		return source{}, fmt.Errorf("%w: tag %s is %s, HEAD is %s", ErrTagNotOnCommit, version, tagged, head)
	}
	seconds, err := run(ctx, root, nil, "git", "show", "--no-patch", "--format=%ct", head)
	if err != nil {
		return source{}, err
	}
	unix, err := strconv.ParseInt(seconds, 10, 64)
	if err != nil {
		return source{}, fmt.Errorf("read commit time %q: %w", seconds, err)
	}
	commitTime := time.Unix(unix, 0).UTC()
	pinned, err := pinnedToolchain(filepath.Join(root, "go.mod"))
	if err != nil {
		return source{}, err
	}
	active, err := run(ctx, root, nil, "go", "env", "GOVERSION")
	if err != nil {
		return source{}, err
	}
	if active != pinned {
		return source{}, fmt.Errorf("%w: active %s, go.mod toolchain %s", ErrToolchainMismatch, active, pinned)
	}
	return source{root: root, version: version, commit: head, time: commitTime, date: commitTime.Format(time.RFC3339), toolchain: pinned}, nil
}

func pinnedToolchain(goMod string) (string, error) {
	file, err := os.Open(goMod)
	if err != nil {
		return "", err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[0] == "toolchain" {
			return fields[1], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", errors.New("go.mod has no toolchain directive")
}

func buildBinary(ctx context.Context, src source, t target, output string) error {
	ldflags := fmt.Sprintf("-s -w -X %[1]s.Version=%[2]s -X %[1]s.Commit=%[3]s -X %[1]s.Date=%[4]s",
		versionPackage, src.version, src.commit, src.date)
	env := []string{
		"CGO_ENABLED=0",
		"GOOS=" + t.goos,
		"GOARCH=" + t.goarch,
		t.archLevelKey + "=" + t.archLevel,
		"GOFLAGS=-mod=readonly",
		"GOEXPERIMENT=",
	}
	if _, err := run(ctx, src.root, env, "go", "build", "-trimpath", "-buildvcs=true", "-ldflags="+ldflags, "-o", output, "./cmd/bediz"); err != nil {
		return err
	}
	return checkBuildInfo(output, src, t)
}

// repoDerivedSettings are build settings that come from the tagged source
// itself (go.mod and default.pgo), not from the build environment.
var repoDerivedSettings = []string{"DefaultGODEBUG", "-pgo"}

// checkBuildInfo rejects a binary whose recorded build settings differ from
// the release configuration, so environment settings cannot leak into it.
// -trimpath keeps -ldflags out of the build information; verifyNative checks
// the injected version values instead.
func checkBuildInfo(path string, src source, t target) error {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return err
	}
	want := map[string]string{
		"-buildmode":   "exe",
		"-compiler":    "gc",
		"-trimpath":    "true",
		"CGO_ENABLED":  "0",
		"GOOS":         t.goos,
		"GOARCH":       t.goarch,
		t.archLevelKey: t.archLevel,
		"vcs":          "git",
		"vcs.revision": src.commit,
		"vcs.time":     src.date,
		"vcs.modified": "false",
	}
	var problems []string
	if info.GoVersion != src.toolchain {
		problems = append(problems, fmt.Sprintf("go version %s, want %s", info.GoVersion, src.toolchain))
	}
	if info.Main.Version != src.version {
		problems = append(problems, fmt.Sprintf("module version %s, want %s", info.Main.Version, src.version))
	}
	seen := map[string]bool{}
	for _, setting := range info.Settings {
		seen[setting.Key] = true
		expected, required := want[setting.Key]
		switch {
		case required && setting.Value != expected:
			problems = append(problems, fmt.Sprintf("%s=%q, want %q", setting.Key, setting.Value, expected))
		case !required && !slices.Contains(repoDerivedSettings, setting.Key):
			problems = append(problems, fmt.Sprintf("unexpected %s=%q", setting.Key, setting.Value))
		}
	}
	for key := range want {
		if !seen[key] {
			problems = append(problems, fmt.Sprintf("missing %s", key))
		}
	}
	if len(problems) > 0 {
		slices.Sort(problems)
		return fmt.Errorf("%s/%s build information differs from the release configuration: %s", t.goos, t.goarch, strings.Join(problems, "; "))
	}
	return nil
}

type archiveFile struct {
	name string
	path string
	mode int64
}

func writeTarGz(path string, files []archiveFile, modTime time.Time) error {
	var buf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return err
	}
	// Name, Comment, Extra, and ModTime stay empty; OS is fixed to "unknown".
	gz.OS = 255
	tw := tar.NewWriter(gz)
	for _, file := range files {
		data, err := os.ReadFile(file.path)
		if err != nil {
			return err
		}
		header := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     file.name,
			Mode:     file.mode,
			Size:     int64(len(data)),
			ModTime:  modTime,
			Format:   tar.FormatUSTAR,
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func writeZip(path string, files []archiveFile, modTime time.Time) error {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	date, clock := msDosTime(modTime)
	for _, file := range files {
		data, err := os.ReadFile(file.path)
		if err != nil {
			return err
		}
		// Setting ModifiedDate and ModifiedTime directly, instead of Modified,
		// keeps archive/zip from adding an extended-timestamp extra field.
		header := &zip.FileHeader{
			Name:         file.name,
			Method:       zip.Deflate,
			ModifiedDate: date,
			ModifiedTime: clock,
		}
		header.SetMode(os.FileMode(file.mode))
		w, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// msDosTime encodes t, in UTC, as MS-DOS date and time with two-second precision.
func msDosTime(t time.Time) (date, clock uint16) {
	t = t.UTC()
	date = uint16(t.Day() + int(t.Month())<<5 + (t.Year()-1980)<<9)
	clock = uint16(t.Second()/2 + t.Minute()<<5 + t.Hour()<<11)
	return date, clock
}

func writeChecksums(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var sums strings.Builder
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(sum[:]), entry.Name())
	}
	return os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(sums.String()), 0o644)
}

// verifyNative extracts the host platform's archive and checks that its
// binary reports the release version, commit, and commit date.
func verifyNative(ctx context.Context, src source, staging, dir string) error {
	index := slices.IndexFunc(targets, func(t target) bool { return t.goos == runtime.GOOS && t.goarch == runtime.GOARCH })
	if index < 0 {
		return fmt.Errorf("no release target matches this host (%s/%s), so the built version cannot be verified", runtime.GOOS, runtime.GOARCH)
	}
	t := targets[index]
	binary := filepath.Join(dir, t.binaryName())
	if err := extractBinary(filepath.Join(staging, t.archiveName(src.version)), t, binary); err != nil {
		return err
	}
	output, err := run(ctx, dir, nil, binary, "version", "--json")
	if err != nil {
		return err
	}
	var envelope struct {
		Data version.Info `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		return fmt.Errorf("parse native version output %q: %w", output, err)
	}
	got := envelope.Data
	if want := (version.Info{Version: src.version, Commit: src.commit, Date: src.date}); got != want {
		return fmt.Errorf("native binary reports version %q, commit %q, date %q; want %q, %q, %q",
			got.Version, got.Commit, got.Date, src.version, src.commit, src.date)
	}
	return nil
}

func extractBinary(archive string, t target, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	var data []byte
	if t.goos == "windows" {
		zr, err := zip.OpenReader(archive)
		if err != nil {
			return err
		}
		defer zr.Close()
		rc, err := zr.Open(t.binaryName())
		if err != nil {
			return err
		}
		defer rc.Close()
		if data, err = io.ReadAll(rc); err != nil {
			return err
		}
	} else {
		file, err := os.Open(archive)
		if err != nil {
			return err
		}
		defer file.Close()
		gz, err := gzip.NewReader(file)
		if err != nil {
			return err
		}
		tr := tar.NewReader(gz)
		for {
			header, err := tr.Next()
			if err != nil {
				return fmt.Errorf("find %s in %s: %w", t.binaryName(), filepath.Base(archive), err)
			}
			if header.Name == t.binaryName() {
				if data, err = io.ReadAll(tr); err != nil {
					return err
				}
				break
			}
		}
	}
	return os.WriteFile(dest, data, 0o755)
}

func run(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
