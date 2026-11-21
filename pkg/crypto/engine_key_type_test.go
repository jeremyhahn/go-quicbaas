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

package crypto

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
)

// The engine's key-hierarchy keys are symmetric KMS keys, so they are made
// under the KMS service's symmetric key type, which go-xkms can list and
// export, and not as KeyTypeEncryption (#1794).
func TestEngine_KeysUseServiceSymmetricKeyType(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	for _, algorithm := range []string{
		kms.AlgAES128GCM, kms.AlgAES192GCM, kms.AlgAES256GCM,
		kms.AlgChaCha20Poly1305, "",
	} {
		attrs := engine.algorithmToKeyAttributes("k", algorithm)
		if attrs.KeyType != kms.SymmetricKeyType {
			t.Errorf("algorithm %q: KeyType = %v, want the service symmetric type %v",
				algorithm, attrs.KeyType, kms.SymmetricKeyType)
		}
	}
}
