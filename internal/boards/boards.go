package boards

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
)

type ListRequest struct {
	SchemaVersion   int  `json:"schema_version"`
	Offset          int  `json:"offset"`
	Limit           int  `json:"limit"`
	IncludeArchived bool `json:"include_archived,omitempty"`
}

// Summary is the normalized board summary. It never carries InvokeAI's owner,
// visibility, or other backend-only fields.
type Summary struct {
	BoardID        string  `json:"board_id"`
	BoardName      string  `json:"board_name"`
	ImageCount     int     `json:"image_count"`
	Archived       bool    `json:"archived"`
	CoverImageName *string `json:"cover_image_name,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

type ListResult struct {
	Offset int       `json:"offset"`
	Limit  int       `json:"limit"`
	Total  int       `json:"total"`
	Items  []Summary `json:"items"`
}

// GetRequest selects one board by exact identifier or unique exact name.
type GetRequest struct {
	SchemaVersion int    `json:"schema_version"`
	Board         string `json:"board"`
}

type GetResult struct {
	Board Summary `json:"board"`
}

type listResponse struct {
	Offset int           `json:"offset"`
	Limit  int           `json:"limit"`
	Total  int           `json:"total"`
	Items  []boardRecord `json:"items"`
}

type boardRecord struct {
	BoardID        string  `json:"board_id"`
	BoardName      string  `json:"board_name"`
	ImageCount     int     `json:"image_count"`
	Archived       bool    `json:"archived"`
	CoverImageName *string `json:"cover_image_name"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

// List returns one page of the boards visible to the caller in InvokeAI's
// newest-first order. Boards with equal creation times have no promised order.
func List(ctx context.Context, client *httpclient.Client, request ListRequest) (ListResult, error) {
	if request.SchemaVersion != 1 {
		return ListResult{}, operation.InvalidRequest(fmt.Sprintf("unsupported request schema version %d", request.SchemaVersion))
	}
	if request.Offset < 0 {
		return ListResult{}, operation.InvalidRequest("offset must be non-negative")
	}
	if request.Limit < 1 || request.Limit > 100 {
		return ListResult{}, operation.InvalidRequest("limit must be between 1 and 100")
	}

	query := url.Values{}
	query.Set("order_by", "created_at")
	query.Set("direction", "DESC")
	query.Set("offset", strconv.Itoa(request.Offset))
	query.Set("limit", strconv.Itoa(request.Limit))
	query.Set("include_archived", strconv.FormatBool(request.IncludeArchived))

	var response listResponse
	if err := client.GetJSON(ctx, "/api/v1/boards/?"+query.Encode(), &response); err != nil {
		return ListResult{}, err
	}
	result := ListResult{
		Offset: response.Offset,
		Limit:  response.Limit,
		Total:  response.Total,
		Items:  make([]Summary, 0, len(response.Items)),
	}
	for _, board := range response.Items {
		result.Items = append(result.Items, normalize(board))
	}
	return result, nil
}

// Get resolves the selector as an exact board identifier first. When no
// visible board has that identifier, it matches the exact, case-sensitive
// name across every visible board, archived ones included.
func Get(ctx context.Context, client *httpclient.Client, request GetRequest) (GetResult, error) {
	if request.SchemaVersion != 1 {
		return GetResult{}, operation.InvalidRequest(fmt.Sprintf("unsupported request schema version %d", request.SchemaVersion))
	}
	if request.Board == "" {
		return GetResult{}, operation.InvalidRequest("board selector is required")
	}

	var board boardRecord
	err := client.GetJSON(ctx, "/api/v1/boards/"+url.PathEscape(request.Board), &board)
	if err == nil {
		return GetResult{Board: normalize(board)}, nil
	}
	if !invisibleBoard(err) {
		return GetResult{}, err
	}

	query := url.Values{}
	query.Set("all", "true")
	query.Set("include_archived", "true")
	var visible []boardRecord
	if err := client.GetJSON(ctx, "/api/v1/boards/?"+query.Encode(), &visible); err != nil {
		return GetResult{}, err
	}
	var matches []boardRecord
	for _, candidate := range visible {
		if candidate.BoardName == request.Board {
			matches = append(matches, candidate)
		}
	}
	switch len(matches) {
	case 0:
		return GetResult{}, operation.NotFound(fmt.Sprintf("no visible board has the identifier or name %q", request.Board))
	case 1:
		return GetResult{Board: normalize(matches[0])}, nil
	}
	slices.SortFunc(matches, func(a, b boardRecord) int { return strings.Compare(a.BoardID, b.BoardID) })
	candidates := make([]operation.BoardCandidate, 0, len(matches))
	for _, match := range matches {
		candidates = append(candidates, operation.BoardCandidate{BoardID: match.BoardID, BoardName: match.BoardName})
	}
	return GetResult{}, &operation.BoardSelectionError{Selector: request.Board, Candidates: candidates}
}

// invisibleBoard reports a board detail response meaning no board with that
// identifier is visible: InvokeAI 6.14.1 answers 404 for an absent board and
// 403 for another user's private board.
func invisibleBoard(err error) bool {
	httpErr, ok := errors.AsType[*httpclient.HTTPError](err)
	return ok && (httpErr.StatusCode == http.StatusNotFound || httpErr.StatusCode == http.StatusForbidden)
}

func normalize(board boardRecord) Summary {
	return Summary{
		BoardID:        board.BoardID,
		BoardName:      board.BoardName,
		ImageCount:     board.ImageCount,
		Archived:       board.Archived,
		CoverImageName: board.CoverImageName,
		CreatedAt:      board.CreatedAt,
		UpdatedAt:      board.UpdatedAt,
	}
}
