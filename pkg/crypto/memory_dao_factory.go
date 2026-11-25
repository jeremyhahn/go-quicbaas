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

package crypto

import (
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// NewMemoryDAOFactory returns a go-qrdb DAO factory over one in-memory KV
// store, which serves the default store and every named database alike.
//
// go-xkms stores that persist through DAOs (the key-version store among
// them) take a DAO factory where they used to take a storage backend; this
// is the in-process, non-persistent factory for the managers that used a
// memory backend. Single-process only: its atomicity is a mutex.
func NewMemoryDAOFactory() *qrdbsdk.DAOFactory {
	store := qrdbsdk.NewMemoryKVStore()
	return qrdbsdk.NewDAOFactoryWithConfig(&qrdbsdk.DAOFactoryConfig{
		KVStoreProvider: func(uint64) qrdbsdk.KVStore { return store },
		PartitionedProvider: func(string, uint64) qrdbsdk.KVStore {
			return store
		},
	})
}
