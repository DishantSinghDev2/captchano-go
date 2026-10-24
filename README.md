# CaptchaNo Go SDK — Captcha Solver API Client for Golang

[![Go Reference](https://pkg.go.dev/badge/github.com/DishantSinghDev2/captchano-go.svg)](https://pkg.go.dev/github.com/DishantSinghDev2/captchano-go)
[![Go Report Card](https://goreportcard.com/badge/github.com/DishantSinghDev2/captchano-go)](https://goreportcard.com/report/github.com/DishantSinghDev2/captchano-go)
[![license](https://img.shields.io/badge/license-MIT-green)](LICENSE)

The official **Go SDK** for [CaptchaNo](https://captchano.com): solve **reCAPTCHA v2, v3 and Enterprise, Cloudflare Turnstile, DataDome and image captchas** from Go. Idiomatic Go — `context` on every call, `errors.Is` sentinels for every API error code, functional options, and **zero third-party dependencies**.

**Keywords:** captcha solver · recaptcha solver · turnstile solver · datadome · image captcha · captcha API · chromedp · web scraping · browser automation

## Features

- Standard library only — no transitive dependencies
- `context.Context` on every call; safe for concurrent use
- One call per solve: the client blocks server-side with `?wait=` and falls back to polling
- `errors.Is(err, captchano.ErrInvalidSitekey)` and a sentinel for every other API error code
- You are billed only for successful solves; failed tasks come back as typed errors

## Installation

```bash
go get github.com/DishantSinghDev2/captchano-go
```

Go 1.21+. Standard library only.

Get an API key (starts with `cn-`) from the [dashboard](https://captchano.com/dashboard/keys) and export it:

```bash
export CAPTCHANO_API_KEY=cn-...
```

## Quick start

```go
package main

import (
	"context"
	"fmt"
	"log"

	captchano "github.com/DishantSinghDev2/captchano-go"
)

func main() {
	ctx := context.Background()
	client := captchano.New() // reads CAPTCHANO_API_KEY

	task, err := client.RecaptchaV2(ctx, captchano.RecaptchaV2Params{
		URL:     "https://www.google.com/recaptcha/api2/demo",
		Sitekey: "6Le-wvkSAAAAAPBMRTvw0Q4Muexq9bi0DJwx_mJ-",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("token:", task.Token(), "cost:", task.Cost)
}
```

Every helper returns a `*captchano.Task` with `ID`, `Status`, `Solution` (`Token`, `Cookie`, `Text`, `UserAgent`), `Cost` (USD string), `BilledFrom` and `SolveTime`.

## Every challenge type

### reCAPTCHA v2 (checkbox, invisible, Enterprise)

```go
task, err := client.RecaptchaV2(ctx, captchano.RecaptchaV2Params{
	URL:       "https://example.com/login",
	Sitekey:   "6Lc...",                      // data-sitekey, or k= in the iframe URL
	Invisible: false,                         // true for invisible widgets
	Enterprise: false,                        // true for Enterprise checkbox widgets
	Proxy:     "http://user:pass@host:port",  // optional; the IP that will submit the token
	UserAgent: "Mozilla/5.0 ...",             // optional
})
token := task.Token()
```

### reCAPTCHA v3

```go
task, err := client.RecaptchaV3(ctx, captchano.RecaptchaV3Params{
	URL:      "https://example.com/checkout",
	Sitekey:  "6Lc...",
	Action:   "submit", // the action the page passes to grecaptcha.execute
	MinScore: 0.7,
})
```

### Cloudflare Turnstile

```go
task, err := client.Turnstile(ctx, captchano.TurnstileParams{
	URL:     "https://example.com/signup",
	Sitekey: "0x4AAAAAAA...",
	Action:  "signup",  // optional
	CData:   "",        // challenge pages only
	PageData: "",       // challenge pages only
})
```

### DataDome

```go
task, err := client.DataDome(ctx, captchano.DataDomeParams{
	URL:          "https://example.com/",
	ChallengeURL: "https://geo.captcha-delivery.com/captcha/?initialCid=...", // the challenge iframe src
	Proxy:        "http://user:pass@host:port",                               // required
	UserAgent:    "Mozilla/5.0 ...",
})
cookie := task.Solution.Cookie       // set it, then
ua := task.Solution.UserAgent        // replay this user agent through the same proxy
```

### Image captcha

```go
img, _ := os.ReadFile("captcha.png")
task, err := client.Image(ctx, img)
fmt.Println(task.Solution.Text)
```

### Raw requests, non-blocking submit and polling

```go
// Any field the API accepts
task, err := client.Solve(ctx, &captchano.SolveRequest{Type: captchano.TypeTurnstile, URL: u, Sitekey: k})

// Fire and forget, collect later (solutions are kept for ten minutes)
task, _ = client.Submit(ctx, &captchano.SolveRequest{Type: captchano.TypeImage, Body: b64})
task, err = client.WaitFor(ctx, task.ID) // polls GET /v1/task/{id}
task, err = client.GetTask(ctx, task.ID) // one-shot

// Remaining credit
bal, _ := client.Balance(ctx)
fmt.Println(bal.Balance, bal.Currency) // "4.2500 USD"
```

## Injecting the token with chromedp

Read the sitekey from the page, solve, write the token into the hidden response field and submit. Passing the browser's own user agent (and the browser's proxy, if any) keeps the token bound to the session that submits it. Written for chromedp v0.21+ (`go get github.com/chromedp/chromedp`).

```go
package main

import (
	"context"
	"encoding/json"
	"log"
	"time"

	captchano "github.com/DishantSinghDev2/captchano-go"
	"github.com/chromedp/chromedp"
)

func main() {
	const pageURL = "https://www.google.com/recaptcha/api2/demo"

	browser, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	if err := chromedp.Do(browser, chromedp.Navigate(pageURL)); err != nil {
		log.Fatal(err)
	}
	sitekey, err := chromedp.Run(browser, chromedp.Evaluate[string](`document.querySelector(".g-recaptcha").dataset.sitekey`))
	if err != nil {
		log.Fatal(err)
	}
	ua, err := chromedp.Run(browser, chromedp.Evaluate[string](`navigator.userAgent`))
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := context.WithTimeout(context.Background(), 3*time.Minute)
	defer stop()
	task, err := captchano.New().RecaptchaV2(ctx, captchano.RecaptchaV2Params{
		URL: pageURL, Sitekey: sitekey, UserAgent: ua,
	})
	if err != nil {
		log.Fatal(err)
	}

	tok, _ := json.Marshal(task.Token())
	if _, err := chromedp.Run(browser, chromedp.Evaluate[bool](`(document.getElementById("g-recaptcha-response").value = `+string(tok)+`, true)`)); err != nil {
		log.Fatal(err)
	}
	if err := chromedp.Do(browser,
		chromedp.Click(`#recaptcha-demo-submit`),
		chromedp.WaitVisible(`.recaptcha-success`),
	); err != nil {
		log.Fatal(err)
	}
	log.Println("form accepted")
}
```

For Turnstile, write the token into `input[name="cf-turnstile-response"]` instead. If the site uses a JavaScript callback (`data-callback`), call it with the token after setting the field.

## Error handling

Every API error code has a sentinel:

| Sentinel | Code | Meaning |
|---|---|---|
| `ErrInvalidAPIKey` | `invalid_api_key` | Key missing, revoked or wrong |
| `ErrInsufficientBalance` | `insufficient_balance` | Balance cannot cover one solve |
| `ErrUnsupportedType` | `unsupported_type` | Unknown type or a required field is missing |
| `ErrInvalidSitekey` | `invalid_sitekey` | Sitekey missing, of the wrong family, or rejected for the page |
| `ErrProxyRequired` | `proxy_required` | DataDome without a proxy |
| `ErrProxyUnreachable` | `proxy_unreachable` | Proxy malformed or refused the connection |
| `ErrScoreNotMet` | `score_not_met` | reCAPTCHA v3 score below `MinScore` |
| `ErrUnsolvable` | `unsolvable` | The challenge could not be solved (not billed) |
| `ErrCapacityUnavailable` | `capacity_unavailable` | Temporarily unable to accept solves; retry with backoff |
| `ErrRateLimited` | `rate_limited` | Too many requests |
| `ErrTaskNotFound` | `task_not_found` | No task with that id on your account |
| `ErrUpstreamError` | `upstream_error` | The solve could not be completed (not billed) |

```go
task, err := client.Turnstile(ctx, params)
switch {
case errors.Is(err, captchano.ErrInsufficientBalance):
	log.Println("top up at https://captchano.com/dashboard/topup")
case errors.Is(err, captchano.ErrInvalidSitekey):
	log.Println("re-read the sitekey from the page")
case errors.Is(err, captchano.ErrTimeout):
	log.Println("still solving; resume with client.WaitFor(ctx, task.ID)")
case err != nil:
	var apiErr *captchano.APIError
	if errors.As(err, &apiErr) {
		log.Printf("%s (HTTP %d, task %s): %s", apiErr.Code, apiErr.Status, apiErr.TaskID, apiErr.Message)
	}
}
```

API errors and failed tasks are `*captchano.APIError` (`Code`, `Message`, `Status`, `TaskID`, `RetryAfter`, `Body`); network failures are `*captchano.ConnectionError`. When a task fails, the helpers return both the `*Task` and the error.

## Configuration

```go
client := captchano.New(
	captchano.WithAPIKey(os.Getenv("CAPTCHANO_API_KEY")), // default: $CAPTCHANO_API_KEY
	captchano.WithBaseURL("https://api.captchano.com"),
	captchano.WithHTTPClient(&http.Client{Transport: myTransport}),
	captchano.WithTimeout(30*time.Second),      // per request; blocking solves add the wait
	captchano.WithWait(60*time.Second),         // server-side ?wait= (max 120s, 0 = poll only)
	captchano.WithPollInterval(5*time.Second),  // GET /v1/task interval
	captchano.WithMaxWait(3*time.Minute),       // cap per Solve / WaitFor
	captchano.WithMaxRetries(2),                // retries for HTTP 429
)
```

## API overview

| Capability | Call |
|---|---|
| Solve and block until ready | `client.Solve(ctx, req)` |
| Submit without blocking | `client.Submit(ctx, req)` |
| Fetch a task once | `client.GetTask(ctx, id)` |
| Poll until ready or failed | `client.WaitFor(ctx, id)` |
| Remaining credit | `client.Balance(ctx)` |
| reCAPTCHA v2 / Enterprise | `client.RecaptchaV2(ctx, RecaptchaV2Params{...})` |
| reCAPTCHA v3 | `client.RecaptchaV3(ctx, RecaptchaV3Params{...})` |
| Cloudflare Turnstile | `client.Turnstile(ctx, TurnstileParams{...})` |
| DataDome | `client.DataDome(ctx, DataDomeParams{...})` |
| Image captcha | `client.Image(ctx, imageBytes)` |

Full endpoint reference: <https://captchano.com/captcha-solver-api> · Per-type prices: <https://captchano.com/pricing>

## FAQ

**How do I get an API key?**
Create an account at [captchano.com](https://captchano.com), then open **Dashboard → API keys**. Keys start with `cn-`. Keep them in `CAPTCHANO_API_KEY` or a secret manager, never in source code.

**Am I charged for failed solves?**
No. You pay only for solves that return a usable solution. See [pricing](https://captchano.com/pricing) for per-type prices.

**Why was my token rejected by the site?**
The most common cause is submitting from a different IP than the one the token was minted for. Pass the proxy your scraper uses as `Proxy`, and replay `Solution.UserAgent` when the solution includes one. For reCAPTCHA v3, send the exact `Action` the page uses.

**Is it free to use the SDK?**
Yes — the SDK is MIT-licensed. You pay only for successful solves.

## Development

```bash
go vet ./... && go test -race ./...
```

Issues and pull requests are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md). Security reports: [SECURITY.md](SECURITY.md).

## Links

- Website: <https://captchano.com>
- API reference: <https://captchano.com/captcha-solver-api>
- Pricing: <https://captchano.com/pricing>
- Support: <support@captchano.com>

## License

[MIT](LICENSE) © DishIs Technologies LLC
