package cli_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/result"
)

const downloadImageName = "0a1b2c3d-result.png"

var downloadPNG = func() []byte {
	content, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		panic(err)
	}
	return content
}()

type imageDownloadServer struct {
	*httptest.Server
	requests atomic.Int32
}

// newImageDownloadServer serves the 6.14.1 full-image route for
// downloadImageName, answered by serveImage. It counts every request and
// records any other request as an error.
func newImageDownloadServer(t *testing.T, serveImage http.HandlerFunc) *imageDownloadServer {
	t.Helper()
	server := &imageDownloadServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.requests.Add(1)
		if r.Method == http.MethodGet && r.URL.EscapedPath() == "/api/v1/images/i/"+downloadImageName+"/full" {
			serveImage(w, r)
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL)
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

// fullImage answers as InvokeAI 6.14.1 does for a stored image.
func fullImage(content []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Disposition", `inline; filename="`+downloadImageName+`"`)
		_, _ = w.Write(content)
	}
}

// directoryEntries names every entry in directory, so a test can prove that no
// temporary or partial file was left behind.
func directoryEntries(t *testing.T, directory string) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func errorDetails(t *testing.T, envelope map[string]any) (string, map[string]any) {
	t.Helper()
	failure, ok := envelope["error"].(map[string]any)
	if !ok {
		t.Fatalf("envelope has no error: %#v", envelope)
	}
	details, _ := failure["details"].(map[string]any)
	code, _ := failure["code"].(string)
	return code, details
}

func TestImagesDownloadWritesTheFullImageAndReportsIt(t *testing.T) {
	isolateUserConfigDir(t)
	server := newImageDownloadServer(t, fullImage(downloadPNG))
	directory := t.TempDir()
	output := filepath.Join(directory, "result.png")

	exitCode, envelope, stderr := runBoardsJSON(t, "", "images", "download", downloadImageName, "--output", output, "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	want := map[string]any{
		"image_name":   downloadImageName,
		"path":         output,
		"size_bytes":   float64(len(downloadPNG)),
		"content_type": "image/png",
	}
	if envelope["operation"] != "images.download" || !reflect.DeepEqual(envelope["data"], want) {
		t.Fatalf("envelope = %#v, want data %#v", envelope, want)
	}
	written, err := os.ReadFile(output)
	if err != nil || !bytes.Equal(written, downloadPNG) {
		t.Fatalf("written content = %v, %v; want the served bytes", written, err)
	}
	if entries := directoryEntries(t, directory); !reflect.DeepEqual(entries, []string{"result.png"}) {
		t.Fatalf("directory entries = %q, want only the target", entries)
	}
}

func TestImagesDownloadResolvesRelativeOutputAgainstWorkingDirectory(t *testing.T) {
	isolateUserConfigDir(t)
	server := newImageDownloadServer(t, fullImage(downloadPNG))
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	// The working directory may be reached through a symbolic link, as on macOS.
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	exitCode, envelope, stderr := runBoardsJSON(t, "", "images", "download", downloadImageName, "--output", filepath.Join("out", "result.png"), "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	wantPath := filepath.Join(workingDirectory, "out", "result.png")
	if got := envelope["data"].(map[string]any)["path"]; got != wantPath {
		t.Fatalf("path = %v, want absolute %q", got, wantPath)
	}
	if written, err := os.ReadFile(wantPath); err != nil || !bytes.Equal(written, downloadPNG) {
		t.Fatalf("written content = %v, %v; want the served bytes", written, err)
	}
}

func TestImagesDownloadAcceptsTypedRequestDocument(t *testing.T) {
	isolateUserConfigDir(t)
	server := newImageDownloadServer(t, fullImage(downloadPNG))
	output := filepath.Join(t.TempDir(), "result.png")
	document := `{"schema_version":1,"image_name":"` + downloadImageName + `","output":` + strconv.Quote(output) + `}`

	exitCode, envelope, stderr := runBoardsJSON(t, document, "images", "download", "--request", "-", "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	if got := envelope["data"].(map[string]any)["path"]; got != output {
		t.Fatalf("path = %v, want %q", got, output)
	}
	if written, err := os.ReadFile(output); err != nil || !bytes.Equal(written, downloadPNG) {
		t.Fatalf("written content = %v, %v; want the served bytes", written, err)
	}
}

func TestImagesDownloadRejectsInvalidRequestsBeforeNetwork(t *testing.T) {
	isolateUserConfigDir(t)
	directory := t.TempDir()
	parentFile := filepath.Join(directory, "file")
	if err := os.WriteFile(parentFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(directory, "result.png")
	tests := []struct {
		name  string
		stdin string
		args  []string
	}{
		{name: "missing output", args: []string{downloadImageName}},
		{name: "empty output", args: []string{downloadImageName, "--output", ""}},
		{name: "missing image name", args: []string{"--output", output}},
		{name: "empty image name", args: []string{"", "--output", output}},
		{name: "missing parent directory", args: []string{downloadImageName, "--output", filepath.Join(directory, "missing", "result.png")}},
		{name: "parent is a file", args: []string{downloadImageName, "--output", filepath.Join(parentFile, "result.png")}},
		{name: "image name combined with request", stdin: `{"schema_version":1}`, args: []string{downloadImageName, "--request", "-"}},
		{name: "output combined with request", stdin: `{"schema_version":1,"image_name":"` + downloadImageName + `"}`, args: []string{"--output", output, "--request", "-"}},
		{name: "document without output", stdin: `{"schema_version":1,"image_name":"` + downloadImageName + `"}`, args: []string{"--request", "-"}},
		{name: "document without image name", stdin: `{"schema_version":1,"output":` + strconv.Quote(output) + `}`, args: []string{"--request", "-"}},
		{name: "document with replace", stdin: `{"schema_version":1,"image_name":"` + downloadImageName + `","output":` + strconv.Quote(output) + `,"replace":true}`, args: []string{"--request", "-"}},
		{name: "document with unsupported schema version", stdin: `{"schema_version":2,"image_name":"` + downloadImageName + `","output":` + strconv.Quote(output) + `}`, args: []string{"--request", "-"}},
		{name: "replace flag", args: []string{downloadImageName, "--output", output, "--replace"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newImageDownloadServer(t, fullImage(downloadPNG))
			args := append([]string{"images", "download", "--url", server.URL}, test.args...)

			exitCode, envelope, stderr := runBoardsJSON(t, test.stdin, args...)

			if exitCode != result.ExitInvalidRequest || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			if code, _ := errorDetails(t, envelope); envelope["operation"] != "images.download" || code != "invalid_request" {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
			if requests := server.requests.Load(); requests != 0 {
				t.Fatalf("sent %d requests, want none", requests)
			}
			if entries := directoryEntries(t, directory); !reflect.DeepEqual(entries, []string{"file"}) {
				t.Fatalf("directory entries = %q, want nothing written", entries)
			}
		})
	}
}

func TestImagesDownloadNeverOverwritesAnExistingTarget(t *testing.T) {
	isolateUserConfigDir(t)
	directory := t.TempDir()
	existingFile := filepath.Join(directory, "existing.png")
	if err := os.WriteFile(existingFile, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	existingDirectory := filepath.Join(directory, "existing-directory")
	if err := os.Mkdir(existingDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	targets := []string{existingFile, existingDirectory}
	if runtime.GOOS != "windows" {
		danglingLink := filepath.Join(directory, "dangling.png")
		if err := os.Symlink(filepath.Join(directory, "absent.png"), danglingLink); err != nil {
			t.Fatal(err)
		}
		targets = append(targets, danglingLink)
	}
	before := directoryEntries(t, directory)
	for _, target := range targets {
		t.Run(filepath.Base(target), func(t *testing.T) {
			server := newImageDownloadServer(t, fullImage(downloadPNG))

			exitCode, envelope, stderr := runBoardsJSON(t, "", "images", "download", downloadImageName, "--output", target, "--url", server.URL)

			if exitCode != result.ExitInvalidRequest || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			code, details := errorDetails(t, envelope)
			if code != "invalid_request" || details["reason"] != "output_exists" || details["path"] != target {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
			if requests := server.requests.Load(); requests != 0 {
				t.Fatalf("sent %d requests, want none", requests)
			}
		})
	}
	if content, err := os.ReadFile(existingFile); err != nil || string(content) != "keep me" {
		t.Fatalf("existing file content = %q, %v; want it untouched", content, err)
	}
	if after := directoryEntries(t, directory); !reflect.DeepEqual(after, before) {
		t.Fatalf("directory entries = %q, want %q", after, before)
	}
}

func TestImagesDownloadLeavesATargetCreatedDuringTheDownloadUntouched(t *testing.T) {
	isolateUserConfigDir(t)
	directory := t.TempDir()
	output := filepath.Join(directory, "result.png")
	// The target appears after the pre-check has passed and before Bediz
	// publishes the downloaded content.
	server := newImageDownloadServer(t, func(w http.ResponseWriter, r *http.Request) {
		if err := os.WriteFile(output, []byte("written by another process"), 0o600); err != nil {
			t.Error(err)
		}
		fullImage(downloadPNG)(w, r)
	})

	exitCode, envelope, stderr := runBoardsJSON(t, "", "images", "download", downloadImageName, "--output", output, "--url", server.URL)

	if exitCode != result.ExitInvalidRequest || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	code, details := errorDetails(t, envelope)
	if code != "invalid_request" || details["reason"] != "output_exists" || details["path"] != output {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
	if content, err := os.ReadFile(output); err != nil || string(content) != "written by another process" {
		t.Fatalf("target content = %q, %v; want it untouched", content, err)
	}
	if entries := directoryEntries(t, directory); !reflect.DeepEqual(entries, []string{"result.png"}) {
		t.Fatalf("directory entries = %q, want no temporary file left", entries)
	}
}

func TestImagesDownloadFailuresLeaveNoFile(t *testing.T) {
	isolateUserConfigDir(t)
	oversized := int(httpclient.DefaultMaxBody) + 1
	tests := []struct {
		name        string
		serve       http.HandlerFunc
		wantCode    string
		wantExit    int
		wantDetails map[string]any
	}{
		{
			name:     "absent image",
			serve:    func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) },
			wantCode: "not_found",
			wantExit: result.ExitInvokeAIFailure,
		},
		{
			name: "declared size above limit",
			serve: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "image/png")
				w.Header().Set("Content-Length", strconv.Itoa(oversized))
				_, _ = w.Write(bytes.Repeat([]byte{0}, oversized))
			},
			wantCode:    "invalid_invokeai_response",
			wantExit:    result.ExitInvokeAIFailure,
			wantDetails: map[string]any{"reason": "response_too_large", "limit_bytes": float64(httpclient.DefaultMaxBody)},
		},
		{
			name: "streamed size above limit",
			serve: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "image/png")
				chunk := bytes.Repeat([]byte{0}, 1<<20)
				for written := 0; written < oversized; written += len(chunk) {
					_, _ = w.Write(chunk)
					w.(http.Flusher).Flush()
				}
			},
			wantCode:    "invalid_invokeai_response",
			wantExit:    result.ExitInvokeAIFailure,
			wantDetails: map[string]any{"reason": "response_too_large", "limit_bytes": float64(httpclient.DefaultMaxBody)},
		},
		{
			name: "truncated body",
			serve: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "image/png")
				w.Header().Set("Content-Length", strconv.Itoa(len(downloadPNG)+10))
				_, _ = w.Write(downloadPNG)
			},
			wantCode: "invalid_invokeai_response",
			wantExit: result.ExitInvokeAIFailure,
		},
		{
			name: "content that is not an image",
			serve: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write([]byte("<html>login</html>"))
			},
			wantCode: "invalid_invokeai_response",
			wantExit: result.ExitInvokeAIFailure,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			server := newImageDownloadServer(t, test.serve)

			exitCode, envelope, stderr := runBoardsJSON(t, "", "images", "download", downloadImageName, "--output", filepath.Join(directory, "result.png"), "--url", server.URL)

			code, details := errorDetails(t, envelope)
			if exitCode != test.wantExit || stderr != "" || code != test.wantCode {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			if test.wantDetails != nil && !reflect.DeepEqual(details, test.wantDetails) {
				t.Fatalf("details = %#v, want %#v", details, test.wantDetails)
			}
			if entries := directoryEntries(t, directory); len(entries) != 0 {
				t.Fatalf("directory entries = %q, want none", entries)
			}
		})
	}
}

func TestImagesDownloadReportsLocalWriteFailureWithoutPartialFile(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions do not deny writes on this platform or user")
	}
	isolateUserConfigDir(t)
	directory := t.TempDir()
	output := filepath.Join(directory, "result.png")
	// The directory becomes unwritable after the pre-check has passed.
	server := newImageDownloadServer(t, func(w http.ResponseWriter, r *http.Request) {
		if err := os.Chmod(directory, 0o555); err != nil {
			t.Error(err)
		}
		fullImage(downloadPNG)(w, r)
	})
	t.Cleanup(func() { _ = os.Chmod(directory, 0o755) })

	exitCode, envelope, stderr := runBoardsJSON(t, "", "images", "download", downloadImageName, "--output", output, "--url", server.URL)

	code, details := errorDetails(t, envelope)
	if exitCode != result.ExitInvokeAIFailure || stderr != "" || code != "output_write_failed" || details["path"] != output {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	if entries := directoryEntries(t, directory); len(entries) != 0 {
		t.Fatalf("directory entries = %q, want none", entries)
	}
}

func TestImagesDownloadRetriesTransientReadFailures(t *testing.T) {
	isolateUserConfigDir(t)
	var attempts atomic.Int32
	server := newImageDownloadServer(t, func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
			return
		}
		fullImage(downloadPNG)(w, r)
	})
	output := filepath.Join(t.TempDir(), "result.png")

	exitCode, envelope, stderr := runBoardsJSON(t, "", "images", "download", downloadImageName, "--output", output, "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("sent %d requests, want 2", got)
	}
	if written, err := os.ReadFile(output); err != nil || !bytes.Equal(written, downloadPNG) {
		t.Fatalf("written content = %v, %v; want the served bytes", written, err)
	}
}

func TestImagesDownloadHumanOutputNamesTheWrittenFile(t *testing.T) {
	isolateUserConfigDir(t)
	server := newImageDownloadServer(t, fullImage(downloadPNG))
	output := filepath.Join(t.TempDir(), "result.png")
	var stdout, stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(""), &stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"images", "download", downloadImageName, "--output", output, "--url", server.URL})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	want := output + "\t" + strconv.Itoa(len(downloadPNG)) + "\timage/png\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestImagesDownloadInterruptionLeavesNoFile(t *testing.T) {
	isolateUserConfigDir(t)
	directory := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// The interruption arrives after part of the body has been written.
	server := newImageDownloadServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Length", strconv.Itoa(len(downloadPNG)*2))
		_, _ = w.Write(downloadPNG)
		w.(http.Flusher).Flush()
		cancel()
		<-r.Context().Done()
	})
	var stdout, stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(""), &stdout, &stderr)

	exitCode := app.Run(ctx, []string{"images", "download", downloadImageName, "--output", filepath.Join(directory, "result.png"), "--url", server.URL, "--json"})

	if exitCode != result.ExitInterrupted || stderr.Len() != 0 || !strings.Contains(stdout.String(), `"code":"interrupted"`) {
		t.Fatalf("exit code = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if entries := directoryEntries(t, directory); len(entries) != 0 {
		t.Fatalf("directory entries = %q, want none", entries)
	}
}

func TestImagesDownloadWritesAFileOthersCanRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not apply on this platform")
	}
	isolateUserConfigDir(t)
	server := newImageDownloadServer(t, fullImage(downloadPNG))
	directory := t.TempDir()
	// A target name near the usual 255-byte file-name limit stays usable.
	output := filepath.Join(directory, strings.Repeat("n", 250)+".png")

	exitCode, envelope, stderr := runBoardsJSON(t, "", "images", "download", downloadImageName, "--output", output, "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	info, err := os.Stat(output)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("output mode = %v, %v; want 0644", info, err)
	}
}
