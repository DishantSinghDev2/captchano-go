// Package captchano is the official Go SDK for the CaptchaNo captcha solving API:
// reCAPTCHA v2/v3/Enterprise, Cloudflare Turnstile, DataDome and image captchas.
//
//	client := captchano.New() // reads CAPTCHANO_API_KEY
//	task, err := client.RecaptchaV2(ctx, captchano.RecaptchaV2Params{
//		URL:     "https://example.com/login",
//		Sitekey: "6Lc...",
//	})
//	fmt.Println(task.Token())
//
// API reference: https://captchano.com/captcha-solver-api
package captchano

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Version is the SDK version, sent in the User-Agent.
const Version = "1.0.0"

// DefaultBaseURL is the production API.
const DefaultBaseURL = "https://api.captchano.com"

// MaxWait is the longest ?wait= the API honours, in seconds.
const MaxWait = 120 * time.Second

// ErrTimeout is returned by Solve and WaitFor when the task is still solving
// after the configured max wait. The returned *Task carries the id, so you can
// resume with WaitFor or GetTask (solutions are kept for ten minutes).
var ErrTimeout = errors.New("captchano: task still solving after max wait")

// Client talks to the CaptchaNo API. It is safe for concurrent use.
type Client struct {
	apiKey       string
	baseURL      string
	httpClient   *http.Client
	timeout      time.Duration
	wait         time.Duration
	pollInterval time.Duration
	maxWait      time.Duration
	maxRetries   int
}

// Option configures a Client.
type Option func(*Client)

// WithAPIKey sets the API key (cn-...). Default: $CAPTCHANO_API_KEY.
func WithAPIKey(key string) Option { return func(c *Client) { c.apiKey = strings.TrimSpace(key) } }

// WithBaseURL overrides the API origin (default https://api.captchano.com).
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }

// WithHTTPClient supplies your own *http.Client (transport, tracing, outbound proxy).
// Per-request deadlines are set through the context, so leave its Timeout at zero
// or above MaxWait.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

// WithTimeout sets the per-request timeout (default 30s). Blocking solve requests
// get this plus the ?wait= duration.
func WithTimeout(d time.Duration) Option { return func(c *Client) { c.timeout = d } }

// WithWait sets how long POST /v1/solve blocks server-side before answering
// (default 60s, capped at 120s). Zero disables blocking and Solve polls instead.
func WithWait(d time.Duration) Option { return func(c *Client) { c.wait = d } }

// WithPollInterval sets the GET /v1/task polling interval (default 5s).
func WithPollInterval(d time.Duration) Option { return func(c *Client) { c.pollInterval = d } }

// WithMaxWait caps the total time Solve and WaitFor spend on one task (default 180s).
func WithMaxWait(d time.Duration) Option { return func(c *Client) { c.maxWait = d } }

// WithMaxRetries sets how many times a 429 response is retried (default 2).
func WithMaxRetries(n int) Option { return func(c *Client) { c.maxRetries = n } }

// New creates a Client.
func New(opts ...Option) *Client {
	c := &Client{
		apiKey:       strings.TrimSpace(os.Getenv("CAPTCHANO_API_KEY")),
		baseURL:      DefaultBaseURL,
		httpClient:   &http.Client{},
		timeout:      30 * time.Second,
		wait:         60 * time.Second,
		pollInterval: 5 * time.Second,
		maxWait:      180 * time.Second,
		maxRetries:   2,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{}
	}
	if c.wait < 0 {
		c.wait = 0
	}
	if c.wait > MaxWait {
		c.wait = MaxWait
	}
	if c.pollInterval <= 0 {
		c.pollInterval = 5 * time.Second
	}
	return c
}

// Submit sends a solve request without blocking and returns the queued task.
// Use WaitFor or GetTask to collect the result.
func (c *Client) Submit(ctx context.Context, req *SolveRequest) (*Task, error) {
	return c.submit(ctx, req, 0)
}

// Solve submits a request and blocks until it is ready or failed, the context
// ends, or the max wait passes. A failed task is returned together with an
// *APIError describing it.
func (c *Client) Solve(ctx context.Context, req *SolveRequest) (*Task, error) {
	start := time.Now()
	wait := c.wait
	if c.maxWait > 0 && wait > c.maxWait {
		wait = c.maxWait
	}
	task, err := c.submit(ctx, req, wait)
	if err != nil || task.Done() {
		return task, err
	}
	remaining := time.Duration(0)
	if c.maxWait > 0 {
		remaining = c.maxWait - time.Since(start)
		if remaining <= 0 {
			return task, ErrTimeout
		}
	}
	return c.poll(ctx, task, remaining, true)
}

// GetTask fetches a task once. Solutions stay retrievable for ten minutes.
func (c *Client) GetTask(ctx context.Context, id string) (*Task, error) {
	if id == "" {
		return nil, errors.New("captchano: task id is empty")
	}
	status, body, hdr, err := c.do(ctx, http.MethodGet, "/v1/task/"+url.PathEscape(id), nil, 0)
	if err != nil {
		return nil, err
	}
	return taskResponse(status, body, hdr)
}

// WaitFor polls a task until it is ready or failed, the context ends, or the
// max wait passes. Transient errors while polling are retried.
func (c *Client) WaitFor(ctx context.Context, id string) (*Task, error) {
	return c.poll(ctx, &Task{ID: id, Status: StatusSolving}, c.maxWait, false)
}

// Balance returns the remaining prepaid credit.
func (c *Client) Balance(ctx context.Context) (*Balance, error) {
	status, body, hdr, err := c.do(ctx, http.MethodGet, "/v1/balance", nil, 0)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, errorResponse(status, body, hdr)
	}
	var raw struct {
		Balance  json.RawMessage `json:"balance"`
		Currency string          `json:"currency"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, &APIError{Code: "http_error", Message: "unexpected balance response", Status: status, Body: body}
	}
	b := &Balance{Balance: strings.Trim(string(raw.Balance), `"`), Currency: raw.Currency}
	return b, nil
}

func (c *Client) submit(ctx context.Context, req *SolveRequest, wait time.Duration) (*Task, error) {
	if req == nil {
		return nil, errors.New("captchano: nil SolveRequest")
	}
	path := "/v1/solve"
	secs := int(wait / time.Second)
	if secs > 0 {
		path += "?wait=" + strconv.Itoa(secs)
	}
	status, body, hdr, err := c.do(ctx, http.MethodPost, path, req, time.Duration(secs)*time.Second)
	if err != nil {
		return nil, err
	}
	return taskResponse(status, body, hdr)
}

func (c *Client) poll(ctx context.Context, task *Task, limit time.Duration, sleepFirst bool) (*Task, error) {
	pctx := ctx
	if limit > 0 {
		var cancel context.CancelFunc
		pctx, cancel = context.WithTimeout(ctx, limit)
		defer cancel()
	}
	for {
		if sleepFirst {
			t := time.NewTimer(c.pollInterval)
			select {
			case <-pctx.Done():
				t.Stop()
				return task, ctxErr(ctx)
			case <-t.C:
			}
		}
		sleepFirst = true
		next, err := c.GetTask(pctx, task.ID)
		if next != nil {
			task = next
		}
		if err != nil {
			if pctx.Err() != nil {
				return task, ctxErr(ctx)
			}
			if transient(err) {
				continue
			}
			return task, err
		}
		if task.Done() {
			return task, task.Err()
		}
	}
}

func ctxErr(parent context.Context) error {
	if err := parent.Err(); err != nil {
		return err
	}
	return ErrTimeout
}

func transient(err error) bool {
	var ce *ConnectionError
	if errors.As(err, &ce) {
		return true
	}
	var ae *APIError
	if errors.As(err, &ae) {
		switch ae.Code {
		case CodeRateLimited, CodeCapacityUnavailable:
			return true
		}
		return ae.Status >= 500
	}
	return false
}

// do performs one API call, retrying 429s. extra is added to the request timeout.
func (c *Client) do(ctx context.Context, method, path string, in any, extra time.Duration) (int, []byte, http.Header, error) {
	if c.apiKey == "" {
		return 0, nil, nil, &APIError{
			Code:    CodeInvalidAPIKey,
			Message: "no API key: set CAPTCHANO_API_KEY or pass captchano.WithAPIKey (create one at https://captchano.com/dashboard/keys)",
		}
	}
	var payload []byte
	if in != nil {
		var err error
		if payload, err = json.Marshal(in); err != nil {
			return 0, nil, nil, err
		}
	}
	for attempt := 0; ; attempt++ {
		status, body, hdr, err := c.once(ctx, method, path, payload, extra)
		if err != nil {
			return 0, nil, nil, err
		}
		if status == http.StatusTooManyRequests && attempt < c.maxRetries {
			d := retryAfter(hdr)
			if d <= 0 {
				d = time.Duration(500*(1<<attempt)) * time.Millisecond
			}
			t := time.NewTimer(d)
			select {
			case <-ctx.Done():
				t.Stop()
				return 0, nil, nil, ctx.Err()
			case <-t.C:
			}
			continue
		}
		return status, body, hdr, nil
	}
}

func (c *Client) once(ctx context.Context, method, path string, payload []byte, extra time.Duration) (int, []byte, http.Header, error) {
	rctx := ctx
	if c.timeout > 0 {
		var cancel context.CancelFunc
		rctx, cancel = context.WithTimeout(ctx, c.timeout+extra)
		defer cancel()
	}
	var rd io.Reader
	if payload != nil {
		rd = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(rctx, method, c.baseURL+path, rd)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "captchano-go/"+Version)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, nil, ctx.Err()
		}
		return 0, nil, nil, &ConnectionError{Err: err}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return 0, nil, nil, &ConnectionError{Err: err}
	}
	return resp.StatusCode, body, resp.Header, nil
}

func retryAfter(h http.Header) time.Duration {
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}
	if s, err := strconv.ParseFloat(v, 64); err == nil && s >= 0 {
		d := time.Duration(s * float64(time.Second))
		if d > 30*time.Second {
			d = 30 * time.Second
		}
		return d
	}
	return 0
}

// taskResponse decodes a response that should carry a task. 2xx, 402 and 422
// responses can carry a task body; a failed task is returned with its error.
func taskResponse(status int, body []byte, hdr http.Header) (*Task, error) {
	var t Task
	if json.Unmarshal(body, &t) == nil && t.ID != "" && t.Status != "" {
		if t.Status == StatusFailed {
			err := t.Err().(*APIError)
			err.Status = status
			err.Body = body
			return &t, err
		}
		if status >= 200 && status < 300 {
			return &t, nil
		}
	}
	if status >= 200 && status < 300 {
		return nil, &APIError{Code: "http_error", Message: "unexpected response body", Status: status, Body: body}
	}
	return nil, errorResponse(status, body, hdr)
}

func errorResponse(status int, body []byte, hdr http.Header) *APIError {
	e := &APIError{Status: status, Body: body}
	var env struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
		ID      string          `json:"id"`
	}
	if json.Unmarshal(body, &env) == nil {
		var obj TaskError
		var s string
		if json.Unmarshal(env.Error, &obj) == nil {
			e.Code, e.Message = obj.Code, obj.Message
		} else if json.Unmarshal(env.Error, &s) == nil {
			e.Code = s
		}
		if e.Message == "" {
			e.Message = env.Message
		}
		e.TaskID = env.ID
	}
	if e.Code == "" {
		e.Code = codeForStatus(status)
	}
	if e.Message == "" {
		e.Message = "CaptchaNo API returned HTTP " + strconv.Itoa(status)
	}
	if status == http.StatusTooManyRequests {
		e.RetryAfter = retryAfter(hdr)
	}
	return e
}
