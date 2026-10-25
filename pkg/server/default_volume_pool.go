// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package server

import (
	"context"
	"os"
	"path/filepath"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// defaultVolumePoolName is the catalog name of the volume pool the node maps
// under its data dir for barrier-mode tenants.
const defaultVolumePoolName = "default"

// qrdbDataDir is the embedded node's data dir: the storage data dir, else the
// barrier dir.
func qrdbDataDir(cfg *config.Config) string {
	if cfg.Storage.DataDir != "" {
		return cfg.Storage.DataDir
	}
	return cfg.Seal.BarrierDir
}

// defaultVolumePoolPath is the directory of the default volume pool, or ""
// for a node with no data dir, which maps no pool.
func defaultVolumePoolPath(cfg *config.Config) string {
	dataDir := qrdbDataDir(cfg)
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, defaultVolumePoolDirName)
}

// createDefaultVolumePoolDir creates the default pool's directory before the
// node is built. qrdb's dir provider opens a pool only when its directory
// already exists; otherwise the node serves the pool nowhere and every
// pooled tenant barrier fails to register.
func createDefaultVolumePoolDir(cfg *config.Config) error {
	poolPath := defaultVolumePoolPath(cfg)
	if poolPath == "" {
		return nil
	}
	if err := os.MkdirAll(poolPath, 0o700); err != nil {
		return &QRDBNodeError{Stage: "create default volume pool directory", Cause: err}
	}
	return nil
}

// provisionDefaultVolumePool creates the default pool in the node's
// replicated volume catalog. A barrier tenant reserves its placement on a
// catalog pool, so a pool the node maps but the catalog lacks still fails
// registration. It runs on every boot of an unsealed node; an entry an
// earlier boot or another member created is left as it is.
func (s *Server) provisionDefaultVolumePool() error {
	node := s.getQRDBNode()
	if node == nil || defaultVolumePoolPath(s.config) == "" {
		return nil
	}
	// A node without a volume catalog (no tenant isolation, or a store
	// with no pool attached) places no tenant on a pool.
	if registry := node.BarrierRegistry(); registry == nil || !registry.HasVolumePool() {
		return nil
	}
	ctx, err := node.ServiceContext(context.Background())
	if err != nil {
		return &QRDBNodeError{Stage: "provision default volume pool", Cause: err}
	}
	pools := node.Client().VolumePools()
	if _, err := pools.GetPool(ctx, defaultVolumePoolName); err == nil {
		return nil
	}
	_, createErr := pools.CreatePool(ctx, &qrdbtransport.CreateVolumePoolRequest{Name: defaultVolumePoolName})
	if createErr == nil {
		return nil
	}
	// A member booting at the same time may have created it first.
	if _, err := pools.GetPool(ctx, defaultVolumePoolName); err == nil {
		return nil
	}
	return &QRDBNodeError{Stage: "provision default volume pool", Cause: createErr}
}
