// Command a11y audits the built marketing site against WCAG.
//
// It loads every page in a real browser and runs axe-core over it, in
// both themes and at both a desktop and a phone viewport - because
// contrast depends on the theme and reflow depends on the width, so a
// single pass would miss half of what there is to find.
//
// It is a guard, not a substitute for looking. axe finds the machine-
// checkable subset: contrast ratios, missing names, broken landmark and
// heading structure, invalid ARIA. It cannot tell you whether alt text
// describes the right thing or whether the focus order makes sense. Those
// are reviewed by hand and recorded in marketing/README.md.
//
// Usage:
//
//	make marketing-a11y
//
// or directly, against an already-served site:
//
//	go run ./marketing/a11y --base http://host.docker.internal:8777
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// pages mirrors the site's page list. Kept here rather than imported from
// gen so this tool audits URLs as a visitor requests them.
var pages = []string{
	"index.html",
	"features/nodes.html",
	"features/classification.html",
	"features/code.html",
	"features/orchestration.html",
	"features/security.html",
	"features/access-control.html",
}

// viewports are audited separately. A contrast or reflow problem can
// exist at one width and not the other, and the phone width is where the
// header collapses into a menu.
var viewports = []struct {
	Name          string
	Width, Height int64
}{
	{"desktop", 1440, 900},
	{"phone", 390, 844},
}

var themes = []string{"light", "dark"}

// mustRun are rules whose absence from a run would be silent. axe ships
// target-size disabled, so selecting its tag is not enough to run it -
// and a rule that does not run produces no violations, which reads as a
// pass. Asserting it ran is the difference between checking WCAG 2.2's
// target-size criterion and only believing you did.
var mustRun = []string{"target-size", "color-contrast"}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// wcagTags are the conformance levels audited. WCAG 2.2 AA is the
// current target; the 2.0 and 2.1 tags are included because axe maps
// rules to the earliest level that introduced them.
var wcagTags = []string{"wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"}

// accepted are findings reviewed and deliberately allowed to stand,
// keyed by rule and element selector. Everything else still fails, so
// this suppresses one known case rather than a whole rule.
//
// Each entry needs a reason. An allowlist without one becomes a place
// where real findings go to be forgotten.
var accepted = map[string]map[string]string{
	"region": {
		".skip-link": "A skip link must come before every landmark - that is what it is " +
			"for - so it cannot itself be inside one. axe's region rule is " +
			"best-practice rather than a WCAG criterion, and flags this by design.",
	},
}

// isAccepted reports whether this rule/element pair is a recorded
// exception.
func isAccepted(rule, target string) (string, bool) {
	targets, ok := accepted[rule]
	if !ok {
		return "", false
	}
	reason, ok := targets[target]
	return reason, ok
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "a11y: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		base        = flag.String("base", "http://host.docker.internal:8777", "base URL of the served site, as the browser reaches it")
		browserURL  = flag.String("browser-url", "http://localhost:9222", "DevTools endpoint of the headless browser")
		axePath     = flag.String("axe", "", "path to axe.min.js (default: marketing/node_modules/axe-core/axe.min.js)")
		bestPrimary = flag.Bool("best-practice", false, "also report axe's best-practice rules, which are advisory rather than WCAG failures")
	)
	flag.Parse()

	axeJS, err := loadAxe(*axePath)
	if err != nil {
		return err
	}

	ws, err := devtoolsURL(*browserURL)
	if err != nil {
		return err
	}

	alloc, cancelAlloc := chromedp.NewRemoteAllocator(context.Background(), ws)
	defer cancelAlloc()

	tags := wcagTags
	if *bestPrimary {
		tags = append(append([]string{}, wcagTags...), "best-practice")
	}

	findings := map[string]*finding{}
	var checked int

	for _, vp := range viewports {
		for _, theme := range themes {
			for _, page := range pages {
				result, err := auditPage(alloc, axeJS, *base, page, theme, vp.Width, vp.Height, tags)
				if err != nil {
					return fmt.Errorf("%s (%s, %s): %w", page, theme, vp.Name, err)
				}
				checked++
				for _, want := range mustRun {
					if !contains(result.Ran, want) {
						return fmt.Errorf(
							"the %q rule did not run on %s (%s, %s).\n"+
								"It reports no violations when it does not run, which looks exactly like passing.\n"+
								"Check that its tag is in wcagTags and that it is enabled by name",
							want, page, theme, vp.Name)
					}
				}

				for _, v := range result.Violations {
					f, ok := findings[v.ID]
					if !ok {
						f = &finding{Rule: v.ID, Impact: v.Impact, Help: v.Help, HelpURL: v.HelpURL,
							Contexts: map[string]bool{}, Nodes: map[string][]string{}}
						findings[v.ID] = f
					}
					var kept int
					for _, n := range v.Nodes {
						target := join(n.Target)
						if _, ok := isAccepted(v.ID, target); ok {
							continue
						}
						f.Nodes[target] = append(f.Nodes[target], n.FailureSummary)
						kept++
					}
					if kept > 0 {
						f.Contexts[fmt.Sprintf("%s (%s, %s)", page, theme, vp.Name)] = true
					}
				}
			}
		}
	}

	// Checks axe cannot make, run after it so a page that fails both
	// reports both rather than stopping at the first.
	var extra []string
	for _, check := range []struct {
		name string
		run  func() ([]string, error)
	}{
		{"reflow at 320px (WCAG 1.4.10)", func() ([]string, error) { return checkReflow(alloc, *base, pages, themes) }},
		{"text spacing (WCAG 1.4.12)", func() ([]string, error) { return checkTextSpacing(alloc, *base, pages) }},
		{"skip link (WCAG 2.4.1)", func() ([]string, error) { return checkSkipLink(alloc, *base, pages) }},
	} {
		problems, err := check.run()
		if err != nil {
			return fmt.Errorf("%s: %w", check.name, err)
		}
		if len(problems) == 0 {
			fmt.Printf("  ok  %s\n", check.name)
			continue
		}
		for _, p := range problems {
			extra = append(extra, fmt.Sprintf("[%s] %s", check.name, p))
		}
	}

	// A rule whose every failing node was an accepted exception is not a
	// finding at all, and reporting it with an empty element list would
	// be noise that trains people to ignore the report.
	for id, f := range findings {
		if len(f.Nodes) == 0 {
			delete(findings, id)
		}
	}

	err = report(findings, checked)
	if len(extra) > 0 {
		fmt.Printf("\n%d additional finding(s):\n", len(extra))
		for _, e := range extra {
			fmt.Printf("  %s\n", e)
		}
		return fmt.Errorf("%d accessibility finding(s) outside axe's rule set", len(extra))
	}
	return err
}

// auditPage loads one page in one theme at one viewport and runs axe.
func auditPage(alloc context.Context, axeJS, base, page, theme string, w, h int64, tags []string) (*axeResult, error) {
	ctx, cancel := chromedp.NewContext(alloc, chromedp.WithErrorf(quietErrorf))
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()

	url := fmt.Sprintf("%s/%s?a11y=%d", strings.TrimSuffix(base, "/"), page, time.Now().UnixNano())

	// The theme is set before the document loads, the same way the
	// site's own pre-paint script does it, so the page renders in the
	// theme being audited rather than switching after paint.
	setTheme := fmt.Sprintf(`
		try { localStorage.setItem('vox-theme', %q); } catch (e) {}
		document.documentElement.setAttribute('data-vox-theme', %q);`, theme, theme)

	// target-size is WCAG 2.2 SC 2.5.8 and ships *disabled* in axe-core,
	// so selecting the wcag22aa tag alone does not run it. Enabling it
	// by name is the only way that criterion is actually checked.
	runAxe := fmt.Sprintf(`axe.run(document, {
		runOnly: { type: 'tag', values: %s },
		rules: { 'target-size': { enabled: true } },
	}).then(r => JSON.stringify({
		violations: r.violations,
		// Every rule axe actually evaluated, whatever the outcome. The
		// caller asserts the rules it cares about are in here: a rule
		// that silently did not run reports no violations, which is
		// indistinguishable from passing.
		ran: [].concat(r.violations, r.passes, r.incomplete, r.inapplicable).map(x => x.id),
	}))`, mustJSON(tags))

	var raw string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(w, h),
		chromedp.Navigate(url),
		chromedp.Evaluate(setTheme, nil),
		chromedp.Reload(),
		chromedp.WaitVisible("vox-footer", chromedp.ByQuery),
		// Custom elements upgrade asynchronously; auditing before they
		// have rendered their shadow DOM would audit empty shells.
		chromedp.Evaluate(`(async () => {
			await customElements.whenDefined('vox-footer');
			await customElements.whenDefined('vox-header');
			await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)));
			return true;
		})()`, nil, awaitPromise),
		chromedp.Evaluate(axeJS, nil),
		chromedp.Evaluate(runAxe, &raw, awaitPromise),
	)
	if err != nil {
		return nil, err
	}

	var result axeResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("parsing axe output: %w", err)
	}
	return &result, nil
}

// report prints findings grouped by rule, worst impact first.
func report(findings map[string]*finding, checked int) error {
	fmt.Printf("Audited %d page renders (%d pages x %d themes x %d viewports)\n",
		checked, len(pages), len(themes), len(viewports))

	if len(findings) == 0 {
		fmt.Printf("\nNo WCAG violations found by axe-core.\n")
		fmt.Printf("Machine checks only - see marketing/README.md for what was reviewed by hand.\n")
		return nil
	}

	order := map[string]int{"critical": 0, "serious": 1, "moderate": 2, "minor": 3}
	var ids []string
	for id := range findings {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := findings[ids[i]], findings[ids[j]]
		if order[a.Impact] != order[b.Impact] {
			return order[a.Impact] < order[b.Impact]
		}
		return ids[i] < ids[j]
	})

	fmt.Printf("\n%d rule(s) violated:\n", len(findings))
	for _, id := range ids {
		f := findings[id]
		fmt.Printf("\n  [%s] %s\n    %s\n    %s\n", strings.ToUpper(f.Impact), f.Rule, f.Help, f.HelpURL)

		var targets []string
		for t := range f.Nodes {
			targets = append(targets, t)
		}
		sort.Strings(targets)
		for _, t := range targets {
			fmt.Printf("    element: %s\n", t)
			if s := f.Nodes[t][0]; s != "" {
				for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
					fmt.Printf("      %s\n", strings.TrimSpace(line))
				}
			}
		}

		var ctxs []string
		for c := range f.Contexts {
			ctxs = append(ctxs, c)
		}
		sort.Strings(ctxs)
		fmt.Printf("    seen on: %s\n", strings.Join(ctxs, ", "))
	}

	return fmt.Errorf("%d accessibility rule(s) violated", len(findings))
}

type finding struct {
	Rule     string
	Impact   string
	Help     string
	HelpURL  string
	Contexts map[string]bool
	Nodes    map[string][]string
}

type axeResult struct {
	Ran        []string `json:"ran"`
	Violations []struct {
		ID      string `json:"id"`
		Impact  string `json:"impact"`
		Help    string `json:"help"`
		HelpURL string `json:"helpUrl"`
		Nodes   []struct {
			Target         []any  `json:"target"`
			FailureSummary string `json:"failureSummary"`
		} `json:"nodes"`
	} `json:"violations"`
}

func loadAxe(path string) (string, error) {
	if path == "" {
		dir, err := repoRelative("marketing", "node_modules", "axe-core", "axe.min.js")
		if err != nil {
			return "", err
		}
		path = dir
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("cannot read axe-core at %s: %w\nRun `npm install` in marketing/", path, err)
	}
	return string(b), nil
}

// repoRelative resolves a path against the repository root, found by
// walking up for the marketing directory.
func repoRelative(parts ...string) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		if info, err := os.Stat(filepath.Join(dir, "marketing")); err == nil && info.IsDir() {
			return filepath.Join(append([]string{dir}, parts...)...), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find the repository root from %s", wd)
		}
		dir = parent
	}
}

func devtoolsURL(endpoint string) (string, error) {
	resp, err := http.Get(strings.TrimSuffix(endpoint, "/") + "/json/version")
	if err != nil {
		return "", fmt.Errorf("no headless browser at %s: %w\nStart it with `make screenshots-up`", endpoint, err)
	}
	defer resp.Body.Close()
	var v struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	return v.WebSocketDebuggerURL, nil
}

func join(target []any) string {
	var parts []string
	for _, t := range target {
		parts = append(parts, fmt.Sprint(t))
	}
	return strings.Join(parts, " >> ")
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// awaitPromise tells Evaluate to wait for the promise the expression
// returns, rather than handing back an unresolved handle.
func awaitPromise(p *runtime.EvaluateParams) *runtime.EvaluateParams {
	return p.WithAwaitPromise(true)
}
