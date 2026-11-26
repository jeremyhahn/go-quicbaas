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

package kms

import (
	"context"
	"errors"
	"io/fs"
	"sort"
	"strings"

	goxkms "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// xkmsKeyNameMappingMarker is the key-store record whose presence says every
// go-xkms name in the store was written under xkmsKeyName.
const xkmsKeyNameMappingMarker = "quicbaas.xkms-key-name-mapping.v1"

// ErrLegacyXKMSKeyName is the sentinel a LegacyXKMSKeyNamesError unwraps to:
// the key store holds a name written before the xkmsKeyName mapping that now
// reads as an encoded key ID.
var ErrLegacyXKMSKeyName = errors.New("kms: stored go-xkms key name reads as an encoded key ID")

// LegacyXKMSKeyNamesError lists the stored go-xkms key names that read as
// encoded key IDs. The KMS does not open over such a store until the
// operator re-imports each listed key under a name outside that shape.
type LegacyXKMSKeyNamesError struct {
	Names []string
}

// Error names every colliding key and the operator's remedy.
func (e *LegacyXKMSKeyNamesError) Error() string {
	return "kms: stored go-xkms key names " + strings.Join(e.Names, ", ") +
		" read as encoded key IDs; re-import each key under a name that does not start with \"qbx.\", then start again"
}

// Unwrap returns ErrLegacyXKMSKeyName.
func (e *LegacyXKMSKeyNamesError) Unwrap() error {
	return ErrLegacyXKMSKeyName
}

// checkXKMSKeyNames refuses a key store that holds a legacy name which reads
// as an encoded key ID, and marks a clean store so later opens skip the scan.
// A nil store has nothing persisted to mark and is not checked. A backend
// whose key directory does not exist holds no keys.
func checkXKMSKeyNames(ctx context.Context, store goxkms.StorageBackend, backends []string) error {
	if store == nil {
		return nil
	}

	if _, err := store.Get(ctx, xkmsKeyNameMappingMarker); err == nil {
		return nil
	} else if !errors.Is(err, goxkms.ErrStorageNotFound) {
		return err
	}

	seen := make(map[string]struct{})
	for _, backend := range backends {
		keys, err := xkms.ListKeys(ctx, backend)
		if errors.Is(err, fs.ErrNotExist) {
			// A key directory that does not exist holds no keys.
			continue
		}
		if err != nil {
			return err
		}
		for _, attrs := range keys {
			if attrs == nil {
				continue
			}
			if _, ok := decodeXKMSKeyName(attrs.CN); ok {
				seen[attrs.CN] = struct{}{}
			}
		}
	}

	if len(seen) > 0 {
		names := make([]string, 0, len(seen))
		for name := range seen {
			names = append(names, name)
		}
		sort.Strings(names)
		return &LegacyXKMSKeyNamesError{Names: names}
	}

	return store.Put(ctx, xkmsKeyNameMappingMarker, []byte("1"))
}
