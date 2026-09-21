package codemanager

import "net/url"

// redactedUserinfo is what replaces a remote's credentials. A fixed
// marker rather than removing the userinfo entirely: an operator
// looking at the page should be able to see that this remote carries
// an embedded credential at all, which is itself worth knowing.
//
// Only unreserved URL characters, so url.String() reproduces it
// literally - a marker of "***" comes back out as "%2A%2A%2A", which
// reads like corruption rather than redaction.
const redactedUserinfo = "REDACTED"

// redactRemote returns remote with any embedded credentials replaced,
// preserving enough to identify the repository. An HTTPS remote can
// carry a token in its userinfo
// (https://x-access-token:ghp_xxx@github.com/org/repo.git), and the
// repository overview is visible to every code:read user.
//
// Parsed as a URL rather than matched with a regular expression: a
// regex over the raw string risks either missing an unanticipated shape
// or mangling a legitimate one, whereas url.Parse already knows where
// userinfo ends.
//
// A remote that does not parse as a URL is returned unchanged. That is
// safe rather than lax: the SCP-like form git uses
// (git@github.com:org/repo.git) has no password field, so its
// "user@host" carries an account name and no secret. Returning it
// unchanged also avoids mangling a perfectly ordinary remote into
// something an operator cannot recognise.
func redactRemote(remote string) string {
	parsed, err := url.Parse(remote)
	if err != nil || parsed.User == nil {
		return remote
	}
	parsed.User = url.User(redactedUserinfo)
	return parsed.String()
}
