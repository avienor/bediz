package httpclient

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultTimeout   = 30 * time.Second
	DefaultRetries   = 2
	DefaultMaxBody   = int64(16 << 20)
	defaultRetryWait = 100 * time.Millisecond
)

type Client struct {
	baseURL   *url.URL
	token     string
	http      *http.Client
	retries   int
	maxBody   int64
	userAgent string
	retryWait func(context.Context, time.Duration) error
}

type Options struct {
	HTTPClient *http.Client
	Timeout    time.Duration
	Retries    int
	MaxBody    int64
	UserAgent  string
}

type HTTPError struct {
	StatusCode int
	Status     string
	Body       string
}

func (e *HTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("InvokeAI returned %s", e.Status)
	}
	return fmt.Sprintf("InvokeAI returned %s: %s", e.Status, e.Body)
}

func (e *HTTPError) AuthenticationFailure() bool {
	return e.StatusCode == http.StatusUnauthorized || e.StatusCode == http.StatusForbidden
}

type NetworkError struct {
	Method string
	URL    string
	Err    error
}

func (e *NetworkError) Error() string {
	return fmt.Sprintf("%s %s: %v", e.Method, e.URL, e.Err)
}

func (e *NetworkError) Unwrap() error { return e.Err }

type InvalidResponseError struct {
	URL string
	Err error
}

func (e *InvalidResponseError) Error() string {
	return fmt.Sprintf("invalid response from %s: %v", e.URL, e.Err)
}

func (e *InvalidResponseError) Unwrap() error { return e.Err }

// OutcomeUnknownError means a state-changing request may have reached InvokeAI,
// but Bediz did not receive a conclusive response. Callers must inspect remote
// state before deciding whether to submit the operation again. StatusCode is
// the gateway status that made the answer inconclusive, or zero when no status
// was received.
type OutcomeUnknownError struct {
	Method     string
	URL        string
	StatusCode int
	Err        error
}

func (e *OutcomeUnknownError) Error() string {
	return fmt.Sprintf("outcome unknown for %s %s: %v", e.Method, e.URL, e.Err)
}

func (e *OutcomeUnknownError) Unwrap() error { return e.Err }

func New(baseURL, token string, options Options) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base url: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New("base url must be an absolute http or https url")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("base url must not include credentials, a query, or a fragment")
	}

	timeout := cmp.Or(options.Timeout, DefaultTimeout)
	if timeout < 0 {
		return nil, errors.New("timeout must be positive")
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	retries := options.Retries
	if retries < 0 {
		return nil, errors.New("retries cannot be negative")
	}
	if retries == 0 && options.HTTPClient == nil {
		retries = DefaultRetries
	}
	maxBody := cmp.Or(options.MaxBody, DefaultMaxBody)
	if maxBody < 0 {
		return nil, errors.New("maximum response body size must be positive")
	}
	userAgent := cmp.Or(options.UserAgent, "bediz/dev")

	return &Client{
		baseURL:   parsed,
		token:     token,
		http:      httpClient,
		retries:   retries,
		maxBody:   maxBody,
		userAgent: userAgent,
		retryWait: wait,
	}, nil
}

func (c *Client) BaseURL() string { return c.baseURL.String() }

func (c *Client) HasToken() bool { return c.token != "" }

func (c *Client) AllowsSourceToken() bool {
	if c.baseURL.Scheme == "https" {
		return true
	}
	host := c.baseURL.Hostname()
	return strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}

func (c *Client) ResolveURL(path string) (string, error) { return c.resolve(path) }

func (c *Client) GetJSON(ctx context.Context, path string, target any) error {
	return c.doJSON(ctx, http.MethodGet, path, nil, target, true)
}

func (c *Client) DoJSON(ctx context.Context, method, path string, requestBody, target any) error {
	safeRead := method == http.MethodGet || method == http.MethodHead
	return c.doJSON(ctx, method, path, requestBody, target, safeRead)
}

// DoJSONPrivate sends a mutation without exposing its URL, backend response
// body, or transport error through any returned error. It refuses redirects so
// a credential cannot be forwarded and the mutation cannot be replayed.
func (c *Client) DoJSONPrivate(ctx context.Context, method, path string, requestBody, target any) error {
	privateClient := *c
	privateHTTP := *c.http
	privateHTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	privateClient.http = &privateHTTP
	err := privateClient.doJSON(ctx, method, path, requestBody, target, false)
	if err == nil {
		return nil
	}
	if httpErr, ok := errors.AsType[*HTTPError](err); ok {
		return &HTTPError{StatusCode: httpErr.StatusCode, Status: fmt.Sprintf("%d %s", httpErr.StatusCode, http.StatusText(httpErr.StatusCode))}
	}
	if unknown, ok := errors.AsType[*OutcomeUnknownError](err); ok {
		return &OutcomeUnknownError{Method: method, StatusCode: unknown.StatusCode, Err: errors.New("private request response was inconclusive")}
	}
	return &InvalidResponseError{Err: errors.New("private request could not be prepared")}
}

// QueryJSON performs a semantically read-only POST request. Unlike a mutation,
// a transient failure may be retried because repeating the query cannot change
// InvokeAI state.
func (c *Client) QueryJSON(ctx context.Context, path string, requestBody, target any) error {
	return c.doJSON(ctx, http.MethodPost, path, requestBody, target, true)
}

func (c *Client) PostStream(ctx context.Context, path string, body io.Reader, contentType string, target any) error {
	requestURL, err := c.resolve(path)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, body)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("User-Agent", c.userAgent)
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}

	response, err := c.http.Do(request)
	if err != nil {
		return &OutcomeUnknownError{Method: http.MethodPost, URL: requestURL, Err: err}
	}
	responseBody, readErr := readBody(response.Body, c.maxBody)
	_ = response.Body.Close()
	if readErr != nil {
		return &OutcomeUnknownError{Method: http.MethodPost, URL: requestURL, Err: readErr}
	}
	if gatewayStatus(response.StatusCode) {
		return gatewayOutcomeUnknown(http.MethodPost, requestURL, response)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &HTTPError{StatusCode: response.StatusCode, Status: response.Status, Body: compactBody(responseBody)}
	}
	if target == nil || len(responseBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(responseBody, target); err != nil {
		return &OutcomeUnknownError{Method: http.MethodPost, URL: requestURL, Err: fmt.Errorf("decode response: %w", err)}
	}
	return nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, requestBody, target any, safeRead bool) error {
	var body []byte
	var err error
	if requestBody != nil {
		body, err = json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
	}

	requestURL, err := c.resolve(path)
	if err != nil {
		return err
	}
	attempts := 1
	if safeRead {
		attempts += c.retries
	}
	for attempt := range attempts {
		response, doErr := c.do(ctx, method, requestURL, body)
		if doErr != nil {
			if attempt+1 < attempts && ctx.Err() == nil {
				if err := c.retryWait(ctx, defaultRetryWait*time.Duration(attempt+1)); err != nil {
					return err
				}
				continue
			}
			if !safeRead {
				return &OutcomeUnknownError{Method: method, URL: requestURL, Err: doErr}
			}
			return &NetworkError{Method: method, URL: requestURL, Err: doErr}
		}

		responseBody, readErr := readBody(response.Body, c.maxBody)
		response.Body.Close()
		if readErr != nil {
			if !safeRead {
				return &OutcomeUnknownError{Method: method, URL: requestURL, Err: readErr}
			}
			return &InvalidResponseError{URL: requestURL, Err: readErr}
		}
		if !safeRead && gatewayStatus(response.StatusCode) {
			return gatewayOutcomeUnknown(method, requestURL, response)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			httpErr := &HTTPError{
				StatusCode: response.StatusCode,
				Status:     response.Status,
				Body:       compactBody(responseBody),
			}
			if attempt+1 < attempts && gatewayStatus(response.StatusCode) {
				if err := c.retryWait(ctx, defaultRetryWait*time.Duration(attempt+1)); err != nil {
					return err
				}
				continue
			}
			return httpErr
		}
		if target == nil || len(responseBody) == 0 {
			return nil
		}
		decoder := json.NewDecoder(bytes.NewReader(responseBody))
		if err := decoder.Decode(target); err != nil {
			if !safeRead {
				return &OutcomeUnknownError{Method: method, URL: requestURL, Err: fmt.Errorf("decode response: %w", err)}
			}
			return &InvalidResponseError{URL: requestURL, Err: fmt.Errorf("decode response: %w", err)}
		}
		return nil
	}
	return errors.New("request attempts exhausted")
}

func (c *Client) do(ctx context.Context, method, requestURL string, body []byte) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", c.userAgent)
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	return c.http.Do(request)
}

func (c *Client) resolve(path string) (string, error) {
	if path == "" {
		return "", errors.New("request path is required")
	}
	parsed, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("parse request path: %w", err)
	}
	if parsed.IsAbs() || parsed.Host != "" {
		return "", errors.New("request path must be relative")
	}
	if parsed.Fragment != "" {
		return "", errors.New("request path must not include a fragment")
	}
	joined, err := url.JoinPath(c.baseURL.String(), strings.TrimPrefix(parsed.Path, "/"))
	if err != nil {
		return "", fmt.Errorf("join request path: %w", err)
	}
	joinedURL, err := url.Parse(joined)
	if err != nil {
		return "", fmt.Errorf("parse joined request url: %w", err)
	}
	joinedURL.RawQuery = parsed.RawQuery
	return joinedURL.String(), nil
}

func readBody(reader io.Reader, max int64) ([]byte, error) {
	limited := io.LimitReader(reader, max+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if int64(len(body)) > max {
		return nil, fmt.Errorf("response body exceeds %d bytes", max)
	}
	return body, nil
}

func compactBody(body []byte) string {
	const max = 2048
	value := strings.TrimSpace(string(body))
	if len(value) > max {
		return value[:max] + "…"
	}
	return value
}

// gatewayStatus reports a 502, 503, or 504 answer. InvokeAI 6.14.1 answers
// none of the routes Bediz uses with these statuses, so they come from an
// intermediary: a safe read may be retried, and a mutation may already have
// been forwarded, which makes its outcome unknown.
func gatewayStatus(status int) bool {
	return status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func gatewayOutcomeUnknown(method, requestURL string, response *http.Response) *OutcomeUnknownError {
	return &OutcomeUnknownError{
		Method: method, URL: requestURL, StatusCode: response.StatusCode,
		Err: fmt.Errorf("gateway answered %s", response.Status),
	}
}

func wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
