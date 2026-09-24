package nodetransport

import (
	"crypto/tls"

	"github.com/nats-io/nats-server/v2/server"
)

// authorize maps a node connection's already-verified client certificate
// (Subject.CommonName) to a NATS user restricted to that node's own
// subjects - the "a node can only ever be addressed as itself" property.
// It is the messaging.NodeListener's Authorize hook; the messaging
// package places the user in messaging.AccountNodes.
//
// By the time this runs, the TLS handshake has required a client
// certificate, verified its chain against the configured CA, and checked
// it against the current CRL (see crl.go). The revocation check is
// repeated here as defence in depth; the primary enforcement point for
// isolation is the Permissions assigned below - without them any
// CA-signed connection would get NATS's default unrestricted access
// within the account.
func (l *Listener) authorize(state *tls.ConnectionState) (*server.User, bool) {
	if len(state.PeerCertificates) == 0 {
		return nil, false
	}
	leaf := state.PeerCertificates[0]
	certname := leaf.Subject.CommonName
	if certname == "" {
		return nil, false
	}
	if l.crl.revoked(leaf) {
		return nil, false
	}
	l.sessions.record(certname, leaf)

	prefix := nodePrefix(certname) + ">"
	return &server.User{
		Username: certname,
		Permissions: &server.Permissions{
			// A node publishes nothing of its own. Its only outbound
			// messages are replies, which Response allows: exactly one
			// reply to each request actually delivered to it, within
			// ResponseWindow, whatever reply subject the request named.
			// That is what lets the console use ordinary request/reply
			// (a shared _INBOX namespace) without any node being able
			// to publish into that namespace at will.
			Publish:   &server.SubjectPermission{Deny: []string{">"}},
			Subscribe: &server.SubjectPermission{Allow: []string{prefix}},
			Response:  &server.ResponsePermission{MaxMsgs: 1, Expires: ResponseWindow},
		},
	}, true
}
