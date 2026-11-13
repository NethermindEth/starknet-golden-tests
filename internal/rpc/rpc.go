// Package rpc sends JSON-RPC requests to a Starknet node.
package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	maxRetries = 3
	// maxErrorBody is the most bytes of an HTTP error body kept in an error;
	// the body can echo the request, so it stays short.
	maxErrorBody = 200
)

// Response is a JSON-RPC response without the jsonrpc and id members.
type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

// Client posts JSON-RPC bodies to one URL.
type Client struct {
	// RetryBackoff is the wait before the first retry; it doubles each time.
	RetryBackoff time.Duration

	url  string
	http *http.Client
}

// New returns a client that keeps up to maxConns idle connections.
func New(url string, maxConns int) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = max(transport.MaxIdleConns, maxConns)
	transport.MaxIdleConnsPerHost = maxConns
	return &Client{
		RetryBackoff: 500 * time.Millisecond,
		url:          url,
		http:         &http.Client{Timeout: 2 * time.Minute, Transport: transport},
	}
}

// Post sends body and returns the response body. It retries transport errors,
// HTTP 5xx and HTTP 429 (after Retry-After, if given). A JSON-RPC error is a
// result, so it is not retried.
func (c *Client) Post(ctx context.Context, body []byte) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		resp, err := c.post(ctx, body)
		if err == nil {
			return resp, nil
		}
		wait := c.RetryBackoff << attempt
		var status statusError
		if errors.As(err, &status) {
			if status.code != http.StatusTooManyRequests && status.code < http.StatusInternalServerError {
				return nil, err
			}
			wait = max(wait, status.retryAfter)
		}
		if errors.Is(err, errInvalidURL) || attempt == maxRetries || ctx.Err() != nil {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

// errInvalidURL means the node URL cannot work, so a retry cannot help.
var errInvalidURL = errors.New("node URL is not valid")

func (c *Client) post(ctx context.Context, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
		return nil, fmt.Errorf("%w: %s", errInvalidURL, Redact(c.url))
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		// The URL can hold an API key; keep it out of reports.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			urlErr.URL = Redact(urlErr.URL)
		}
		return nil, err
	}
	data, err := io.ReadAll(resp.Body)
	if closeErr := resp.Body.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, newStatusError(resp, data)
	}
	return data, nil
}

// Call sends one request and decodes its result into out.
func (c *Client) Call(ctx context.Context, method string, params, out any) error {
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return err
	}
	data, err := c.Post(ctx, body)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("%s: decode response: %w", method, err)
	}
	if resp.Error != nil {
		return fmt.Errorf("%s: %s", method, resp.Error)
	}
	if err := json.Unmarshal(resp.Result, out); err != nil {
		return fmt.Errorf("%s: decode result: %w", method, err)
	}
	return nil
}

// statusError is an HTTP response with a status other than 200 OK.
type statusError struct {
	code       int
	body       string
	retryAfter time.Duration
}

func newStatusError(resp *http.Response, body []byte) statusError {
	body = bytes.TrimSpace(body)
	if len(body) > maxErrorBody {
		body = append(body[:maxErrorBody:maxErrorBody], "..."...)
	}
	e := statusError{code: resp.StatusCode, body: string(body)}
	if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds > 0 {
		e.retryAfter = time.Duration(seconds) * time.Second
	}
	return e
}

func (e statusError) Error() string {
	return fmt.Sprintf("unexpected HTTP status %d %s: %s", e.code, http.StatusText(e.code), e.body)
}

// Split decodes the response to an entry by request id. The response to a
// single request belongs to singleID whatever its id member says, so an error
// with "id": null is kept.
func Split(data []byte, batch bool, singleID int) (map[int]Response, error) {
	type item struct {
		ID int `json:"id"`
		Response
	}
	var items []item
	if batch {
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, fmt.Errorf("decode batch response: %w", err)
		}
	} else {
		var single Response
		if err := json.Unmarshal(data, &single); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		return map[int]Response{singleID: single}, nil
	}
	byID := make(map[int]Response, len(items))
	for _, it := range items {
		byID[it.ID] = it.Response
	}
	return byID, nil
}

// Redact returns only the scheme and host of a URL. The user info, path and
// query can hold an API key.
func Redact(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "<node URL>"
	}
	return u.Scheme + "://" + u.Host
}
