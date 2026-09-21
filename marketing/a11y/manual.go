package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// This file holds the checks axe does not make. They are real WCAG
// success criteria that a rule engine cannot evaluate from the DOM
// alone, so they are measured by rendering the page and looking at the
// result.

// reflowWidth is the viewport WCAG 1.4.10 (Reflow, AA) names: content
// must not require horizontal scrolling at 320 CSS pixels.
const reflowWidth = 320

// textSpacingCSS is the override WCAG 1.4.12 (Text Spacing, AA)
// requires content to survive. Applying it must not cause loss of
// content or function - in practice, must not clip or overlap text.
const textSpacingCSS = `* {
  line-height: 1.5 !important;
  letter-spacing: 0.12em !important;
  word-spacing: 0.16em !important;
}
p { margin-bottom: 2em !important; }`

// checkReflow reports pages that scroll horizontally at 320px.
func checkReflow(alloc context.Context, base string, pages []string, themes []string) ([]string, error) {
	var problems []string

	for _, theme := range themes {
		for _, page := range pages {
			ctx, cancel := chromedp.NewContext(alloc, chromedp.WithErrorf(quietErrorf))
			tctx, tcancel := context.WithTimeout(ctx, 45*time.Second)

			var res struct {
				ScrollW int    `json:"scrollW"`
				ClientW int    `json:"clientW"`
				Widest  string `json:"widest"`
			}
			err := chromedp.Run(tctx,
				chromedp.EmulateViewport(reflowWidth, 800),
				chromedp.Navigate(fmt.Sprintf("%s/%s?reflow=%d", strings.TrimSuffix(base, "/"), page, time.Now().UnixNano())),
				chromedp.Evaluate(themeScript(theme), nil),
				chromedp.Reload(),
				chromedp.WaitVisible("vox-footer", chromedp.ByQuery),
				chromedp.Sleep(500*time.Millisecond),
				// Name the widest offending element, so a failure says
				// what to fix rather than only that something is wrong.
				chromedp.Evaluate(`(() => {
					const doc = document.documentElement;
					let widest = '', max = 0;
					for (const el of document.querySelectorAll('body *')) {
						const r = el.getBoundingClientRect();
						if (r.right > doc.clientWidth + 1 && r.width > max) {
							max = r.width;
							widest = el.tagName.toLowerCase() +
								(el.className && typeof el.className === 'string' ? '.' + el.className.trim().split(/\s+/).join('.') : '');
						}
					}
					return { scrollW: doc.scrollWidth, clientW: doc.clientWidth, widest };
				})()`, &res),
			)
			tcancel()
			cancel()
			if err != nil {
				return nil, fmt.Errorf("%s (%s): %w", page, theme, err)
			}

			if res.ScrollW > res.ClientW+1 {
				problems = append(problems, fmt.Sprintf(
					"%s (%s): scrolls horizontally at %dpx - content is %dpx wide, widest offender %s",
					page, theme, reflowWidth, res.ScrollW, res.Widest))
			}
		}
	}
	return problems, nil
}

// checkTextSpacing reports pages where the WCAG text-spacing overrides
// cause text to be clipped by its own container.
func checkTextSpacing(alloc context.Context, base string, pages []string) ([]string, error) {
	var problems []string

	for _, page := range pages {
		ctx, cancel := chromedp.NewContext(alloc, chromedp.WithErrorf(quietErrorf))
		tctx, tcancel := context.WithTimeout(ctx, 45*time.Second)

		var clipped []string
		err := chromedp.Run(tctx,
			chromedp.EmulateViewport(1440, 900),
			chromedp.Navigate(fmt.Sprintf("%s/%s?spacing=%d", strings.TrimSuffix(base, "/"), page, time.Now().UnixNano())),
			chromedp.WaitVisible("vox-footer", chromedp.ByQuery),
			chromedp.Evaluate(fmt.Sprintf(`(() => {
				const s = document.createElement('style');
				s.textContent = %q;
				document.head.appendChild(s);
			})()`, textSpacingCSS), nil),
			chromedp.Sleep(600*time.Millisecond),
			chromedp.Evaluate(`(() => {
				const out = [];
				for (const el of document.querySelectorAll('p, h1, h2, h3, li, figcaption, span')) {
					// Overflow is only a failure when the element clips it.
					const style = getComputedStyle(el);
					if (style.overflow === 'visible' && style.overflowY === 'visible') continue;
					if (el.scrollHeight > el.clientHeight + 2) {
						out.push(el.tagName.toLowerCase() + ': ' + (el.textContent || '').trim().slice(0, 40));
					}
				}
				return out;
			})()`, &clipped),
		)
		tcancel()
		cancel()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", page, err)
		}

		for _, c := range clipped {
			problems = append(problems, fmt.Sprintf("%s: text clipped under WCAG 1.4.12 spacing - %s", page, c))
		}
	}
	return problems, nil
}

// checkSkipLink verifies the first focusable element is a skip link that
// targets a real element and becomes visible when focused.
func checkSkipLink(alloc context.Context, base string, pages []string) ([]string, error) {
	var problems []string

	for _, page := range pages {
		ctx, cancel := chromedp.NewContext(alloc, chromedp.WithErrorf(quietErrorf))
		tctx, tcancel := context.WithTimeout(ctx, 45*time.Second)

		var res struct {
			Found          bool   `json:"found"`
			TargetsReal    bool   `json:"targetsReal"`
			VisibleOnFocus bool   `json:"visibleOnFocus"`
			Text           string `json:"text"`
		}
		err := chromedp.Run(tctx,
			chromedp.EmulateViewport(1440, 900),
			chromedp.Navigate(fmt.Sprintf("%s/%s?skip=%d", strings.TrimSuffix(base, "/"), page, time.Now().UnixNano())),
			chromedp.WaitVisible("vox-footer", chromedp.ByQuery),
			chromedp.Evaluate(`(() => {
				const link = document.querySelector('a.skip-link');
				if (!link) return { found: false };
				const href = link.getAttribute('href') || '';
				const target = href.startsWith('#') ? document.getElementById(href.slice(1)) : null;
				const before = link.getBoundingClientRect().top;
				link.focus();
				const after = link.getBoundingClientRect().top;
				return {
					found: true,
					targetsReal: !!target,
					visibleOnFocus: after > before && after >= 0,
					text: (link.textContent || '').trim(),
				};
			})()`, &res),
		)
		tcancel()
		cancel()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", page, err)
		}

		switch {
		case !res.Found:
			problems = append(problems, page+": no skip link")
		case !res.TargetsReal:
			problems = append(problems, page+": skip link points at an element that does not exist")
		case !res.VisibleOnFocus:
			problems = append(problems, page+": skip link does not become visible when focused")
		}
	}
	return problems, nil
}

func themeScript(theme string) string {
	return fmt.Sprintf(`
		try { localStorage.setItem('vox-theme', %q); } catch (e) {}
		document.documentElement.setAttribute('data-vox-theme', %q);`, theme, theme)
}
