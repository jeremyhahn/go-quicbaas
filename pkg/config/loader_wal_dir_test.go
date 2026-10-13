// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// TestLoaderLoad_WALDirFollowsDataDir proves that storage.wal_dir follows
// storage.data_dir when the data dir comes from the config file or the
// environment, that an explicit wal_dir from either source wins, and that the
// defaults-layer WAL dir is kept when neither key is set (#176).
func TestLoaderLoad_WALDirFollowsDataDir(t *testing.T) {
	const (
		fromDataDir  = "derived"
		fromExplicit = "explicit"
		fromDefault  = "default"
	)
	tests := []struct {
		name        string
		fileDataDir bool
		fileWALDir  bool
		envDataDir  bool
		envWALDir   bool
		want        string
	}{
		{name: "config file data_dir only", fileDataDir: true, want: fromDataDir},
		{name: "config file sets both", fileDataDir: true, fileWALDir: true, want: fromExplicit},
		{name: "env data_dir only", envDataDir: true, want: fromDataDir},
		{name: "env sets both", envDataDir: true, envWALDir: true, want: fromExplicit},
		{name: "config file data_dir with env wal_dir", fileDataDir: true, envWALDir: true, want: fromExplicit},
		{name: "neither set", want: fromDefault},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			explicitWAL := filepath.Join(dir, "operator-wal")

			// Empty values read as unset, so the host environment cannot leak in.
			t.Setenv("QUICBAAS_STORAGE_DATA_DIR", "")
			t.Setenv("QUICBAAS_STORAGE_WAL_DIR", "")
			if tt.envDataDir {
				t.Setenv("QUICBAAS_STORAGE_DATA_DIR", dir)
			}
			if tt.envWALDir {
				t.Setenv("QUICBAAS_STORAGE_WAL_DIR", explicitWAL)
			}

			storage := ""
			if tt.fileDataDir {
				storage += fmt.Sprintf("  data_dir: %s\n", dir)
			}
			if tt.fileWALDir {
				storage += fmt.Sprintf("  wal_dir: %s\n", explicitWAL)
			}
			content := "logging:\n  level: warn\n"
			if storage != "" {
				content += "storage:\n" + storage
			}
			path := filepath.Join(dir, "quicbaas.yaml")
			require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

			cfg, err := NewLoader().Load(path)
			require.NoError(t, err)

			switch tt.want {
			case fromDataDir:
				require.Equal(t, dir, cfg.Storage.DataDir)
				require.Equal(t, filepath.Join(dir, "wal"), cfg.Storage.WALDir)
				require.NoError(t, Validate(cfg),
					"config with a derived WAL dir must pass validation")
			case fromExplicit:
				require.Equal(t, dir, cfg.Storage.DataDir)
				require.Equal(t, explicitWAL, cfg.Storage.WALDir)
			case fromDefault:
				require.Equal(t, Default().Storage.DataDir, cfg.Storage.DataDir)
				require.Equal(t, Default().Storage.WALDir, cfg.Storage.WALDir)
			}
		})
	}
}

// TestLoaderLoad_MissingFileDerivesWALDirFromEnv covers the missing-file
// branch of Load, which decodes the environment layer on its own path.
func TestLoaderLoad_MissingFileDerivesWALDirFromEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("QUICBAAS_STORAGE_DATA_DIR", dir)
	t.Setenv("QUICBAAS_STORAGE_WAL_DIR", "")

	cfg, err := NewLoader().Load(filepath.Join(dir, "missing.yaml"))
	require.NoError(t, err)

	require.Equal(t, dir, cfg.Storage.DataDir)
	require.Equal(t, filepath.Join(dir, "wal"), cfg.Storage.WALDir)
}

// TestDeriveWALDir_EmptyDataDirKeepsWALDir proves that a data_dir set to the
// empty string does not derive a relative "wal" directory: the WAL dir is
// left for validation to judge.
func TestDeriveWALDir_EmptyDataDirKeepsWALDir(t *testing.T) {
	v := viper.New()
	v.Set("storage.data_dir", "")
	cfg := Default()
	cfg.Storage.DataDir = ""
	want := cfg.Storage.WALDir

	DeriveWALDir(v, cfg)

	require.Equal(t, want, cfg.Storage.WALDir)
}
