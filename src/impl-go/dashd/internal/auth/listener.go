// TLS listener factory.
//
// AC-3 requires every dashd listener (REST, gRPC, admin, and any future
// addition) to be created via NewListener. PA-1 shipped a no-op that
// returned the same net.Listener net.Listen would; PD activates real
// TLS termination when AuthConfig.Mode != "none" and AuthConfig.TLS is
// populated.
//
// Modes:
//
//	none  -> plain net.Listen (unchanged)
//	token -> tls.Listen (TLS material required for the listener;
//	         per-request bearer-token check happens in the interceptor)
//	mtls  -> tls.Listen with ClientAuth=RequireAndVerifyClientCert
//	         using the configured CA bundle
//
// By forcing every caller through one factory, the PD-day TLS rollout
// is a single config change in main.go's wiring; no caller needs
// updating, no listener goes plaintext by accident.
package auth

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
)

// ListenerConfig is the per-listener subset of dashd's AuthConfig that
// the auth package needs to decide whether to wrap a listener in TLS.
//
// We deliberately accept a small purpose-built struct instead of
// internal/config.AuthConfig — that avoids a circular import between
// internal/auth and internal/config, and keeps the auth package
// dependency-free (PD's TLS implementation only needs crypto/tls).
type ListenerConfig struct {
	// Mode mirrors internal/config.AuthConfig.Mode: "none" | "token" | "mtls".
	Mode string

	// CertFile, KeyFile, CAFile, RequireClientCert mirror
	// internal/config.TLSConfig.
	CertFile          string
	KeyFile           string
	CAFile            string
	RequireClientCert bool
}

// NewListener returns a net.Listener bound to addr.
//
//	mode=none  -> plain net.Listen (today's behaviour)
//	mode=token -> tls.Listen when CertFile+KeyFile present, else plain
//	mode=mtls  -> tls.Listen with ClientAuth=RequireAndVerifyClientCert
//
// Token mode WITHOUT TLS material is intentional: operators may
// terminate TLS upstream (e.g. via an Envoy sidecar) and still want
// dashd to enforce bearer-token RBAC. We never silently turn off TLS
// when material IS supplied — that would be a foot-gun.
func NewListener(network, addr string, lc ListenerConfig) (net.Listener, error) {
	switch lc.Mode {
	case "", "none":
		return net.Listen(network, addr)

	case "token":
		if lc.CertFile == "" && lc.KeyFile == "" {
			// Operator opted out of in-process TLS for token mode.
			return net.Listen(network, addr)
		}
		cfg, err := buildTLSConfig(lc, false)
		if err != nil {
			return nil, err
		}
		return tls.Listen(network, addr, cfg)

	case "mtls":
		cfg, err := buildTLSConfig(lc, true)
		if err != nil {
			return nil, err
		}
		return tls.Listen(network, addr, cfg)

	default:
		return nil, fmt.Errorf("auth.NewListener: unsupported mode %q", lc.Mode)
	}
}

// buildTLSConfig assembles a *tls.Config from the listener config. When
// requireClient is true (mtls), CAFile is mandatory and ClientAuth is
// forced to RequireAndVerifyClientCert regardless of RequireClientCert
// (the validator already enforces this; we re-enforce here for defence
// in depth).
func buildTLSConfig(lc ListenerConfig, requireClient bool) (*tls.Config, error) {
	if lc.CertFile == "" || lc.KeyFile == "" {
		return nil, errors.New("auth.NewListener: cert_file and key_file are required for TLS modes")
	}
	cert, err := tls.LoadX509KeyPair(lc.CertFile, lc.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("auth.NewListener: load keypair: %w", err)
	}
	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	if requireClient || lc.RequireClientCert {
		if lc.CAFile == "" {
			return nil, errors.New("auth.NewListener: ca_file is required when client cert verification is enabled")
		}
		pool, err := loadCABundle(lc.CAFile)
		if err != nil {
			return nil, err
		}
		cfg.ClientCAs = pool
		if requireClient {
			cfg.ClientAuth = tls.RequireAndVerifyClientCert
		} else {
			cfg.ClientAuth = tls.VerifyClientCertIfGiven
		}
	}
	return cfg, nil
}

func loadCABundle(path string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("auth.NewListener: read ca: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("auth.NewListener: ca_file contains no PEM certificates")
	}
	return pool, nil
}
