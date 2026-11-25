package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestShippedConfigs_EnableTheSeal guards the shipped example configurations
// and the integration stack's node configuration: every one must run behind
// the at-rest barrier, because the secrets manager refuses secrets on a node
// without one.
func TestShippedConfigs_EnableTheSeal(t *testing.T) {
	root := repoRoot(t)

	configs := []string{
		".quicbaas.example.yaml",
		"configs/memory.yaml",
		"configs/file-local.yaml",
		"configs/pebble-local.yaml",
		"configs/raft-single-node.yaml",
		"configs/raft-3-node/node1.yaml",
		"configs/raft-3-node/node2.yaml",
		"configs/raft-3-node/node3.yaml",
		"test/integration/quicbaas.yaml",
	}
	for _, rel := range configs {
		t.Run(rel, func(t *testing.T) {
			cfg, err := LoadFromFile(filepath.Join(root, rel))
			if err != nil {
				t.Fatalf("loading %s failed: %v", rel, err)
			}
			if !cfg.Seal.Enabled {
				t.Errorf("%s leaves seal.enabled off: secrets are refused without an at-rest barrier", rel)
			}
			if cfg.Seal.Strategy == "" || cfg.Seal.Strategy == "none" {
				t.Errorf("%s sets seal.strategy %q: secrets are refused without an at-rest barrier", rel, cfg.Seal.Strategy)
			}
		})
	}

	composeFiles := []string{
		"configs/docker/docker-compose.file-local.yaml",
		"configs/docker/docker-compose.memory.yaml",
		"configs/docker/docker-compose.pebble-local.yaml",
		"configs/docker/docker-compose.raft-3-node.yaml",
		"configs/docker/docker-compose.raft-single-node.yaml",
	}
	for _, rel := range composeFiles {
		t.Run(rel, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, rel))
			if err != nil {
				t.Fatalf("reading %s failed: %v", rel, err)
			}
			text := string(data)
			for _, want := range []string{"QUICBAAS_SEAL_ENABLED=true", "QUICBAAS_SEAL_PASSPHRASE="} {
				if !strings.Contains(text, want) {
					t.Errorf("%s does not set %s: secrets are refused without an at-rest barrier", rel, want)
				}
			}
		})
	}
}
