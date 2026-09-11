package nodetransport

import (
	"github.com/nats-io/nats-server/v2/server"
)

// authenticator implements server.Authentication (wired in as
// Options.CustomClientAuthentication) - it maps a connecting node's
// already-TLS-verified client certificate (Subject.CommonName) to a NATS
// identity restricted to only that node's own subjects, giving the
// "a node can only ever be addressed as itself" property, which NATS has
// per-node isolation NATS has no equivalent of on its own (a broker
// session a certname-keyed identity for free).
//
// ns is set once, between server.NewServer and ns.Start (see server.go) -
// Check is only ever invoked for an actual client connection, which can't
// happen until after that point, so this has no initialization race.
type authenticator struct {
	ns *server.Server
}

// Check implements server.Authentication. By the time this runs, the TLS
// handshake has already required and verified a client certificate signed
// by the configured CA (see loadServerTLSConfig) - this is a defensive
// check (defence in depth), not
// the primary enforcement point. The primary enforcement point is the
// Permissions assigned below, scoping this connection to only its own
// node.<certname>.* subjects (see subjects.go) - without this, any
// CA-signed connection would get NATS's default unrestricted access.
//
// This also runs for the console's own internal, in-process client(s)
// (see server.go's and registry.go's use of nats.InProcessServer) - those
// connections never go through the TLS listener at all, so they have no
// TLS connection state. Since every *external* connection is forced
// through the mTLS listener (TLSConfig.ClientAuth:
// RequireAndVerifyClientCert already rejects an external connection with
// no valid client cert before Check ever runs), a nil TLS state here can
// only be one of the console's own trusted internal connections - allow
// it, with the system account specifically for the internal client
// registry.go uses to consume $SYS.> connect/disconnect events (a
// regular connection's default global-account membership can't see
// those), and no restriction otherwise.
func (a *authenticator) Check(c server.ClientAuthentication) bool {
	state := c.GetTLSConnectionState()
	if state == nil {
		if c.GetOpts().Name == internalAdminConnName {
			c.RegisterUser(&server.User{Username: internalAdminConnName, Account: a.ns.SystemAccount()})
		}
		return true
	}
	if len(state.PeerCertificates) == 0 {
		return false
	}
	certname := state.PeerCertificates[0].Subject.CommonName
	if certname == "" {
		return false
	}

	subject := nodePrefix(certname) + ">"
	c.RegisterUser(&server.User{
		Username: certname,
		Permissions: &server.Permissions{
			Publish:   &server.SubjectPermission{Allow: []string{subject}},
			Subscribe: &server.SubjectPermission{Allow: []string{subject}},
		},
	})
	return true
}
