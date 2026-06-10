// Config types and validation for the auth knob.
//
// Auth is a Phase-2 milestone (PD). Phase 1B / PA-1 ships only the
// parsing + validation shape: AuthConfig.Mode defaults to "none" (which is
// the today-equivalent plaintext behaviour), and "token" / "mtls" parse
// successfully but are rejected at startup with a clean "not yet
// implemented" error. This lets contributors add auth-aware Phase-2 code
// from day one without auth itself being live.
//
// Locked decisions (see specs/Impl-Plan/impl-phases.md § Auth contract):
//
//	D11  knob name + default        auth.mode: none|token|mtls; default "none"
//	D12  none semantics             every interceptor no-op; integration unchanged
//	D13  missing≡bad bearer         both → UNAUTHENTICATED
//	D14  unmapped mTLS CN           PERMISSION_DENIED
//	D15  startup banner             WARN when auth.mode=none
package config

import (
	"errors"
	"fmt"
)

// AuthMode enumerates the supported auth postures.
const (
	AuthModeNone  = "none"
	AuthModeToken = "token"
	AuthModeMTLS  = "mtls"
)

// AuthRoleViewer, AuthRoleOperator, AuthRoleAdmin name the built-in RBAC
// roles. Operators may override the per-role RPC permission list via
// Auth.Roles, but the role names themselves are not configurable.
const (
	AuthRoleViewer   = "viewer"
	AuthRoleOperator = "operator"
	AuthRoleAdmin    = "admin"
)

// AuthConfig describes the auth posture for THIS dashd process.
//
// The zero value (all fields empty) is treated as Mode="none" after
// applyAuthDefaults runs — i.e. auth disabled, identical to today's
// plaintext behaviour. This preserves backwards compatibility with every
// existing dashd.yaml that pre-dates PA-1.
type AuthConfig struct {
	Mode   string              `yaml:"mode"`   // none | token | mtls
	TLS    TLSConfig           `yaml:"tls"`    // TLS material; required when mode != none and you want TLS termination
	Tokens []TokenEntry        `yaml:"tokens"` // bearer tokens; used only when Mode=="token"
	Roles  map[string][]string `yaml:"roles"`  // optional override of built-in role→RPC map; nil means "use defaults"
}

// TLSConfig holds paths to TLS material on disk. dashd reads these once at
// startup; cert hot-rotation is a PD-late enhancement.
//
// When Mode=="mtls", CAFile is required and RequireClientCert is forced
// to true regardless of the YAML value. When Mode=="token", TLS material
// is optional (the operator may run plaintext-over-trusted-network or
// terminate TLS in front of dashd).
type TLSConfig struct {
	CertFile          string `yaml:"cert_file"`
	KeyFile           string `yaml:"key_file"`
	CAFile            string `yaml:"ca_file"`
	RequireClientCert bool   `yaml:"require_client_cert"`
}

// TokenEntry maps a bearer token to a role and a human-readable name.
// The Name field is purely for audit-log readability (PD-late); it is
// never matched against the wire.
type TokenEntry struct {
	Token string `yaml:"token"`
	Role  string `yaml:"role"`
	Name  string `yaml:"name"`
}

// defaultAuthConfig returns the zero-friction default: mode=none, no
// tokens, no TLS, no role overrides. This is what fresh installs get and
// what every pre-PA-1 dashd.yaml resolves to.
func defaultAuthConfig() AuthConfig {
	return AuthConfig{Mode: AuthModeNone}
}

// applyAuthDefaults fills in zero-value fields from the default.
func applyAuthDefaults(a *AuthConfig) {
	if a.Mode == "" {
		a.Mode = AuthModeNone
	}
}

// validateAuth returns nil on a valid AuthConfig. Today (PA-1) it rejects
// Mode="token" and Mode="mtls" with "not yet implemented" so that
// contributors writing those configs get a clean error instead of silent
// no-op behaviour. PD will replace those rejections with live behaviour.
func validateAuth(a AuthConfig) error {
	var errs []error

	switch a.Mode {
	case AuthModeNone:
		// always valid

	case AuthModeToken:
		errs = append(errs, errors.New(`auth.mode="token" is not yet implemented (planned for Phase 2 PD); use "none" until then`))
		if len(a.Tokens) == 0 {
			errs = append(errs, errors.New(`auth.mode="token" requires at least one entry in auth.tokens`))
		}
		for i, t := range a.Tokens {
			if t.Token == "" {
				errs = append(errs, fmt.Errorf("auth.tokens[%d].token is required", i))
			}
			if !validAuthRole(t.Role) {
				errs = append(errs, fmt.Errorf("auth.tokens[%d].role: unsupported value %q (allowed: viewer, operator, admin)", i, t.Role))
			}
		}

	case AuthModeMTLS:
		errs = append(errs, errors.New(`auth.mode="mtls" is not yet implemented (planned for Phase 2 PD); use "none" until then`))
		if a.TLS.CAFile == "" {
			errs = append(errs, errors.New(`auth.mode="mtls" requires auth.tls.ca_file`))
		}

	default:
		errs = append(errs, fmt.Errorf("auth.mode: unsupported value %q (allowed: none, token, mtls)", a.Mode))
	}

	if a.Mode != AuthModeNone {
		if (a.TLS.CertFile == "") != (a.TLS.KeyFile == "") {
			errs = append(errs, errors.New("auth.tls.cert_file and auth.tls.key_file must be set together"))
		}
	}

	for role := range a.Roles {
		if !validAuthRole(role) {
			errs = append(errs, fmt.Errorf("auth.roles: unsupported role %q (allowed: viewer, operator, admin)", role))
		}
	}

	return errors.Join(errs...)
}

// validAuthRole reports whether role is one of the three built-in roles.
func validAuthRole(role string) bool {
	switch role {
	case AuthRoleViewer, AuthRoleOperator, AuthRoleAdmin:
		return true
	default:
		return false
	}
}
