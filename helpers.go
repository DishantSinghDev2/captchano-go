package captchano

import (
	"context"
	"encoding/base64"
	"errors"
)

// RecaptchaV2Params describes a reCAPTCHA v2 checkbox or invisible widget.
type RecaptchaV2Params struct {
	// URL of the page that shows the widget.
	URL string
	// Sitekey from data-sitekey or the k= parameter (starts with 6L).
	Sitekey string
	// Invisible marks an invisible widget.
	Invisible bool
	// Enterprise solves an Enterprise checkbox-style widget (type recaptcha_enterprise).
	Enterprise bool
	UserAgent  string
	// Proxy as scheme://user:pass@host:port. Use the IP that will submit the token.
	Proxy string
}

// RecaptchaV3Params describes a score-based reCAPTCHA v3 widget.
type RecaptchaV3Params struct {
	URL     string
	Sitekey string
	// Action the page passes to grecaptcha.execute.
	Action string
	// MinScore (0.1-0.9). Zero lets the API choose.
	MinScore  float64
	UserAgent string
	Proxy     string
}

// TurnstileParams describes a Cloudflare Turnstile widget.
type TurnstileParams struct {
	URL string
	// Sitekey (starts with 0x).
	Sitekey string
	Action  string
	// CData and PageData are only needed on Cloudflare challenge pages.
	CData     string
	PageData  string
	UserAgent string
	Proxy     string
}

// DataDomeParams describes a DataDome challenge. Proxy is required.
type DataDomeParams struct {
	// URL of the protected page.
	URL string
	// ChallengeURL is the src of the DataDome challenge iframe.
	ChallengeURL string
	// Proxy as scheme://user:pass@host:port. Required.
	Proxy     string
	UserAgent string
}

// RecaptchaV2 solves a reCAPTCHA v2 (or Enterprise checkbox) widget.
// The token is in task.Solution.Token.
func (c *Client) RecaptchaV2(ctx context.Context, p RecaptchaV2Params) (*Task, error) {
	t := TypeRecaptchaV2
	if p.Enterprise {
		t = TypeRecaptchaEnterprise
	}
	return c.Solve(ctx, &SolveRequest{
		Type: t, URL: p.URL, Sitekey: p.Sitekey, Invisible: p.Invisible,
		UserAgent: p.UserAgent, Proxy: p.Proxy,
	})
}

// RecaptchaV3 solves a score-based reCAPTCHA v3 widget.
func (c *Client) RecaptchaV3(ctx context.Context, p RecaptchaV3Params) (*Task, error) {
	return c.Solve(ctx, &SolveRequest{
		Type: TypeRecaptchaV3, URL: p.URL, Sitekey: p.Sitekey, Action: p.Action,
		MinScore: p.MinScore, UserAgent: p.UserAgent, Proxy: p.Proxy,
	})
}

// Turnstile solves a Cloudflare Turnstile widget.
func (c *Client) Turnstile(ctx context.Context, p TurnstileParams) (*Task, error) {
	return c.Solve(ctx, &SolveRequest{
		Type: TypeTurnstile, URL: p.URL, Sitekey: p.Sitekey, Action: p.Action,
		CData: p.CData, PageData: p.PageData, UserAgent: p.UserAgent, Proxy: p.Proxy,
	})
}

// DataDome solves a DataDome challenge. The cookie to set is in
// task.Solution.Cookie; replay task.Solution.UserAgent and the same proxy.
func (c *Client) DataDome(ctx context.Context, p DataDomeParams) (*Task, error) {
	if p.Proxy == "" {
		return nil, &APIError{Code: CodeProxyRequired, Message: "DataDome requires a proxy"}
	}
	return c.Solve(ctx, &SolveRequest{
		Type: TypeDataDome, URL: p.URL, ChallengeURL: p.ChallengeURL,
		Proxy: p.Proxy, UserAgent: p.UserAgent,
	})
}

// Image solves an image (text) captcha from its raw bytes (PNG, JPEG, GIF...).
// The answer is in task.Solution.Text.
func (c *Client) Image(ctx context.Context, image []byte) (*Task, error) {
	if len(image) == 0 {
		return nil, errors.New("captchano: image is empty")
	}
	return c.Solve(ctx, &SolveRequest{Type: TypeImage, Body: base64.StdEncoding.EncodeToString(image)})
}
