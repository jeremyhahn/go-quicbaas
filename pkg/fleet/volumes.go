package fleet

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

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
	// under on the host. Empty means `<work_dir>/volumes`. The provider name
	// is appended in both cases, and one volume runtime holds the dir at a
	// time.
	StagingDir string `json:"staging_dir,omitempty" yaml:"staging_dir,omitempty"`

	// LockDir is the absolute host-wide directory each owned volume's lock is
	// held under, so two daemons whose provider settings name the same backing
	// storage never own one volume at once. Empty means
	// DefaultVolumeLockDir. Every daemon on a host must use the same value.
	LockDir string `json:"lock_dir,omitempty" yaml:"lock_dir,omitempty"`
}

// DefaultVolumeLockDir is the host-wide volume lock directory used when
// VolumeConfig.LockDir is empty.
const DefaultVolumeLockDir = "/run/lock/quicbaas/volumes"

// volumeNamespace names the backing storage a provider's settings select:
// the first 16 hex characters of a sha256 over the sorted "k=v" lines of
// every setting but node_id, which is per daemon.
func volumeNamespace(settings map[string]string) string {
	keys := make([]string, 0, len(settings))
	for k := range settings {
		if k != "node_id" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		fmt.Fprintf(h, "%s=%s\n", k, settings[k])
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
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
// stop, remove and a failed start. Its staging dir is cfg.StagingDir, or
// `<workDir>/volumes` when that is empty, with the provider name appended, so
// daemons with their own work dirs never share attachment state; the
// runtime holds the dir exclusively. With a work dir, a volume launch whose
// quicbaas.owner label is not that work dir is refused, so every volume
// container is one the startup sweep removes before the recovered volumes are
// released; without one the sweep never releases them.
func WrapVolumeRuntime(cfg VolumeConfig, workDir string, inner orchestration.ContainerRuntime) (orchestration.ContainerRuntime, error) {
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
	if cfg.LockDir != "" && !filepath.IsAbs(cfg.LockDir) {
		return nil, fmt.Errorf("%w: fleet volumes: lock_dir %q is not absolute",
			orchestration.ErrVolumeInvalidArgument, cfg.LockDir)
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
	base := cfg.StagingDir
	if base == "" {
		if workDir == "" {
			return nil, fmt.Errorf("%w: fleet volumes: no staging_dir and no work dir", orchestration.ErrVolumeInvalidArgument)
		}
		abs, err := filepath.Abs(workDir)
		if err != nil {
			return nil, fmt.Errorf("fleet volumes: work dir: %w", err)
		}
		base = filepath.Join(abs, "volumes")
	}
	staging := filepath.Join(base, cfg.Provider)
	lockBase := cfg.LockDir
	if lockBase == "" {
		lockBase = DefaultVolumeLockDir
	}
	lockDir := filepath.Join(lockBase, cfg.Provider+"-"+volumeNamespace(cfg.Settings))
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
	opts := []orchestration.VolumeRuntimeOption{orchestration.WithVolumeLockDir(lockDir)}
	if strings.TrimSpace(workDir) != "" {
		opts = append(opts, orchestration.WithVolumeOwner(workDir))
	}
	return orchestration.NewVolumeRuntime(inner, attacher, opts...)
}
