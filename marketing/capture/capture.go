package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/voxpupuli/enterprise-console/marketing/shots"
)

// capture screenshots every declared view in both themes.
//
// Themes are the outer loop and views the inner one: switching theme
// means a fresh browser context (the theme is applied before the first
// document loads), which is the expensive part, while moving between
// views within a theme is just navigation.
func capture(ctx context.Context, opts options) error {
	wsURL, err := devtoolsURL(ctx, opts.browserURL)
	if err != nil {
		return err
	}

	selected, err := selectShots(opts.only)
	if err != nil {
		return err
	}

	tokens, err := logIn(ctx, opts)
	if err != nil {
		return err
	}

	allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(ctx, wsURL)
	defer cancelAlloc()

	var written []string
	for _, theme := range shots.Themes {
		paths, err := captureTheme(allocCtx, opts, theme, tokens, selected)
		written = append(written, paths...)
		if err != nil {
			// Say what did get written, so a partial run is visible
			// rather than leaving the asset directory in a state the
			// operator has to work out for themselves.
			if len(written) > 0 {
				fmt.Fprintf(os.Stderr, "\n%d image(s) were written before this failure:\n", len(written))
				for _, p := range written {
					fmt.Fprintf(os.Stderr, "  %s\n", filepath.Base(p))
				}
				fmt.Fprintf(os.Stderr, "The set is incomplete - do not commit it until a full run succeeds.\n\n")
			}
			return err
		}
	}

	fmt.Printf("\n%d images written to %s\n", len(written), opts.outDir)
	return nil
}

// captureTheme runs one theme's pass in its own browser context.
func captureTheme(allocCtx context.Context, opts options, theme shots.Theme, tokens tokenPair, selected []shots.Shot) ([]string, error) {
	ctx, cancel := chromedp.NewContext(allocCtx, chromedp.WithErrorf(quietErrorf))
	defer cancel()

	// Registered once for the context, and applied to every document
	// loaded in it - which is what puts the theme, the frozen clock and
	// the session in place before any of the console's own scripts run.
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(initScript(theme, tokens, opts.locale)).Do(ctx)
		return err
	})); err != nil {
		return nil, fmt.Errorf("%s: installing the page init script: %w", theme, err)
	}

	var written []string
	for _, shot := range selected {
		path, err := captureShot(ctx, opts, theme, shot)
		if err != nil {
			return written, err
		}
		written = append(written, path)
		fmt.Printf("  %-14s %s\n", string(theme), filepath.Base(path))
	}
	return written, nil
}

// tokenPair is what the console's login endpoint returns.
type tokenPair struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

// logIn authenticates against the console's API and returns the token
// pair the frontend would have stored.
//
// The browser is not driven through the login form. The form's fields
// are voxblocks custom elements, so their host elements are not
// focusable and the real <input> sits inside a shadow root that a CSS
// selector cannot reach - typing into it means reaching through the
// shadow boundary from injected JavaScript, which is brittle in exactly
// the way this tool cannot afford.
//
// Calling the same endpoint the form calls and seeding localStorage with
// the result is what the frontend itself does on a successful login, and
// every page afterwards is the genuinely authenticated UI.
func logIn(ctx context.Context, opts options) (tokenPair, error) {
	body, err := json.Marshal(map[string]string{
		"username": opts.username,
		"password": opts.password,
	})
	if err != nil {
		return tokenPair{}, err
	}

	url := strings.TrimSuffix(opts.consoleURL, "/") + "/api/v1/auth/login"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return tokenPair{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return tokenPair{}, fmt.Errorf("no console at %s: %w", opts.consoleURL, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return tokenPair{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return tokenPair{}, fmt.Errorf("logging in to %s as %q failed (status %d): %s",
			opts.consoleURL, opts.username, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var tokens tokenPair
	if err := json.Unmarshal(respBody, &tokens); err != nil {
		return tokenPair{}, fmt.Errorf("parsing the console's login response: %w", err)
	}
	if tokens.AccessToken == "" {
		return tokenPair{}, fmt.Errorf("the console's login response carried no access token")
	}
	return tokens, nil
}

// captureShot navigates to one view, waits for it to genuinely render,
// and writes its PNG.
func captureShot(ctx context.Context, opts options, theme shots.Theme, shot shots.Shot) (string, error) {
	// The browser's view of the console, which is not necessarily this
	// tool's - see options.browserConsoleURL.
	url := strings.TrimSuffix(opts.browserConsoleURL, "/") + shot.Path

	// Per-shot deadline: a view that never renders should name itself
	// and fail, not consume the whole run's budget.
	shotCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	actions := []chromedp.Action{
		chromedp.EmulateViewport(int64(shot.Width), int64(shot.Height)),
		chromedp.Navigate(url),
	}
	if shot.ReadyWhen != "" {
		actions = append(actions, chromedp.WaitVisible(shot.ReadyWhen, chromedp.ByQuery))
	}
	for _, step := range shot.Interactions {
		if step.Click != "" {
			actions = append(actions, chromedp.Click(step.Click, chromedp.ByQuery))
		}
		if step.Selector != "" {
			actions = append(actions, chromedp.SendKeys(step.Selector, step.Text, chromedp.ByQuery))
		}
		if step.WaitFor != "" {
			actions = append(actions, chromedp.WaitVisible(step.WaitFor, chromedp.ByQuery))
		}
	}
	actions = append(actions,
		chromedp.Evaluate(stillnessScript(), nil),
		// The console's layout pulls its typefaces from Google Fonts.
		// Screenshotting before they arrive captures the fallback face,
		// and whether that happens depends on the network rather than
		// on the page - exactly the kind of run-to-run variation the
		// determinism requirement rules out. document.fonts.ready
		// settles either way, so a genuinely unreachable font service
		// ends in a consistent fallback rather than a hang.
		chromedp.ActionFunc(func(ctx context.Context) error {
			return chromedp.Evaluate(`document.fonts.ready.then(() => true)`, nil,
				func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
					return p.WithAwaitPromise(true)
				}).Do(ctx)
		}),
	)

	if err := chromedp.Run(shotCtx, actions...); err != nil {
		return "", describeShotFailure(shot, theme, err)
	}

	// The content check runs after the view has rendered: a page can
	// satisfy ReadyWhen with an empty-state row in it, which would
	// screenshot as a product with no data.
	if err := verifyContent(shotCtx, shot, theme); err != nil {
		return "", err
	}

	var buf []byte
	shootAction := chromedp.CaptureScreenshot(&buf)
	if shot.FullPage {
		shootAction = chromedp.FullScreenshot(&buf, 100)
	}
	if err := chromedp.Run(shotCtx, shootAction); err != nil {
		return "", fmt.Errorf("screenshotting %s (%s): %w", shot.Name, theme, err)
	}

	buf, err := optimisePNG(buf)
	if err != nil {
		return "", fmt.Errorf("optimising %s (%s): %w", shot.Name, theme, err)
	}

	if len(buf) > shots.MaxBytes {
		return "", fmt.Errorf(
			"%s (%s) is %d bytes, over the %d-byte limit.\n"+
				"These images are committed and replaced on every refresh, so an oversized one\n"+
				"grows the repository permanently. Narrow the shot's viewport, turn off FullPage,\n"+
				"or raise shots.MaxBytes deliberately",
			shot.Name, theme, len(buf), shots.MaxBytes)
	}

	path := filepath.Join(opts.outDir, shot.FileName(theme, opts.locale))
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}

// verifyContent checks the rendered page actually contains what the shot
// says it must.
func verifyContent(ctx context.Context, shot shots.Shot, theme shots.Theme) error {
	if shot.MustContain == "" {
		return nil
	}

	var text string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.body.innerText`, &text)); err != nil {
		return fmt.Errorf("reading %s (%s) to verify its content: %w", shot.Name, theme, err)
	}
	if strings.Contains(text, shot.MustContain) {
		return nil
	}

	return fmt.Errorf(
		"%s (%s) rendered without %q in it.\n"+
			"The view loaded but does not hold the data it is supposed to show - most likely\n"+
			"the console is not seeded, or is seeded with something other than the demo fleet.\n"+
			"Run `make demo-seed` against it and try again. No image was written",
		shot.Name, theme, shot.MustContain)
}

// describeShotFailure turns a navigation or wait failure into something
// that names the view and says what to check.
func describeShotFailure(shot shots.Shot, theme shots.Theme, err error) error {
	return fmt.Errorf(
		"%s (%s) at %s did not reach the state it declares: %w\n"+
			"It waits for %q. Either the view is erroring, or it rendered an empty state\n"+
			"because the console is not seeded. No image was written",
		shot.Name, theme, shot.Path, err, shot.ReadyWhen)
}

// selectShots resolves --only, defaulting to everything.
func selectShots(only string) ([]shots.Shot, error) {
	if only == "" {
		return shots.All, nil
	}
	shot, ok := shots.Find(only)
	if !ok {
		names := make([]string, 0, len(shots.All))
		for _, s := range shots.All {
			names = append(names, s.Name)
		}
		return nil, fmt.Errorf("no shot named %q - declared shots are: %s", only, strings.Join(names, ", "))
	}
	return []shots.Shot{shot}, nil
}

// devtoolsURL turns a browser endpoint into the websocket URL chromedp
// connects with. An http:// endpoint is resolved via /json/version,
// which is what the headless container publishes; a ws:// one is used
// as given.
func devtoolsURL(ctx context.Context, endpoint string) (string, error) {
	if strings.HasPrefix(endpoint, "ws://") || strings.HasPrefix(endpoint, "wss://") {
		return endpoint, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(endpoint, "/")+"/json/version", nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("no headless browser at %s: %w\nStart it with `make screenshots-up`", endpoint, err)
	}
	defer resp.Body.Close()

	var version struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&version); err != nil {
		return "", fmt.Errorf("parsing the browser's /json/version response: %w", err)
	}
	if version.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("the browser at %s reported no websocket debugger URL", endpoint)
	}
	return version.WebSocketDebuggerURL, nil
}
