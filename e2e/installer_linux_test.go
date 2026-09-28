package e2e_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallerLetsUserSelectSkillAgentsWithPipedScript(t *testing.T) {
	fixture := prepareInstaller(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "script", "-q", "-e", "-c", `cat "$BEDIZ_TEST_SCRIPT" | sh`, "/dev/null")
	command.Stdin = strings.NewReader("codex\n")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("install in a terminal: %v\n%s", err, output)
	}
	selected, err := os.ReadFile(fixture.selection)
	if err != nil {
		t.Fatalf("read installed skill selection: %v\n%s", err, output)
	}
	if want := "https://github.com/avienor/bediz/tree/v1.0.0/skills/bediz\ncodex\n"; string(selected) != want {
		t.Fatalf("installed skill selection = %q, want %q\n%s", selected, want, output)
	}
	fixture.assertBinaryInstalled(t)
}

func TestInstallerWithoutTerminalInstallsBinaryAndPrintsSkillCommand(t *testing.T) {
	fixture := prepareInstaller(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "sh", "-c", `cat "$BEDIZ_TEST_SCRIPT" | sh`)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("install without a terminal: %v\n%s", err, output)
	}
	if _, err := os.Stat(fixture.selection); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("skill selection should be left to the user; stat error=%v\n%s", err, output)
	}
	if !strings.Contains(string(output), "no interactive terminal") ||
		!strings.Contains(string(output), "npx skills add https://github.com/avienor/bediz/tree/v1.0.0/skills/bediz -g") {
		t.Fatalf("installer did not explain how to choose skill agents later:\n%s", output)
	}
	fixture.assertBinaryInstalled(t)
}

func TestInstallerSkillCancellationKeepsBinaryWithoutClaimingSkillInstalled(t *testing.T) {
	fixture := prepareInstaller(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "script", "-q", "-e", "-c", `cat "$BEDIZ_TEST_SCRIPT" | sh`, "/dev/null")
	command.Stdin = strings.NewReader("cancel\n")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("cancel skill installation: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "Installation cancelled") || strings.Contains(string(output), "Installed the agent skill") {
		t.Fatalf("installer should relay cancellation without claiming the skill was installed:\n%s", output)
	}
	if _, err := os.Stat(fixture.selection); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled skill should not be installed; stat error=%v", err)
	}
	fixture.assertBinaryInstalled(t)
}

type installerFixture struct {
	selection string
	binary    string
}

func prepareInstaller(t *testing.T) installerFixture {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	archiveName := "bediz_v1.0.0_linux_amd64.tar.gz"
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	binary := []byte("#!/bin/sh\nprintf '%s\\n' '{\"version\":\"v1.0.0\"}'\n")
	if err := tw.WriteHeader(&tar.Header{Name: "bediz", Mode: 0o755, Size: int64(len(binary))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	writeInstallerFixture(t, filepath.Join(dir, archiveName), archive.Bytes(), 0o644)
	checksum := fmt.Sprintf("%x  %s\n", sha256.Sum256(archive.Bytes()), archiveName)
	writeInstallerFixture(t, filepath.Join(dir, "SHA256SUMS"), []byte(checksum), 0o644)
	writeInstallerFixture(t, filepath.Join(bin, "curl"), []byte(`#!/bin/sh
set -eu
cp "$BEDIZ_TEST_RELEASE/${2##*/}" .
`), 0o755)
	writeInstallerFixture(t, filepath.Join(bin, "npx"), []byte(`#!/bin/sh
set -eu
[ "$1" = -y ]
shift
[ "$1" = skills ] && [ "$2" = add ]
source=$3
shift 3
automatic=false
for arg do
    case "$arg" in -y|--yes) automatic=true ;; esac
done
if "$automatic"; then
    agent=all
else
    [ -t 0 ] && [ -t 1 ]
    printf 'Which agents do you want to install to?\n'
    IFS= read -r agent
    if [ "$agent" = cancel ]; then
        printf 'Installation cancelled\n'
        exit 0
    fi
fi
printf '%s\n%s\n' "$source" "$agent" > "$BEDIZ_TEST_SELECTION"
`), 0o755)
	selection := filepath.Join(dir, "selected-skill")
	installDir := filepath.Join(dir, "installed")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BEDIZ_VERSION", "v1.0.0")
	t.Setenv("BEDIZ_INSTALL_DIR", installDir)
	t.Setenv("BEDIZ_TEST_SCRIPT", filepath.Join(root, "..", "install.sh"))
	t.Setenv("BEDIZ_TEST_RELEASE", dir)
	t.Setenv("BEDIZ_TEST_SELECTION", selection)
	return installerFixture{selection: selection, binary: filepath.Join(installDir, "bediz")}
}

func writeInstallerFixture(t *testing.T, path string, contents []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, contents, mode); err != nil {
		t.Fatal(err)
	}
}

func (fixture installerFixture) assertBinaryInstalled(t *testing.T) {
	t.Helper()
	output, err := exec.CommandContext(t.Context(), fixture.binary, "version", "--json").CombinedOutput()
	if err != nil || string(output) != "{\"version\":\"v1.0.0\"}\n" {
		t.Fatalf("installed binary version: error=%v output=%q", err, output)
	}
}
