// TLS listener factory.
//
// AC-3 requires every dashd listener (REST, gRPC, admin, and any future
// addition) to be created via NewListener. PA-1 ships a no-op that
// returns the same net.Listener net.Listen would. PD swaps this
// implementation to wrap the listener in a tls.Listener when
// AuthConfig.Mode != "none" and AuthConfig.TLS is populated.
//
// By forcing every caller through one factory today, the PD-day TLS
// rollout is a one-file change — no caller needs updating, no listener
// goes plaintext by accident.
package auth

import (
	"errors"
	"net"
)

// ListenerConfig is the per-listener subset of dashd's AuthConfig that
// the auth package needs to decide whether to wrap a listener in TLS.
//
// We deliberately accept a small purpose-built struct instead of
// internal/config.AuthConfig — that avoids a circular import between
// internal/auth and internal/config, and keeps the auth package
// dependency-free (PD's TLS implementation will only need crypto/tls).
type ListenerConfig struct {
	// Mode mirrors internal/config.AuthConfig.Mode: "none" | "token" | "mtls".
	// PA-1 only honours "none" (which means "do not wrap the listener").
	Mode string

	// CertFile, KeyFile, CAFile, RequireClientCert mirror
	// internal/config.TLSConfig. PA-1 ignores them entirely; PD reads
	// them to build the *tls.Config wrapping the listener.
	CertFile          string
	KeyFile           string
	CAFile            string
	RequireClientCert bool
}

// NewListener returns a net.Listener bound to addr. PA-1 always returns
// a plain TCP listener — auth.mode=none means no TLS wrapping. PD will
// extend this to:
//
//	mode=none  → plain net.Listen (today's behaviour)
//	mode=token → tls.Listen if TLS material present, otherwise plain
//	mode=mtls  → tls.Listen with ClientAuth=RequireAndVerifyClientCert
//
// Returning a plain listener under "none" is the explicit Phase 1
// semantics: dashd is reachable over plaintext HTTP/gRPC, identical to
// today.
func NewListener(network, addr string, lc ListenerConfig) (net.Listener, error) {
	switch lc.Mode {
	case "", "none":
		// Default and explicit "off" both yield a plain listener.
		return net.Listen(network, addr)

	case "token", "mtls":
		// PA-1 should never reach here — the config validator rejects
		// these modes at startup with a clean "not yet implemented"
		// error before any listener is opened. Treating the case as a
		// programmer error rather than silently downgrading prevents a
		// future PD-day refactor from accidentally producing plaintext
		// listeners.
		return nil, errors.New("auth.NewListener: TLS modes not yet implemented (Phase 2 PD); this code path should be unreachable under PA-1 validation")

	default:
		return nil, errors.New("auth.NewListener: unsupported mode " + lc.Mode)
	}
}
