package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("test write failure")
}

func TestConfigSetJSONNeverPrintsToken(t *testing.T) {
	isolateUserConfigDir(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(context.Background(), []string{"config", "set", "--token", "top-secret", "--json"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	if strings.Contains(stdout.String(), "top-secret") {
		t.Fatalf("token leaked to stdout: %s", stdout.String())
	}
	var setEnvelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &setEnvelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v", err)
	}
	if !setEnvelope.OK || setEnvelope.Operation != "config.set" {
		t.Fatalf("unexpected set envelope: %#v", setEnvelope)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = app.Run(context.Background(), []string{"config", "get", "--json"})
	if exitCode != result.ExitSuccess {
		t.Fatalf("get exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	var getEnvelope struct {
		OK        bool   `json:"ok"`
		Operation string `json:"operation"`
		Data      struct {
			Stored struct {
				TokenConfigured bool `json:"token_configured"`
			} `json:"stored"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &getEnvelope); err != nil {
		t.Fatalf("config get stdout is not one JSON object: %v", err)
	}
	if !getEnvelope.OK || getEnvelope.Operation != "config.get" || !getEnvelope.Data.Stored.TokenConfigured {
		t.Fatalf("unexpected get envelope: %#v", getEnvelope)
	}
}

func TestConfigGetFailsWhenHumanOutputCannotBeWritten(t *testing.T) {
	isolateUserConfigDir(t)
	var stderr bytes.Buffer
	app := cli.New(errorWriter{}, &stderr)

	exitCode := app.Run(context.Background(), []string{"config", "get"})

	if exitCode != result.ExitInvokeAIFailure {
		t.Fatalf("exit code = %d, want %d", exitCode, result.ExitInvokeAIFailure)
	}
	if !strings.Contains(stderr.String(), "output_write_failed") {
		t.Fatalf("stderr = %q, want output_write_failed", stderr.String())
	}
}

func isolateUserConfigDir(t *testing.T) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "config")
	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", dir)
	case "darwin":
		t.Setenv("HOME", dir)
	default:
		t.Setenv("XDG_CONFIG_HOME", dir)
	}
}

func TestInvalidCommandUsesJSONEnvelope(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(context.Background(), []string{"unknown", "--json"})

	if exitCode != result.ExitInvalidRequest {
		t.Fatalf("exit code = %d, want %d", exitCode, result.ExitInvalidRequest)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v", err)
	}
	if envelope.OK || envelope.Operation != "cli" || envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestGlobalJSONFlagBeforeVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(context.Background(), []string{"--json", "version"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if !envelope.OK || envelope.Operation != "version" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestConfigHelpListsNestedCommands(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(context.Background(), []string{"config", "--help"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, command := range []string{"get", "set"} {
		if !bytes.Contains(stdout.Bytes(), []byte(command)) {
			t.Fatalf("help does not list %q command; stdout = %q", command, stdout.String())
		}
	}
}

func TestConfigGetHelpIsGeneratedByCommandTree(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(context.Background(), []string{"config", "get", "--help"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("--json")) {
		t.Fatalf("help does not list inherited --json flag; stdout = %q", stdout.String())
	}
}

func TestConfigSetHelpListsConfigurationFlags(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(context.Background(), []string{"config", "set", "--help"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, flag := range []string{"--url", "--token", "--unset-url", "--unset-token"} {
		if !bytes.Contains(stdout.Bytes(), []byte(flag)) {
			t.Fatalf("help does not list %q; stdout = %q", flag, stdout.String())
		}
	}
}

func TestDoctorHelpListsConnectionFlags(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(context.Background(), []string{"doctor", "--help"})

	if exitCode != result.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr = %q", exitCode, result.ExitSuccess, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, flag := range []string{"--url", "--token", "--timeout", "--json"} {
		if !bytes.Contains(stdout.Bytes(), []byte(flag)) {
			t.Fatalf("help does not list %q; stdout = %q", flag, stdout.String())
		}
	}
}

func TestDoctorHelpInJSONModeUsesFailureEnvelope(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "JSON flag after help flag", args: []string{"doctor", "--help", "--json"}},
		{name: "JSON flag before command", args: []string{"--json", "doctor", "--help"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app := cli.New(&stdout, &stderr)

			exitCode := app.Run(context.Background(), test.args)

			if exitCode != result.ExitInvalidRequest {
				t.Fatalf("exit code = %d, want %d", exitCode, result.ExitInvalidRequest)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
			var envelope result.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
			}
			if envelope.OK || envelope.Operation != "doctor" || envelope.Error == nil || envelope.Error.Code != "invalid_request" {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
		})
	}
}

func TestDoctorFlagErrorKeepsOperationInJSONEnvelope(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(context.Background(), []string{"doctor", "--unknown", "--json"})

	if exitCode != result.ExitInvalidRequest {
		t.Fatalf("exit code = %d, want %d", exitCode, result.ExitInvalidRequest)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	var envelope result.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q", err, stdout.String())
	}
	if envelope.OK || envelope.Operation != "doctor" || envelope.Error == nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}
