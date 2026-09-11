package codemanager

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// webhookPushPayload is the subset of a git host's push webhook payload
// this package needs - matches GitHub's push event shape (ref, and the
// SHA the branch now points at). See design.md for why GitHub's
// convention is the v1 target.
type webhookPushPayload struct {
	Ref   string `json:"ref"`   // e.g. "refs/heads/production"
	After string `json:"after"` // the commit SHA the branch now points at
}

// verifyWebhookSignature checks header (GitHub's `X-Hub-Signature-256`
// value, "sha256=<hex hmac>") against an HMAC-SHA256 of body keyed by
// secret. Returns false for a missing/malformed/mismatched signature.
func verifyWebhookSignature(secret string, body []byte, header string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	given, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := mac.Sum(nil)

	return hmac.Equal(given, want)
}

// environmentFromRef extracts a branch name from a full ref
// ("refs/heads/production" -> "production"), the environment name g10k
// (and this package's Deployer) expects.
func environmentFromRef(ref string) (string, error) {
	const prefix = "refs/heads/"
	if !strings.HasPrefix(ref, prefix) {
		return "", fmt.Errorf("unsupported ref %q (expected %s<branch>)", ref, prefix)
	}
	branch := strings.TrimPrefix(ref, prefix)
	if branch == "" {
		return "", fmt.Errorf("empty branch name in ref %q", ref)
	}
	return branch, nil
}

func parseWebhookPayload(body []byte) (webhookPushPayload, error) {
	var p webhookPushPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return webhookPushPayload{}, fmt.Errorf("decode webhook payload: %w", err)
	}
	return p, nil
}
