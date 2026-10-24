package captchano

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testKey = "cn-test-key"

type recorded struct {
	Method string
	Path   string
	Query  string
	Auth   string
	UA     string
	Body   map[string]any
}

type fake struct {
	mu   sync.Mutex
	reqs []recorded
	srv  *httptest.Server
}

func newFake(t *testing.T, h func(w http.ResponseWriter, r *http.Request, n int)) (*fake, *Client) {
	t.Helper()
	f := &fake{}
	var n int32
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := recorded{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Auth: r.Header.Get("Authorization"), UA: r.Header.Get("User-Agent")}
		if b, _ := io.ReadAll(r.Body); len(b) > 0 {
			_ = json.Unmarshal(b, &rec.Body)
		}
		f.mu.Lock()
		f.reqs = append(f.reqs, rec)
		f.mu.Unlock()
		h(w, r, int(atomic.AddInt32(&n, 1)))
	}))
	t.Cleanup(f.srv.Close)
	t.Setenv("CAPTCHANO_API_KEY", "")
	c := New(WithAPIKey(testKey), WithBaseURL(f.srv.URL), WithPollInterval(5*time.Millisecond), WithTimeout(5*time.Second))
	return f, c
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func readyTask(id string, typ ChallengeType, sol map[string]any) map[string]any {
	return map[string]any{"id": id, "status": "ready", "type": typ, "solution": sol, "cost": "0.0010", "billedFrom": "balance", "solveTime": 9.5, "createdAt": 1767225600000}
}

func TestSolveReadyInOneCall(t *testing.T) {
	f, c := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 200, readyTask("slv_1", TypeRecaptchaV2, map[string]any{"token": "tok-123", "userAgent": "UA/1"}))
	})
	task, err := c.RecaptchaV2(context.Background(), RecaptchaV2Params{URL: "https://example.com/login", Sitekey: "6Lsitekey", Proxy: "http://u:p@h:1"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Token() != "tok-123" || task.Solution.UserAgent != "UA/1" || task.Cost != "0.0010" || float64(task.SolveTime) != 9.5 {
		t.Fatalf("unexpected task %+v", task)
	}
	r := f.reqs[0]
	if r.Method != "POST" || r.Path != "/v1/solve" || r.Query != "wait=60" {
		t.Fatalf("unexpected request %+v", r)
	}
	if r.Auth != "Bearer "+testKey || r.UA != "captchano-go/"+Version {
		t.Fatalf("headers: %q %q", r.Auth, r.UA)
	}
	if r.Body["type"] != "recaptcha_v2" || r.Body["sitekey"] != "6Lsitekey" || r.Body["proxy"] != "http://u:p@h:1" {
		t.Fatalf("body %+v", r.Body)
	}
	if _, ok := r.Body["minScore"]; ok {
		t.Fatal("empty fields must be omitted")
	}
}

func TestSolvePollsAfter202(t *testing.T) {
	f, c := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		switch n {
		case 1:
			writeJSON(w, 202, map[string]any{"id": "slv_2", "status": "solving", "type": "turnstile", "createdAt": 1})
		case 2:
			writeJSON(w, 200, map[string]any{"id": "slv_2", "status": "solving", "type": "turnstile", "createdAt": 1})
		case 3:
			writeJSON(w, 503, map[string]any{"error": map[string]any{"code": "capacity_unavailable", "message": "busy"}})
		default:
			writeJSON(w, 200, readyTask("slv_2", TypeTurnstile, map[string]any{"token": "ts-tok"}))
		}
	})
	task, err := c.Turnstile(context.Background(), TurnstileParams{URL: "https://example.com", Sitekey: "0x4AAAAAAAAAAA", CData: "cd"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Token() != "ts-tok" {
		t.Fatalf("token %q", task.Token())
	}
	if len(f.reqs) != 4 || f.reqs[1].Path != "/v1/task/slv_2" || f.reqs[1].Method != "GET" {
		t.Fatalf("requests %+v", f.reqs)
	}
	if f.reqs[0].Body["cData"] != "cd" {
		t.Fatalf("body %+v", f.reqs[0].Body)
	}
}

func TestFailedTaskReturnsSentinel(t *testing.T) {
	_, c := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 422, map[string]any{"id": "slv_3", "status": "failed", "type": "recaptcha_v3", "error": map[string]any{"code": "score_not_met", "message": "low"}})
	})
	task, err := c.RecaptchaV3(context.Background(), RecaptchaV3Params{URL: "https://e.com", Sitekey: "6L", Action: "login", MinScore: 0.7})
	if !errors.Is(err, ErrScoreNotMet) || errors.Is(err, ErrUnsolvable) {
		t.Fatalf("err = %v", err)
	}
	var ae *APIError
	if !errors.As(err, &ae) || ae.TaskID != "slv_3" || ae.Status != 422 || ae.Message != "low" {
		t.Fatalf("api error %+v", ae)
	}
	if task == nil || task.Status != StatusFailed {
		t.Fatalf("task %+v", task)
	}
}

func TestErrorEnvelopes(t *testing.T) {
	cases := []struct {
		status int
		body   any
		want   error
	}{
		{401, map[string]any{"error": map[string]any{"code": "invalid_api_key", "message": "bad"}}, ErrInvalidAPIKey},
		{400, map[string]any{"error": map[string]any{"code": "invalid_sitekey", "message": "x"}}, ErrInvalidSitekey},
		{400, map[string]any{"error": map[string]any{"code": "unsupported_type", "message": "x"}}, ErrUnsupportedType},
		{400, map[string]any{"error": map[string]any{"code": "proxy_unreachable", "message": "x"}}, ErrProxyUnreachable},
		{402, map[string]any{"id": "slv_4", "status": "failed", "type": "image", "error": map[string]any{"code": "insufficient_balance", "message": "top up"}}, ErrInsufficientBalance},
		{422, map[string]any{"id": "slv_5", "status": "failed", "type": "datadome", "error": map[string]any{"code": "proxy_required", "message": "x"}}, ErrProxyRequired},
		{422, map[string]any{"id": "slv_6", "status": "failed", "type": "image", "error": map[string]any{"code": "upstream_error", "message": "x"}}, ErrUpstreamError},
		{422, map[string]any{"id": "slv_7", "status": "failed", "type": "image", "error": map[string]any{"code": "unsolvable", "message": "x"}}, ErrUnsolvable},
		{503, map[string]any{"error": map[string]any{"code": "capacity_unavailable", "message": "x"}}, ErrCapacityUnavailable},
		{503, "not json", ErrCapacityUnavailable},
	}
	for _, tc := range cases {
		_, c := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) { writeJSON(w, tc.status, tc.body) })
		_, err := c.Submit(context.Background(), &SolveRequest{Type: TypeImage, Body: "aGk="})
		if !errors.Is(err, tc.want) {
			t.Errorf("HTTP %d: got %v, want %v", tc.status, err, tc.want)
		}
	}
}

func TestRateLimitRetried(t *testing.T) {
	f, c := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			writeJSON(w, 429, map[string]any{"error": map[string]any{"code": "rate_limited", "message": "slow"}})
			return
		}
		writeJSON(w, 200, map[string]any{"balance": "4.2500", "currency": "USD"})
	})
	b, err := c.Balance(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if b.Balance != "4.2500" || b.Currency != "USD" || b.Float() != 4.25 || len(f.reqs) != 2 {
		t.Fatalf("balance %+v reqs %d", b, len(f.reqs))
	}
}

func TestRateLimitExhausted(t *testing.T) {
	_, c := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		w.Header().Set("Retry-After", "0")
		writeJSON(w, 429, map[string]any{"error": map[string]any{"code": "rate_limited", "message": "slow"}})
	})
	_, err := c.Balance(context.Background())
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
}

func TestGetTaskNotFound(t *testing.T) {
	f, c := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 404, map[string]any{"error": map[string]any{"code": "task_not_found", "message": "No task with that id."}})
	})
	_, err := c.GetTask(context.Background(), "slv/../x")
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("err = %v", err)
	}
	if f.reqs[0].Path != "/v1/task/slv/../x" && f.reqs[0].Path != "/v1/task/slv%2F..%2Fx" {
		// httptest decodes the escaped path; either way it must hit /v1/task/.
		t.Fatalf("path %q", f.reqs[0].Path)
	}
}

func TestWaitForTimeout(t *testing.T) {
	_, c := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 200, map[string]any{"id": "slv_8", "status": "solving", "type": "image"})
	})
	c.maxWait = 40 * time.Millisecond
	task, err := c.WaitFor(context.Background(), "slv_8")
	if !errors.Is(err, ErrTimeout) || task.ID != "slv_8" {
		t.Fatalf("task %+v err %v", task, err)
	}
}

func TestWaitForContextCancel(t *testing.T) {
	_, c := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 200, map[string]any{"id": "slv_9", "status": "queued", "type": "image"})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := c.WaitFor(ctx, "slv_9")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestImageAndDataDome(t *testing.T) {
	f, c := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n == 1 {
			writeJSON(w, 200, readyTask("slv_10", TypeImage, map[string]any{"text": "x7k2"}))
			return
		}
		writeJSON(w, 200, readyTask("slv_11", TypeDataDome, map[string]any{"cookie": "datadome=abc", "userAgent": "UA"}))
	})
	task, err := c.Image(context.Background(), []byte("png-bytes"))
	if err != nil || task.Solution.Text != "x7k2" {
		t.Fatalf("image %+v %v", task, err)
	}
	if f.reqs[0].Body["body"] != base64.StdEncoding.EncodeToString([]byte("png-bytes")) {
		t.Fatalf("image body %+v", f.reqs[0].Body)
	}
	if _, err := c.DataDome(context.Background(), DataDomeParams{URL: "https://e.com"}); !errors.Is(err, ErrProxyRequired) {
		t.Fatalf("want proxy_required, got %v", err)
	}
	task, err = c.DataDome(context.Background(), DataDomeParams{URL: "https://e.com", ChallengeURL: "https://geo.captcha-delivery.com/x", Proxy: "http://u:p@h:1"})
	if err != nil || task.Solution.Cookie != "datadome=abc" {
		t.Fatalf("datadome %+v %v", task, err)
	}
	if f.reqs[1].Body["challengeUrl"] != "https://geo.captcha-delivery.com/x" || len(f.reqs) != 2 {
		t.Fatalf("datadome body %+v", f.reqs[1].Body)
	}
}

func TestEnterpriseAndWaitOption(t *testing.T) {
	f, _ := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 200, readyTask("slv_12", TypeRecaptchaEnterprise, map[string]any{"token": "t"}))
	})
	c := New(WithAPIKey(testKey), WithBaseURL(f.srv.URL), WithWait(500*time.Second))
	if _, err := c.RecaptchaV2(context.Background(), RecaptchaV2Params{URL: "u", Sitekey: "6L", Enterprise: true, Invisible: true}); err != nil {
		t.Fatal(err)
	}
	if f.reqs[0].Query != "wait=120" || f.reqs[0].Body["type"] != "recaptcha_enterprise" || f.reqs[0].Body["invisible"] != true {
		t.Fatalf("req %+v", f.reqs[0])
	}
	c = New(WithAPIKey(testKey), WithBaseURL(f.srv.URL), WithWait(0))
	if _, err := c.Submit(context.Background(), &SolveRequest{Type: TypeImage, Body: "x"}); err != nil {
		t.Fatal(err)
	}
	if f.reqs[1].Query != "" {
		t.Fatalf("submit must not block: %q", f.reqs[1].Query)
	}
}

func TestMissingKeyAndEnv(t *testing.T) {
	t.Setenv("CAPTCHANO_API_KEY", "")
	_, err := New().Balance(context.Background())
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("err = %v", err)
	}
	t.Setenv("CAPTCHANO_API_KEY", "cn-from-env")
	if New().apiKey != "cn-from-env" {
		t.Fatal("env key not read")
	}
}

func TestConnectionError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := New(WithAPIKey(testKey), WithBaseURL(url)).Balance(context.Background())
	var ce *ConnectionError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %T %v", err, err)
	}
}

func TestNumberAcceptsStrings(t *testing.T) {
	var task Task
	if err := json.Unmarshal([]byte(`{"id":"a","status":"ready","solveTime":"3.5","createdAt":"17"}`), &task); err != nil {
		t.Fatal(err)
	}
	if task.SolveTime != 3.5 || task.CreatedAt != 17 {
		t.Fatalf("%+v", task)
	}
}
