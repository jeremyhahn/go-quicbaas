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

package signserver_test

import (
	"context"
	"errors"
	"testing"

	xkmssignserver "github.com/jeremyhahn/go-xkms/sdk/go/signserver"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/signserver"
)

// Compile-time proof the aliases are the go-xkms types: a value crosses the
// boundary in both directions without conversion.
var (
	_ xkmssignserver.Config             = signserver.Config{}
	_ signserver.Config                 = xkmssignserver.Config{}
	_ xkmssignserver.Slot               = signserver.Slot{}
	_ xkmssignserver.KeyRequest         = signserver.KeyRequest{}
	_ xkmssignserver.Key                = signserver.Key{}
	_ xkmssignserver.ProvisionConfig    = signserver.ProvisionConfig{}
	_ xkmssignserver.KeyPairRequest     = signserver.KeyPairRequest{}
	_ xkmssignserver.ProvisionedKeyPair = signserver.ProvisionedKeyPair{}
	_ *xkmssignserver.Server            = (*signserver.Server)(nil)
	_ *xkmssignserver.Error             = (*signserver.Error)(nil)
	_ *xkmssignserver.SlotError         = (*signserver.SlotError)(nil)
	_ *xkmssignserver.ProvisionError    = (*signserver.ProvisionError)(nil)
)

// TestNew_RefusesAConfigWithNoSocket drives the aliased constructor: it is the
// go-xkms server, so it validates exactly as go-xkms does and its failure is
// matched by the re-exported sentinel.
func TestNew_RefusesAConfigWithNoSocket(t *testing.T) {
	srv, err := signserver.New(context.Background(), signserver.Config{})
	if srv != nil {
		t.Fatal("no server may be built from an empty config")
	}
	if !errors.Is(err, signserver.ErrSocketPathRequired) {
		t.Fatalf("New(empty) = %v, want ErrSocketPathRequired", err)
	}
}

// TestProvisionECKeyPair_RefusesMissingClient proves the key-create entry
// point is go-xkms's: with no key service it fails with the re-exported
// sentinel rather than touching any token.
func TestProvisionECKeyPair_RefusesMissingClient(t *testing.T) {
	_, err := signserver.ProvisionECKeyPair(context.Background(), signserver.ProvisionConfig{}, signserver.KeyPairRequest{Label: "vtep", ID: []byte{0x01}})
	if !errors.Is(err, signserver.ErrProvisionClientRequired) {
		t.Fatalf("ProvisionECKeyPair(no client) = %v, want ErrProvisionClientRequired", err)
	}
}

// TestSentinels_AreTheUpstreamValues keeps the sentinels one-for-one, so a
// consumer's errors.Is decision survives the alias.
func TestSentinels_AreTheUpstreamValues(t *testing.T) {
	for _, tc := range []struct {
		name      string
		local, up error
	}{
		{"SocketPathRequired", signserver.ErrSocketPathRequired, xkmssignserver.ErrSocketPathRequired},
		{"NoAllowedPeers", signserver.ErrNoAllowedPeers, xkmssignserver.ErrNoAllowedPeers},
		{"KeyNotAdmitted", signserver.ErrKeyNotAdmitted, xkmssignserver.ErrKeyNotAdmitted},
		{"TokenWriteDenied", signserver.ErrTokenWriteDenied, xkmssignserver.ErrTokenWriteDenied},
		{"ProvisionKeyConflict", signserver.ErrProvisionKeyConflict, xkmssignserver.ErrProvisionKeyConflict},
	} {
		if !errors.Is(tc.local, tc.up) {
			t.Errorf("%s is not the go-xkms sentinel", tc.name)
		}
	}
}
