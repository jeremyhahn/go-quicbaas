package fleet

import (
	"errors"
	"fmt"
	"os"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// DefaultVolumeStagingDir is where the node mounts filesystem volumes before
// bind-mounting them into containers.
const DefaultVolumeStagingDir = "/run/quicbaas/volumes"

// VolumeConfig selects the go-blockstore provider the node agent attaches
// workload volumes with.
type VolumeConfig struct {
	// Provider names the go-blockstore provider: "local". "drbd" is refused
	// until launches promote and demote its volumes. Empty disables volumes
	// on this node.
	Provider string `json:"provider,omitempty" yaml:"provider,omitempty"`

	// Settings are the provider's snake_case settings.
	Settings map[string]string `json:"settings,omitempty" yaml:"settings,omitempty"`

	// NodeID is this node's identity to the attacher and, for the local
	// provider, to the provider (as its node_id setting). Empty means
	// settings.node_id, then the host name. settings.node_id must not differ.
	NodeID string `json:"node_id,omitempty" yaml:"node_id,omitempty"`

	// StagingDir is the absolute directory filesystem volumes are mounted
	// under on the host. Empty means DefaultVolumeStagingDir.
	StagingDir string `json:"staging_dir,omitempty" yaml:"staging_dir,omitempty"`
}

// Enabled reports whether a provider is configured.
func (v VolumeConfig) Enabled() bool { return v.Provider != "" }

// newVolumeProvider is the provider constructor; tests replace it.
var newVolumeProvider = orchestration.NewVolumeProvider

// hostname is the default node ID source; tests replace it.
var hostname = os.Hostname

// WrapVolumeRuntime returns the node's launch runtime: inner itself when no
// volume provider is configured (a spec that claims volumes is then refused
// by ContainerSpec.Validate), otherwise an orchestration.VolumeRuntime that
// attaches each launch's volumes before inner runs it and releases them on
// stop, remove and a failed start.
func WrapVolumeRuntime(cfg VolumeConfig, inner orchestration.ContainerRuntime) (orchestration.ContainerRuntime, error) {
	if !cfg.Enabled() || inner == nil {
		return inner, nil
	}
	if cfg.Provider == "drbd" {
		// A drbd volume is Secondary after Provision and Attach, and Mount
		// needs Primary: until a launch promotes and demotes, refuse it.
		return nil, fmt.Errorf("%w: fleet volumes: the drbd provider needs a launch that promotes and demotes its volumes, which the node agent does not do yet", orchestration.ErrVolumeUnsupported)
	}
	settingsNode := cfg.Settings["node_id"]
	if cfg.NodeID != "" && settingsNode != "" && settingsNode != cfg.NodeID {
		return nil, fmt.Errorf("%w: fleet volumes: node_id %q and settings.node_id %q differ",
			orchestration.ErrVolumeInvalidArgument, cfg.NodeID, settingsNode)
	}
	node := cfg.NodeID
	if node == "" {
		node = settingsNode
	}
	if node == "" {
		h, err := hostname()
		if err != nil {
			return nil, fmt.Errorf("fleet volumes: no node_id and no host name: %w", err)
		}
		if h == "" {
			return nil, errors.New("fleet volumes: no node_id and the host name is empty")
		}
		node = h
	}
	staging := cfg.StagingDir
	if staging == "" {
		staging = DefaultVolumeStagingDir
	}
	settings := cfg.Settings
	if cfg.Provider == "local" {
		// The local provider refuses Attach for any node but its own, which it
		// otherwise takes from the host name: give it the attacher's identity.
		settings = make(map[string]string, len(cfg.Settings)+1)
		for k, v := range cfg.Settings {
			settings[k] = v
		}
		settings["node_id"] = node
	}
	provider, err := newVolumeProvider(cfg.Provider, settings)
	if err != nil {
		return nil, fmt.Errorf("fleet volumes: %w", err)
	}
	attacher, err := orchestration.NewVolumeAttacher(provider, orchestration.VolumeNodeID(node), staging)
	if err != nil {
		return nil, fmt.Errorf("fleet volumes: %w", err)
	}
	return orchestration.NewVolumeRuntime(inner, attacher)
}
