// Config types and validation for the HA (high availability) block.
//
// dashd has two HA personalities, one per deployment mode:
//
//	mode: controller      → ha.controller.elector  (etcd lease)        PA-3
//	mode: controllerless  → ha.controllerless.{gossip,raft}            PF
//
// Only the block matching cfg.Mode may be non-zero — populating the wrong
// block is a config error caught at startup. This makes the YAML
// self-documenting: a reader sees one populated block and knows the
// topology.
//
// Locked decisions (specs/Impl-Plan/impl-phases.md § Frozen knob):
//
//	K6   nested under ha.{controller,controllerless}
//	K7   storage.backend=raft reuses ha.controllerless.raft transport
//	D3   etcd lease TTL default = 15s
package config

import (
	"errors"
	"fmt"
	"time"
)

// HAConfig holds both mode-specific HA blocks. dashd enforces that only
// the block matching cfg.Mode is non-zero.
type HAConfig struct {
	Controller     ControllerHAConfig     `yaml:"controller"`
	Controllerless ControllerlessHAConfig `yaml:"controllerless"`
}

// ControllerHAConfig is the HA block for mode="controller".
//
// Only Elector is honoured today. When Elector.Backend == "" or "none",
// dashd uses the PA-0 NoneElector (always-leader). When Elector.Backend
// == "etcd", PA-3's EtcdElector takes over and dashd campaigns for the
// lease against the configured endpoints.
type ControllerHAConfig struct {
	Elector ElectorConfig `yaml:"elector"`
}

// ElectorBackend enumerates the supported leader-election backends.
const (
	ElectorBackendNone = "none"
	ElectorBackendEtcd = "etcd"
)

// ElectorConfig parameterises a leader-election backend. PA-1 supports
// only "none" (the always-leader NoneElector from PA-0). PA-3 adds "etcd".
type ElectorConfig struct {
	Backend     string        `yaml:"backend"`      // none | etcd
	Endpoints   []string      `yaml:"endpoints"`    // etcd peer URLs
	LeaseTTL    time.Duration `yaml:"lease_ttl"`    // default 15s (D3 locked)
	LeaderKey   string        `yaml:"leader_key"`   // default /dashd/leader
	DialTimeout time.Duration `yaml:"dial_timeout"` // default 5s
	TLS         TLSConfig     `yaml:"tls"`
}

// ControllerlessHAConfig is the HA block for mode="controllerless" (PF).
// Parsed today but rejected at startup until PF-3 lands.
type ControllerlessHAConfig struct {
	BindAddr      string         `yaml:"bind_addr"`
	AdvertiseAddr string         `yaml:"advertise_addr"`
	Gossip        GossipConfig   `yaml:"gossip"`
	Raft          RaftConfig     `yaml:"raft"`
}

// GossipConfig parameterises the SWIM membership overlay (PF-1).
type GossipConfig struct {
	Port              int           `yaml:"port"`
	Seeds             []string      `yaml:"seeds"`
	EncryptionKeyFile string        `yaml:"encryption_key_file"`
	ProbeInterval     time.Duration `yaml:"probe_interval"`
}

// RaftConfig parameterises the Raft consensus log (PF-2). Reused by
// storage.backend=raft (PF-2 store backend) — no separate transport.
type RaftConfig struct {
	Port              int           `yaml:"port"`
	DataDir           string        `yaml:"data_dir"`
	SnapshotInterval  time.Duration `yaml:"snapshot_interval"`
	SnapshotThreshold int           `yaml:"snapshot_threshold"`
	HeartbeatTimeout  time.Duration `yaml:"heartbeat_timeout"`
	ElectionTimeout   time.Duration `yaml:"election_timeout"`
}

// defaultHAConfig returns the zero-friction default: NoneElector
// (Backend == "none") under controller mode. This matches the PA-0
// single-node behaviour exactly.
func defaultHAConfig() HAConfig {
	return HAConfig{
		Controller: ControllerHAConfig{
			Elector: ElectorConfig{
				Backend:     ElectorBackendNone,
				LeaseTTL:    15 * time.Second,
				LeaderKey:   "/dashd/leader",
				DialTimeout: 5 * time.Second,
			},
		},
	}
}

// applyHADefaults fills in zero-value fields from the default. Called
// before validation; idempotent.
func applyHADefaults(c *Config) {
	d := defaultHAConfig()
	if c.HA.Controller.Elector.Backend == "" {
		c.HA.Controller.Elector.Backend = d.Controller.Elector.Backend
	}
	if c.HA.Controller.Elector.LeaseTTL == 0 {
		c.HA.Controller.Elector.LeaseTTL = d.Controller.Elector.LeaseTTL
	}
	if c.HA.Controller.Elector.LeaderKey == "" {
		c.HA.Controller.Elector.LeaderKey = d.Controller.Elector.LeaderKey
	}
	if c.HA.Controller.Elector.DialTimeout == 0 {
		c.HA.Controller.Elector.DialTimeout = d.Controller.Elector.DialTimeout
	}
}

// controllerlessHAIsSet reports whether any field in the controllerless
// block has been populated. Used by validateHA to reject mode/block
// mismatches.
func controllerlessHAIsSet(c ControllerlessHAConfig) bool {
	if c.BindAddr != "" || c.AdvertiseAddr != "" {
		return true
	}
	g := c.Gossip
	if g.Port != 0 || len(g.Seeds) > 0 || g.EncryptionKeyFile != "" || g.ProbeInterval != 0 {
		return true
	}
	r := c.Raft
	if r.Port != 0 || r.DataDir != "" || r.SnapshotInterval != 0 || r.SnapshotThreshold != 0 || r.HeartbeatTimeout != 0 || r.ElectionTimeout != 0 {
		return true
	}
	return false
}

// controllerHAIsSet reports whether the controller block has been
// populated with anything beyond the defaults. We check Endpoints and
// non-default Backend rather than the auto-applied defaults so that an
// operator who never set the controller block in YAML does not get
// flagged as "controller block set under mode=controllerless".
func controllerHAIsSet(c ControllerHAConfig) bool {
	e := c.Elector
	if len(e.Endpoints) > 0 {
		return true
	}
	if e.Backend != "" && e.Backend != ElectorBackendNone {
		return true
	}
	if e.TLS.CertFile != "" || e.TLS.KeyFile != "" || e.TLS.CAFile != "" || e.TLS.RequireClientCert {
		return true
	}
	return false
}

// validateHA returns nil on a valid HAConfig. Today (PA-1) it rejects:
//   - both blocks populated;
//   - mode/block mismatch (e.g. controllerless block under mode=controller);
//   - elector.backend=etcd (planned PA-3);
//   - any populated controllerless field (planned PF).
func validateHA(mode string, ha HAConfig) error {
	var errs []error

	ctrlSet := controllerHAIsSet(ha.Controller)
	ctrllSet := controllerlessHAIsSet(ha.Controllerless)

	switch mode {
	case ModeController:
		if ctrllSet {
			errs = append(errs, errors.New(`ha.controllerless is set but mode="controller"; clear ha.controllerless or change mode`))
		}
		switch ha.Controller.Elector.Backend {
		case ElectorBackendNone:
			// always valid
		case ElectorBackendEtcd:
			// PA-3: EtcdElector is now wired through cmd/dashd/main.go.
			if len(ha.Controller.Elector.Endpoints) == 0 {
				errs = append(errs, errors.New(`ha.controller.elector.backend="etcd" requires ha.controller.elector.endpoints`))
			}
			for i, ep := range ha.Controller.Elector.Endpoints {
				if ep == "" {
					errs = append(errs, fmt.Errorf("ha.controller.elector.endpoints[%d] is empty", i))
				}
			}
			if (ha.Controller.Elector.TLS.CertFile == "") != (ha.Controller.Elector.TLS.KeyFile == "") {
				errs = append(errs, errors.New("ha.controller.elector.tls.cert_file and ha.controller.elector.tls.key_file must be set together"))
			}
		default:
			errs = append(errs, fmt.Errorf("ha.controller.elector.backend: unsupported value %q (allowed: none, etcd)", ha.Controller.Elector.Backend))
		}
		if ha.Controller.Elector.LeaseTTL < 0 {
			errs = append(errs, errors.New("ha.controller.elector.lease_ttl must be >= 0"))
		}

	case ModeControllerless:
		// mode itself already rejected by validateMode with a clean message,
		// so we don't pile on here — but we still call out the wrong-block
		// case for the same hygiene reason.
		if ctrlSet {
			errs = append(errs, errors.New(`ha.controller is set but mode="controllerless"; clear ha.controller or change mode`))
		}
	}

	return errors.Join(errs...)
}
