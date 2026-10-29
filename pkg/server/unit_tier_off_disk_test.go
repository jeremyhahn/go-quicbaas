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
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pebbleStoresWithoutMemoryOption names the Pebble stores, relative to
// Storage.DataDir, that a withTempServer start still opens because no
// in-memory option exists for them yet (#1770):
//
//   - "seal": buildDataPlaneBarrierService opens a Pebble seal sub-store for
//     any non-empty Storage.DataDir, whatever Storage.Backend says.
//   - "vector": the embedded go-qrdb node always backs its vector service with
//     Pebble under its data directory, and refuses an empty one.
//
// Each entry leaves this list when its in-memory option lands.
var pebbleStoresWithoutMemoryOption = map[string]bool{
	"seal":   true,
	"vector": true,
}

// TestWithTempServer_StartOpensNoPebbleDatabase proves the unit-tier fixture
// selects the in-memory qrdb engine (#1770): an embedded start with KMS on,
// through withTempServer, opens no Pebble store under the temp root except
// the ones listed in pebbleStoresWithoutMemoryOption. A fixture that selects
// the Pebble backend again opens the node database at data/pebble and fails.
func TestWithTempServer_StartOpensNoPebbleDatabase(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.KMS.Enabled = true
	dataDir := srv.config.Storage.DataDir
	root := filepath.Dir(dataDir)

	require.NoError(t, srv.initialize(), "the embedded start must succeed")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx), "the shutdown must succeed")

	var unexpected []string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() || !(strings.HasPrefix(name, "MANIFEST-") || strings.HasSuffix(name, ".sst")) {
			return nil
		}
		store, relErr := filepath.Rel(dataDir, filepath.Dir(path))
		if relErr != nil {
			return relErr
		}
		if !pebbleStoresWithoutMemoryOption[store] {
			unexpected = append(unexpected, path)
		}
		return nil
	})
	require.NoError(t, walkErr)
	assert.Empty(t, unexpected, "the unit-tier server must not open a Pebble store with an in-memory option")
}
