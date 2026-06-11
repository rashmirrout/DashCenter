// Config types and validation for the dashd deployment mode + node identity.
//
// dashd ships as one Go binary with two deployment-topology personalities:
//
//	mode: controller       — dedicated x86 host(s) outside the DPU fleet;
//	                         leader election via etcd lease (Phase 2 PA-3)
//	mode: controllerless   — embedded on every DPU; gossip + raft + proxy
//	                         (Phase 2 PF)
//
// PA-1 parses both modes but only "controller" activates. "controllerless"
// fails at startup with a clean "not yet implemented" error so contributors
// can author future Phase-2 configs without breaking the build.
//
// Single-node dev (today) is mode="controller" with no HA elector — the
// leaderLoop from PA-0 uses NoneElector unchanged.
//
// Locked decisions (specs/Impl-Plan/impl-phases.md § Frozen knob, K1..K8):
//
//	K1  field name              "mode" (not "topology" / "cluster.mode")
//	K2  allowed values          controller | controllerless
//	K3  default                 controller
//	K5  placement               mode + node_id are top-level fields
package config

import (
	"errors"
	"fmt"
	"os"
)

// ModeController and ModeControllerless name the two deployment personalities.
const (
	ModeController     = "controller"
	ModeControllerless = "controllerless"
)

// defaultMode is what fresh installs and pre-PA-1 dashd.yaml files
// resolve to: the controller-mode personality, which today (without an
// elector configured) behaves identically to the single-node Phase 1
// deployment via NoneElector.
const defaultMode = ModeController

// defaultNodeID returns this process's identity for HA — used as the
// etcd-lease key (controller mode, PA-3) and the raft node id
// (controllerless mode, PF). When unset, dashd derives it from the OS
// hostname. Falls back to "dashd-local" only when hostname lookup itself
// fails, which is exceptionally rare.
func defaultNodeID() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "dashd-local"
}

// applyModeDefaults sets cfg.Mode + cfg.NodeID to their defaults when
// unset. Called before validation; idempotent.
func applyModeDefaults(c *Config) {
	if c.Mode == "" {
		c.Mode = defaultMode
	}
	if c.NodeID == "" {
		c.NodeID = defaultNodeID()
	}
}

// validateMode returns nil on a valid Mode + NodeID. PA-1 rejects
// Mode="controllerless" cleanly until PF lands; this means contributors
// can ship configs with `mode: controllerless` without the YAML parser
// silently treating it as the controller default.
func validateMode(mode, nodeID string) error {
	var errs []error

	switch mode {
	case ModeController:
		// always valid
	case ModeControllerless:
		errs = append(errs, errors.New(`mode="controllerless" is not yet implemented (planned for Phase 2 PF); use "controller" until then`))
	default:
		errs = append(errs, fmt.Errorf("mode: unsupported value %q (allowed: controller, controllerless)", mode))
	}

	if nodeID == "" {
		errs = append(errs, errors.New("node_id is required (defaults to OS hostname when unset)"))
	}

	return errors.Join(errs...)
}
