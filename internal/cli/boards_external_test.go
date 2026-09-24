package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avienor/bediz/internal/cli"
	"github.com/avienor/bediz/internal/result"
)

// boardRecord is an InvokeAI 6.14.1 BoardDTO including the backend-only fields
// that Bediz must not expose.
func boardRecord(boardID, boardName string, archived bool) map[string]any {
	return map[string]any{
		"board_id": boardID, "board_name": boardName, "user_id": "user-1",
		"created_at": "2026-09-20 10:00:00.000", "updated_at": "2026-09-20 10:01:00.000", "deleted_at": nil,
		"cover_image_name": nil, "archived": archived, "board_visibility": "private", "cover_video_name": nil,
		"image_count": 3, "video_count": 0, "asset_count": 1, "owner_username": "owner",
	}
}

type boardsServer struct {
	*httptest.Server
	requests atomic.Int32
}

// newBoardsServer serves the 6.14.1 board detail route for the given boards and
// the unpaged all-boards listing used by name lookup.
func newBoardsServer(t *testing.T, boards []map[string]any, detailStatus map[string]int) *boardsServer {
	t.Helper()
	server := &boardsServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.requests.Add(1)
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method %s %s", r.Method, r.URL)
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path == "/api/v1/boards/" {
			query := r.URL.Query()
			if query.Get("all") != "true" || query.Get("include_archived") != "true" {
				t.Errorf("name lookup must list every visible board including archived ones: %v", query)
			}
			_ = json.NewEncoder(w).Encode(boards)
			return
		}
		boardID, ok := strings.CutPrefix(r.URL.Path, "/api/v1/boards/")
		if !ok {
			http.NotFound(w, r)
			return
		}
		if status, ok := detailStatus[boardID]; ok {
			http.Error(w, "detail", status)
			return
		}
		for _, board := range boards {
			if board["board_id"] == boardID {
				_ = json.NewEncoder(w).Encode(board)
				return
			}
		}
		http.Error(w, `{"detail":"Board not found"}`, http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	return server
}

func runBoardsJSON(t *testing.T, stdin string, args ...string) (int, map[string]any, string) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewWithIO(strings.NewReader(stdin), &stdout, &stderr)
	exitCode := app.Run(t.Context(), append(args, "--json"))
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v; stdout = %q, stderr = %q", err, stdout.String(), stderr.String())
	}
	return exitCode, envelope, stderr.String()
}

func TestBoardsListJSONReturnsAllowlistedSummariesInInvokeAIOrder(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if r.URL.Path != "/api/v1/boards/" || query.Get("offset") != "0" || query.Get("limit") != "20" ||
			query.Get("include_archived") != "false" || query.Get("order_by") != "created_at" || query.Get("direction") != "DESC" || query.Has("all") {
			t.Errorf("unexpected request path=%q query=%v", r.URL.Path, query)
		}
		newer := boardRecord("board-b", "Newer", false)
		newer["cover_image_name"] = "cover.png"
		_ = json.NewEncoder(w).Encode(map[string]any{
			"offset": 0, "limit": 20, "total": 7,
			"items": []map[string]any{newer, boardRecord("board-a", "Older", false)},
		})
	}))
	defer server.Close()

	exitCode, envelope, stderr := runBoardsJSON(t, "", "boards", "list", "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	want := map[string]any{
		"schema_version": float64(1), "ok": true, "operation": "boards.list", "warnings": []any{},
		"data": map[string]any{
			"offset": float64(0), "limit": float64(20), "total": float64(7),
			"items": []any{
				map[string]any{
					"board_id": "board-b", "board_name": "Newer", "image_count": float64(3), "archived": false,
					"cover_image_name": "cover.png", "created_at": "2026-09-20 10:00:00.000", "updated_at": "2026-09-20 10:01:00.000",
				},
				map[string]any{
					"board_id": "board-a", "board_name": "Older", "image_count": float64(3), "archived": false,
					"created_at": "2026-09-20 10:00:00.000", "updated_at": "2026-09-20 10:01:00.000",
				},
			},
		},
	}
	if !reflect.DeepEqual(envelope, want) {
		t.Fatalf("envelope = %#v\nwant %#v", envelope, want)
	}
}

func TestBoardsListFlagsCompileToTypedRequest(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("offset") != "40" || query.Get("limit") != "100" || query.Get("include_archived") != "true" {
			t.Errorf("unexpected query: %v", query)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"offset": 40, "limit": 100, "total": 0, "items": []any{}})
	}))
	defer server.Close()

	exitCode, envelope, stderr := runBoardsJSON(t, "", "boards", "list", "--offset", "40", "--limit", "100", "--include-archived", "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	if items := envelope["data"].(map[string]any)["items"]; !reflect.DeepEqual(items, []any{}) {
		t.Fatalf("items = %#v, want an empty array", items)
	}
}

func TestBoardsListAcceptsTypedRequestDocument(t *testing.T) {
	isolateUserConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("offset") != "2" || query.Get("limit") != "1" || query.Get("include_archived") != "true" {
			t.Errorf("unexpected query: %v", query)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"offset": 2, "limit": 1, "total": 0, "items": []any{}})
	}))
	defer server.Close()

	exitCode, envelope, stderr := runBoardsJSON(t, `{"schema_version":1,"offset":2,"limit":1,"include_archived":true}`,
		"boards", "list", "--request", "-", "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
}

func TestBoardsListRejectsInvalidRequestsBeforeNetwork(t *testing.T) {
	isolateUserConfigDir(t)
	tests := []struct {
		name  string
		stdin string
		args  []string
	}{
		{name: "limit zero", args: []string{"--limit", "0"}},
		{name: "limit above one hundred", args: []string{"--limit", "101"}},
		{name: "negative offset", args: []string{"--offset", "-1"}},
		{name: "document limit above one hundred", stdin: `{"schema_version":1,"offset":0,"limit":101}`, args: []string{"--request", "-"}},
		{name: "unknown document field", stdin: `{"schema_version":1,"offset":0,"limit":20,"all":true}`, args: []string{"--request", "-"}},
		{name: "unsupported schema version", stdin: `{"schema_version":2,"offset":0,"limit":20}`, args: []string{"--request", "-"}},
		{name: "flags mixed with document", stdin: `{"schema_version":1,"offset":0,"limit":20}`, args: []string{"--request", "-", "--limit", "5"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newBoardsServer(t, nil, nil)
			args := append([]string{"boards", "list", "--url", server.URL}, test.args...)

			exitCode, envelope, stderr := runBoardsJSON(t, test.stdin, args...)

			if exitCode != result.ExitInvalidRequest || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			if envelope["operation"] != "boards.list" || envelope["error"].(map[string]any)["code"] != "invalid_request" {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
			if requests := server.requests.Load(); requests != 0 {
				t.Fatalf("sent %d requests, want none", requests)
			}
		})
	}
}

func TestBoardsGetReturnsBoardByExactIdentifier(t *testing.T) {
	isolateUserConfigDir(t)
	// Another board is named after the requested identifier; the exact
	// identifier wins without a name search.
	server := newBoardsServer(t, []map[string]any{
		boardRecord("board-1", "Portraits", false),
		boardRecord("board-2", "board-1", false),
	}, nil)

	exitCode, envelope, stderr := runBoardsJSON(t, "", "boards", "get", "board-1", "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	want := map[string]any{"board": map[string]any{
		"board_id": "board-1", "board_name": "Portraits", "image_count": float64(3), "archived": false,
		"created_at": "2026-09-20 10:00:00.000", "updated_at": "2026-09-20 10:01:00.000",
	}}
	if envelope["operation"] != "boards.get" || !reflect.DeepEqual(envelope["data"], want) {
		t.Fatalf("envelope = %#v", envelope)
	}
	if requests := server.requests.Load(); requests != 1 {
		t.Fatalf("sent %d requests, want only the detail request", requests)
	}
}

func TestBoardsGetResolvesUniqueExactNameIncludingArchivedBoards(t *testing.T) {
	isolateUserConfigDir(t)
	server := newBoardsServer(t, []map[string]any{
		boardRecord("board-1", "portraits", false),
		boardRecord("board-2", "Portraits", true),
		boardRecord("board-3", "Portraits 2", false),
	}, nil)

	exitCode, envelope, stderr := runBoardsJSON(t, `{"schema_version":1,"board":"Portraits"}`, "boards", "get", "--request", "-", "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	want := map[string]any{"board": map[string]any{
		"board_id": "board-2", "board_name": "Portraits", "image_count": float64(3), "archived": true,
		"created_at": "2026-09-20 10:00:00.000", "updated_at": "2026-09-20 10:01:00.000",
	}}
	if !reflect.DeepEqual(envelope["data"], want) {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestBoardsGetFallsBackToNameWhenIdentifierIsNotVisible(t *testing.T) {
	isolateUserConfigDir(t)
	// InvokeAI 6.14.1 answers 403 for another user's private board.
	server := newBoardsServer(t, []map[string]any{boardRecord("board-1", "hidden-id", false)}, map[string]int{"hidden-id": http.StatusForbidden})

	exitCode, envelope, stderr := runBoardsJSON(t, "", "boards", "get", "hidden-id", "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	if board := envelope["data"].(map[string]any)["board"].(map[string]any); board["board_id"] != "board-1" {
		t.Fatalf("board = %#v", board)
	}
}

func TestBoardsGetReturnsSortedCandidatesForAmbiguousName(t *testing.T) {
	isolateUserConfigDir(t)
	server := newBoardsServer(t, []map[string]any{
		boardRecord("board-c", "Shared", false),
		boardRecord("board-a", "Shared", true),
		boardRecord("board-b", "Other", false),
		boardRecord("board-b2", "Shared", false),
	}, nil)

	exitCode, envelope, stderr := runBoardsJSON(t, "", "boards", "get", "Shared", "--url", server.URL)

	if exitCode != result.ExitSelectionRequired || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	failure := envelope["error"].(map[string]any)
	wantDetails := map[string]any{
		"kind": "board", "selector": "Shared",
		"candidates": []any{
			map[string]any{"board_id": "board-a", "board_name": "Shared"},
			map[string]any{"board_id": "board-b2", "board_name": "Shared"},
			map[string]any{"board_id": "board-c", "board_name": "Shared"},
		},
	}
	if envelope["operation"] != "boards.get" || failure["code"] != "selection_required" || !reflect.DeepEqual(failure["details"], wantDetails) {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestBoardsGetReportsAbsentBoardAsNotFound(t *testing.T) {
	isolateUserConfigDir(t)
	server := newBoardsServer(t, []map[string]any{boardRecord("board-1", "Portraits", false)}, nil)

	exitCode, envelope, stderr := runBoardsJSON(t, "", "boards", "get", "PORTRAITS", "--url", server.URL)

	if exitCode != result.ExitInvokeAIFailure || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	if envelope["operation"] != "boards.get" || envelope["error"].(map[string]any)["code"] != "not_found" {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestBoardsGetRejectsMissingOrMixedSelectorsBeforeNetwork(t *testing.T) {
	isolateUserConfigDir(t)
	tests := []struct {
		name  string
		stdin string
		args  []string
	}{
		{name: "no selector"},
		{name: "empty selector", args: []string{""}},
		{name: "document without selector", stdin: `{"schema_version":1}`, args: []string{"--request", "-"}},
		{name: "document with unknown field", stdin: `{"schema_version":1,"board":"x","board_id":"x"}`, args: []string{"--request", "-"}},
		{name: "selector mixed with document", stdin: `{"schema_version":1,"board":"x"}`, args: []string{"x", "--request", "-"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newBoardsServer(t, nil, nil)
			args := append([]string{"boards", "get", "--url", server.URL}, test.args...)

			exitCode, envelope, stderr := runBoardsJSON(t, test.stdin, args...)

			if exitCode != result.ExitInvalidRequest || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			if envelope["operation"] != "boards.get" || envelope["error"].(map[string]any)["code"] != "invalid_request" {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
			if requests := server.requests.Load(); requests != 0 {
				t.Fatalf("sent %d requests, want none", requests)
			}
		})
	}
}

func TestBoardsHumanOutputListsIdentifiersAndNames(t *testing.T) {
	isolateUserConfigDir(t)
	server := newBoardsServer(t, []map[string]any{boardRecord("board-1", "Portraits", false)}, nil)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.New(&stdout, &stderr)

	exitCode := app.Run(t.Context(), []string{"boards", "get", "board-1", "--url", server.URL})

	if exitCode != result.ExitSuccess || stderr.Len() != 0 || stdout.String() != "board-1\tPortraits\t3\n" {
		t.Fatalf("exit code = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
}

type boardCreateServer struct {
	*httptest.Server
	requests atomic.Int32
	creates  atomic.Int32
}

// newBoardCreateServer serves the 6.14.1 version route, the unpaged all-boards
// listing used by the duplicate-name check, and the create route answered by
// create. It counts every request and every create request.
func newBoardCreateServer(t *testing.T, version string, boards []map[string]any, create http.HandlerFunc) *boardCreateServer {
	t.Helper()
	server := &boardCreateServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.requests.Add(1)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/app/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": version})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/boards/":
			query := r.URL.Query()
			if query.Get("all") != "true" || query.Get("include_archived") != "true" {
				t.Errorf("duplicate check must list every visible board including archived ones: %v", query)
			}
			_ = json.NewEncoder(w).Encode(boards)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/boards/":
			server.creates.Add(1)
			create(w, r)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// createdBoard answers a create request with the BoardDTO InvokeAI 6.14.1
// returns for the requested name.
func createdBoard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(boardRecord("board-new", r.URL.Query().Get("board_name"), false))
	}
}

func TestBoardsCreateReturnsCreatedBoardSummary(t *testing.T) {
	isolateUserConfigDir(t)
	server := newBoardCreateServer(t, "6.14.1", []map[string]any{boardRecord("board-1", "portraits", false)}, createdBoard())

	exitCode, envelope, stderr := runBoardsJSON(t, "", "boards", "create", " Portraits & Co ", "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	want := map[string]any{
		"schema_version": float64(1), "ok": true, "operation": "boards.create", "warnings": []any{},
		"data": map[string]any{
			"board": map[string]any{
				"board_id": "board-new", "board_name": " Portraits & Co ", "image_count": float64(3), "archived": false,
				"created_at": "2026-09-20 10:00:00.000", "updated_at": "2026-09-20 10:01:00.000",
			},
		},
	}
	if !reflect.DeepEqual(envelope, want) {
		t.Fatalf("envelope = %#v\nwant %#v", envelope, want)
	}
	if creates := server.creates.Load(); creates != 1 {
		t.Fatalf("sent %d create requests, want 1", creates)
	}
}

func TestBoardsCreateAcceptsTypedRequestDocument(t *testing.T) {
	isolateUserConfigDir(t)
	server := newBoardCreateServer(t, "6.14.1", nil, createdBoard())

	exitCode, envelope, stderr := runBoardsJSON(t, `{"schema_version":1,"board_name":"Landscapes"}`, "boards", "create", "--request", "-", "--url", server.URL)

	if exitCode != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
	}
	board := envelope["data"].(map[string]any)["board"].(map[string]any)
	if board["board_id"] != "board-new" || board["board_name"] != "Landscapes" || server.creates.Load() != 1 {
		t.Fatalf("envelope = %#v, creates = %d", envelope, server.creates.Load())
	}
}

func TestBoardsCreateRejectsInvalidRequestsBeforeNetwork(t *testing.T) {
	isolateUserConfigDir(t)
	tests := []struct {
		name  string
		stdin string
		args  []string
	}{
		{name: "no name"},
		{name: "empty name", args: []string{""}},
		{name: "whitespace-only name", args: []string{" \t\n "}},
		{name: "document without name", stdin: `{"schema_version":1}`, args: []string{"--request", "-"}},
		{name: "document with whitespace-only name", stdin: `{"schema_version":1,"board_name":"  "}`, args: []string{"--request", "-"}},
		{name: "document with unknown field", stdin: `{"schema_version":1,"board_name":"x","board_id":"x"}`, args: []string{"--request", "-"}},
		{name: "document with unsupported schema version", stdin: `{"schema_version":2,"board_name":"x"}`, args: []string{"--request", "-"}},
		{name: "name mixed with document", stdin: `{"schema_version":1,"board_name":"x"}`, args: []string{"x", "--request", "-"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newBoardCreateServer(t, "6.14.1", nil, createdBoard())
			args := append([]string{"boards", "create", "--url", server.URL}, test.args...)

			exitCode, envelope, stderr := runBoardsJSON(t, test.stdin, args...)

			if exitCode != result.ExitInvalidRequest || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			if envelope["operation"] != "boards.create" || envelope["error"].(map[string]any)["code"] != "invalid_request" {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
			if requests := server.requests.Load(); requests != 0 {
				t.Fatalf("sent %d requests, want none", requests)
			}
		})
	}
}

func TestBoardsCreateRejectsExistingExactNameWithoutMutation(t *testing.T) {
	isolateUserConfigDir(t)
	tests := []struct {
		name         string
		boards       []map[string]any
		wantBoardIDs []any
	}{
		{
			name:         "one board",
			boards:       []map[string]any{boardRecord("board-1", "Portraits", false), boardRecord("board-2", "portraits", false)},
			wantBoardIDs: []any{"board-1"},
		},
		{
			name:         "archived board",
			boards:       []map[string]any{boardRecord("board-9", "Portraits", true)},
			wantBoardIDs: []any{"board-9"},
		},
		{
			name: "several boards",
			boards: []map[string]any{
				boardRecord("board-c", "Portraits", false), boardRecord("board-x", "Other", false),
				boardRecord("board-a", "Portraits", true), boardRecord("board-b", "Portraits", false),
			},
			wantBoardIDs: []any{"board-a", "board-b", "board-c"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newBoardCreateServer(t, "6.14.1", test.boards, createdBoard())

			exitCode, envelope, stderr := runBoardsJSON(t, "", "boards", "create", "Portraits", "--url", server.URL)

			if exitCode != result.ExitInvalidRequest || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			failure := envelope["error"].(map[string]any)
			wantDetails := map[string]any{"reason": "board_name_exists", "board_ids": test.wantBoardIDs}
			if envelope["operation"] != "boards.create" || failure["code"] != "invalid_request" || !reflect.DeepEqual(failure["details"], wantDetails) {
				t.Fatalf("envelope = %#v, want details %#v", envelope, wantDetails)
			}
			if creates := server.creates.Load(); creates != 0 {
				t.Fatalf("sent %d create requests, want none", creates)
			}
		})
	}
}

func TestBoardsCreateSendsMutationOnceOnEveryFailure(t *testing.T) {
	isolateUserConfigDir(t)
	tests := []struct {
		name     string
		create   http.HandlerFunc
		wantCode string
	}{
		{
			name: "conclusive rejection",
			create: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"detail":"invalid"}`, http.StatusUnprocessableEntity)
			},
			wantCode: "invokeai_operation_failed",
		},
		{
			name: "transient gateway status",
			create: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
			},
			wantCode: "invokeai_operation_failed",
		},
		{
			name: "lost response",
			create: func(w http.ResponseWriter, _ *http.Request) {
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Errorf("hijack create connection: %v", err)
					return
				}
				_ = connection.Close()
			},
			wantCode: "outcome_unknown",
		},
		{
			name: "success without board identifier",
			create: func(w http.ResponseWriter, r *http.Request) {
				board := boardRecord("", r.URL.Query().Get("board_name"), false)
				delete(board, "board_id")
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(board)
			},
			wantCode: "outcome_unknown",
		},
		{
			name: "undecodable success",
			create: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"board_id":`))
			},
			wantCode: "outcome_unknown",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newBoardCreateServer(t, "6.14.1", nil, test.create)

			exitCode, envelope, stderr := runBoardsJSON(t, "", "boards", "create", "Portraits", "--url", server.URL)

			if exitCode != result.ExitInvokeAIFailure || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			if envelope["operation"] != "boards.create" || envelope["error"].(map[string]any)["code"] != test.wantCode {
				t.Fatalf("envelope = %#v, want code %q", envelope, test.wantCode)
			}
			if creates := server.creates.Load(); creates != 1 {
				t.Fatalf("sent %d create requests, want exactly 1", creates)
			}
		})
	}
}

func TestBoardsCreateRejectsUnsupportedInvokeAIVersionBeforeMutation(t *testing.T) {
	isolateUserConfigDir(t)
	for _, version := range []string{"6.14.0", "6.15.0"} {
		t.Run(version, func(t *testing.T) {
			server := newBoardCreateServer(t, version, nil, createdBoard())

			exitCode, envelope, stderr := runBoardsJSON(t, "", "boards", "create", "Portraits", "--url", server.URL)

			if exitCode != result.ExitUnsupportedCapability || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, envelope = %#v", exitCode, stderr, envelope)
			}
			if envelope["operation"] != "boards.create" || envelope["error"].(map[string]any)["code"] != "unsupported_capability" {
				t.Fatalf("envelope = %#v", envelope)
			}
			if creates := server.creates.Load(); creates != 0 {
				t.Fatalf("sent %d create requests, want none", creates)
			}
		})
	}
}
