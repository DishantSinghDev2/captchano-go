package captchano

import (
	"encoding/json"
	"strconv"
)

// ChallengeType names a captcha family the API can solve.
type ChallengeType string

// Supported challenge types.
const (
	TypeRecaptchaV2         ChallengeType = "recaptcha_v2"
	TypeRecaptchaV3         ChallengeType = "recaptcha_v3"
	TypeRecaptchaEnterprise ChallengeType = "recaptcha_enterprise"
	TypeTurnstile           ChallengeType = "turnstile"
	TypeDataDome            ChallengeType = "datadome"
	TypeImage               ChallengeType = "image"
)

// Status is the lifecycle state of a task.
type Status string

// Task states. A task moves from queued/solving to ready or failed.
const (
	StatusQueued  Status = "queued"
	StatusSolving Status = "solving"
	StatusReady   Status = "ready"
	StatusFailed  Status = "failed"
)

// SolveRequest is the body of POST /v1/solve. Use the per-type helpers
// (RecaptchaV2, Turnstile, ...) unless you need a field they do not expose.
type SolveRequest struct {
	Type ChallengeType `json:"type"`
	// URL of the page the challenge appears on. Not used by image.
	URL string `json:"url,omitempty"`
	// Sitekey of the widget. Required for reCAPTCHA and Turnstile.
	Sitekey string `json:"sitekey,omitempty"`
	// MinScore for score-based reCAPTCHA (0.1-0.9).
	MinScore float64 `json:"minScore,omitempty"`
	// Action the page reports, for reCAPTCHA v3 and Turnstile.
	Action string `json:"action,omitempty"`
	// CData and PageData are Turnstile challenge-page extras.
	CData    string `json:"cData,omitempty"`
	PageData string `json:"pageData,omitempty"`
	// ChallengeURL is the DataDome challenge iframe src. Required for datadome.
	ChallengeURL string `json:"challengeUrl,omitempty"`
	// Invisible marks an invisible reCAPTCHA widget.
	Invisible bool `json:"invisible,omitempty"`
	// Body is the base64-encoded image, for image.
	Body string `json:"body,omitempty"`
	// UserAgent the solution should be bound to.
	UserAgent string `json:"userAgent,omitempty"`
	// Proxy as scheme://user:pass@host:port (http, socks4 or socks5).
	// Optional for token challenges, required for datadome.
	Proxy string `json:"proxy,omitempty"`
}

// Solution holds whatever the challenge produced.
type Solution struct {
	// Token to submit, for token-based challenges.
	Token string `json:"token,omitempty"`
	// Cookie to set, for session-graded systems such as DataDome.
	Cookie string `json:"cookie,omitempty"`
	// Text, for image challenges.
	Text string `json:"text,omitempty"`
	// UserAgent the solution is bound to. Replay it exactly when submitting.
	UserAgent string `json:"userAgent,omitempty"`
}

// TaskError is the error attached to a failed task.
type TaskError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Task is a solve job as returned by POST /v1/solve and GET /v1/task/{id}.
type Task struct {
	ID       string        `json:"id"`
	Status   Status        `json:"status"`
	Type     ChallengeType `json:"type"`
	Solution *Solution     `json:"solution,omitempty"`
	Error    *TaskError    `json:"error,omitempty"`
	// Cost charged in USD as a decimal string. Present once ready.
	Cost string `json:"cost,omitempty"`
	// BilledFrom is "plan" or "balance". Present once ready.
	BilledFrom string `json:"billedFrom,omitempty"`
	// SolveTime is seconds from submission to solution.
	SolveTime Number `json:"solveTime,omitempty"`
	// CreatedAt is Unix epoch milliseconds.
	CreatedAt Number `json:"createdAt,omitempty"`
}

// Done reports whether the task reached a final state (ready or failed).
func (t *Task) Done() bool {
	return t != nil && (t.Status == StatusReady || t.Status == StatusFailed)
}

// Token is a shortcut for t.Solution.Token ("" when absent).
func (t *Task) Token() string {
	if t == nil || t.Solution == nil {
		return ""
	}
	return t.Solution.Token
}

// Err returns the task's failure as an *APIError, or nil unless Status is failed.
func (t *Task) Err() error {
	if t == nil || t.Status != StatusFailed {
		return nil
	}
	e := &APIError{TaskID: t.ID, Code: "unsolvable", Message: "the task failed"}
	if t.Error != nil {
		if t.Error.Code != "" {
			e.Code = t.Error.Code
		}
		if t.Error.Message != "" {
			e.Message = t.Error.Message
		}
	}
	return e
}

// Balance is the response of GET /v1/balance.
type Balance struct {
	// Balance is the remaining prepaid credit as a decimal string, e.g. "4.2500".
	Balance  string `json:"balance"`
	Currency string `json:"currency"`
}

// Float returns the balance as a float64 (0 if it cannot be parsed).
func (b *Balance) Float() float64 {
	f, _ := strconv.ParseFloat(b.Balance, 64)
	return f
}

// Number accepts a JSON number or a numeric string.
type Number float64

// UnmarshalJSON implements json.Unmarshaler.
func (n *Number) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if s == "" {
			return nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return err
		}
		*n = Number(f)
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	*n = Number(f)
	return nil
}
