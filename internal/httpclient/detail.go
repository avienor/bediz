package httpclient

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"math"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxDetailBytes   = 4096
	maxDetailEntries = 8
	maxDetailPath    = 16
	maxDetailMessage = 512
	maxDetailField   = 128
)

// responseError keeps the conclusive status and extracts InvokeAI's optional
// detail before compactBody truncates the body used by Error.
func (c *Client) responseError(response *http.Response, body []byte) *HTTPError {
	err := &HTTPError{StatusCode: response.StatusCode, Status: response.Status, Body: compactBody(body)}
	if response.StatusCode >= 400 && response.StatusCode < 500 {
		var payload struct {
			Detail any `json:"detail"`
		}
		if json.Unmarshal(body, &payload) == nil {
			err.Detail = c.rejectionDetail(payload.Detail)
		}
	}
	return err
}

// rejectionDetail projects diagnostic fields only. FastAPI's input and ctx
// can contain the complete request or backend-only exception data.
func (c *Client) rejectionDetail(value any) any {
	if message, ok := value.(string); ok {
		if message = c.detailText(message, maxDetailMessage); message != "" {
			return message
		}
		return nil
	}
	entries, ok := value.([]any)
	if !ok {
		return nil
	}
	var detail []map[string]any
	for _, entry := range entries {
		fields, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		message, ok := fields["msg"].(string)
		if !ok {
			continue
		}
		message = c.detailText(message, maxDetailMessage)
		if message == "" {
			continue
		}
		projected := map[string]any{"msg": message}
		if kind, ok := fields["type"].(string); ok {
			projected["type"] = c.detailText(kind, maxDetailField)
		}
		if location, ok := fields["loc"].([]any); ok {
			location = location[:min(len(location), maxDetailPath)]
			path := make([]any, 0, len(location))
			valid := true
			for _, segment := range location {
				switch segment := segment.(type) {
				case string:
					path = append(path, c.detailText(segment, maxDetailField))
				case float64:
					if segment < 0 || segment > 1<<53-1 || math.Trunc(segment) != segment {
						valid = false
					} else {
						path = append(path, segment)
					}
				default:
					valid = false
				}
			}
			if valid {
				projected["loc"] = path
			}
		}
		candidate := append(detail, projected)
		// Account for the escaping used by the V1 Result Envelope writer.
		encoded, err := json.Marshal(candidate, jsontext.EscapeForJS(true))
		if err != nil || len(encoded) > maxDetailBytes {
			break
		}
		detail = candidate
		if len(detail) == maxDetailEntries {
			break
		}
	}
	if len(detail) == 0 {
		return nil
	}
	return detail
}

func (c *Client) detailText(value string, limit int) string {
	if c.token != "" {
		value = strings.ReplaceAll(value, c.token, "[REDACTED]")
	}
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return ' '
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > limit {
		end := limit - len("…")
		for !utf8.RuneStart(value[end]) {
			end--
		}
		value = value[:end] + "…"
	}
	return value
}
