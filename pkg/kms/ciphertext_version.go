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
	"encoding/json"
	"fmt"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"
)

// CiphertextKeyVersion returns the key version a ciphertext produced by
// XKMSBackend.Encrypt was sealed under. It decodes the ciphertext the way
// XKMSBackend.Decrypt does and reads the version from the "kv" metadata entry.
// A ciphertext without that entry, or with one that does not parse, is legacy
// data sealed under version 1, as XKMSBackend.Decrypt assumes.
func CiphertextKeyVersion(ciphertext []byte) (uint64, error) {
	var encrypted kctypes.EncryptedData
	if err := json.Unmarshal(ciphertext, &encrypted); err != nil {
		return 0, err
	}
	versionBytes, ok := encrypted.Metadata[keyVersionMetadataKey]
	if !ok {
		return 1, nil
	}
	version := 1
	if _, err := fmt.Sscanf(string(versionBytes), "%d", &version); err != nil || version < 1 {
		return 1, nil
	}
	return uint64(version), nil
}
