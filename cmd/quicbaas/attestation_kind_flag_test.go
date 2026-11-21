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

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAttestationEndorsementPut_KindFlagNamesEveryKind checks that the
// --kind help of `attestation endorsement put` names every endorsement kind
// the server accepts.
func TestAttestationEndorsementPut_KindFlagNamesEveryKind(t *testing.T) {
	cmd := newAttestationEndorsementCommand()
	put, _, err := cmd.Find([]string{"put"})
	require.NoError(t, err)
	require.Equal(t, "put", put.Name())

	flag := put.Flags().Lookup("kind")
	require.NotNil(t, flag)
	for _, kind := range []string{"ak-root", "reference-signer", "image-signer", "manifest-signer"} {
		assert.Contains(t, flag.Usage, kind)
	}
}
