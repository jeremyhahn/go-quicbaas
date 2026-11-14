package fleet

import (
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
	// Provider names the go-blockstore provider ("local", or "drbd" in a
	// build with the drbd tag). Empty disables volumes on this node.
	Provider string `json:"provider,omitempty" yaml:"provider,omitempty"`

	// Settings are the provider's snake_case settings.
	Settings map[string]string `json:"settings,omitempty" yaml:"settings,omitempty"`

	// NodeID is this node's identity to the provider. Empty means the host
	// name.
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
	node := cfg.NodeID
	if node == "" {
		h, err := hostname()
		if err != nil || h == "" {
			return nil, fmt.Errorf("fleet volumes: no node_id and no host name: %v", err)
		}
		node = h
	}
	staging := cfg.StagingDir
	if staging == "" {
		staging = DefaultVolumeStagingDir
	}
	provider, err := newVolumeProvider(cfg.Provider, cfg.Settings)
	if err != nil {
		return nil, fmt.Errorf("fleet volumes: %w", err)
	}
	attacher, err := orchestration.NewVolumeAttacher(provider, orchestration.VolumeNodeID(node), staging)
	if err != nil {
		return nil, fmt.Errorf("fleet volumes: %w", err)
	}
	return orchestration.NewVolumeRuntime(inner, attacher)
}
