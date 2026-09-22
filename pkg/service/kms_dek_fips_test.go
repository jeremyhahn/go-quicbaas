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

package service

import (
	"context"
	goerrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/fips"
)

// activateFIPS makes the DEK gate behave as it does on a FIPS-activated
// process, and restores the real predicate when the test ends. It is the only
// way to reach both sides of the gate from one build; the fips140 job proves
// the predicate itself agrees with the runtime.
func activateFIPS(t *testing.T, activated bool) {
	t.Helper()
	previous := fipsActivated
	fipsActivated = func() bool { return activated }
	t.Cleanup(func() { fipsActivated = previous })
}

func newDEKTestService(t *testing.T) *kmsServiceImpl {
	t.Helper()
	svc := newKMSService(newMockKMSManager(), newMockLogger())
	require.NotNil(t, svc)
	return svc
}

func TestCreateDEKRefusesUnapprovedAlgorithmWhenFIPSActivated(t *testing.T) {
	tests := []struct {
		name      string
		algorithm KeyAlgorithm
	}{
		{name: "chacha20-poly1305", algorithm: KeyAlgChaCha20Poly1305},
		{name: "xchacha20-poly1305", algorithm: KeyAlgXChaCha20Poly1305},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			activateFIPS(t, true)
			svc := newDEKTestService(t)

			resp, err := svc.CreateDEK(context.Background(), &CreateDEKRequest{
				Path:      "data/objects",
				Algorithm: tt.algorithm,
			})

			require.Error(t, err)
			assert.Nil(t, resp)

			var notApproved *fips.AEADNotApprovedError
			require.True(t, goerrors.As(err, &notApproved),
				"the refusal must be a typed *fips.AEADNotApprovedError, got %T: %v", err, err)
			assert.Equal(t, string(tt.algorithm), notApproved.Algorithm)
			assert.Equal(t, "kms.CreateDEK", notApproved.Operation)
			assert.Contains(t, notApproved.Approved, string(KeyAlgAES256GCM))

			_, exists := svc.dekStore.get("data/objects")
			assert.False(t, exists, "a refused DEK must not be stored")
		})
	}
}

func TestCreateDEKAcceptsApprovedAlgorithmWhenFIPSActivated(t *testing.T) {
	tests := []struct {
		name      string
		algorithm KeyAlgorithm
		want      KeyAlgorithm
	}{
		{name: "aes-128-gcm", algorithm: KeyAlgAES128GCM, want: KeyAlgAES128GCM},
		{name: "aes-192-gcm", algorithm: KeyAlgAES192GCM, want: KeyAlgAES192GCM},
		{name: "aes-256-gcm", algorithm: KeyAlgAES256GCM, want: KeyAlgAES256GCM},
		{name: "unset takes the aes-256-gcm default", algorithm: "", want: KeyAlgAES256GCM},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			activateFIPS(t, true)
			svc := newDEKTestService(t)

			resp, err := svc.CreateDEK(context.Background(), &CreateDEKRequest{
				Path:      "data/objects",
				Algorithm: tt.algorithm,
			})

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, tt.want, resp.DEK.Algorithm)
		})
	}
}

// TestCreateDEKKeepsChaCha20WhenNotActivated holds the other half of owner
// ruling 8 in place: ChaCha20-Poly1305 remains a DEK option, and only FIPS
// activation takes it away.
func TestCreateDEKKeepsChaCha20WhenNotActivated(t *testing.T) {
	activateFIPS(t, false)
	svc := newDEKTestService(t)

	resp, err := svc.CreateDEK(context.Background(), &CreateDEKRequest{
		Path:      "data/objects",
		Algorithm: KeyAlgChaCha20Poly1305,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, KeyAlgChaCha20Poly1305, resp.DEK.Algorithm)
}

func TestRotateDEKRefusesAnUnapprovedExistingDEKWhenFIPSActivated(t *testing.T) {
	activateFIPS(t, false)
	svc := newDEKTestService(t)
	ctx := context.Background()

	created, err := svc.CreateDEK(ctx, &CreateDEKRequest{
		Path:      "data/objects",
		Algorithm: KeyAlgChaCha20Poly1305,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1), created.DEK.Version)

	// The deployment is then switched into FIPS mode. Re-keying the ChaCha20
	// DEK would hand a FIPS-activated process a fresh unapproved key, so the
	// rotation is refused and the existing version is left untouched.
	activateFIPS(t, true)

	resp, err := svc.RotateDEK(ctx, &RotateDEKRequest{Path: "data/objects"})

	require.Error(t, err)
	assert.Nil(t, resp)

	var notApproved *fips.AEADNotApprovedError
	require.True(t, goerrors.As(err, &notApproved),
		"the refusal must be a typed *fips.AEADNotApprovedError, got %T: %v", err, err)
	assert.Equal(t, "kms.RotateDEK", notApproved.Operation)
	assert.Equal(t, string(KeyAlgChaCha20Poly1305), notApproved.Algorithm)

	entry, exists := svc.dekStore.get("data/objects")
	require.True(t, exists)
	assert.Equal(t, uint64(1), entry.metadata.Version, "a refused rotation must not advance the version")
}

func TestRotateDEKAllowsAnApprovedDEKWhenFIPSActivated(t *testing.T) {
	activateFIPS(t, true)
	svc := newDEKTestService(t)
	ctx := context.Background()

	_, err := svc.CreateDEK(ctx, &CreateDEKRequest{
		Path:      "data/objects",
		Algorithm: KeyAlgAES256GCM,
	})
	require.NoError(t, err)

	resp, err := svc.RotateDEK(ctx, &RotateDEKRequest{Path: "data/objects"})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, uint64(2), resp.NewVersion)
}

// TestCreateDEKMatchesThisBuild is the conformance assertion: with the gate's
// own predicate in place, the service must agree with the runtime it was built
// for. The fips140 job runs this with FIPS activated; an ordinary run proves
// the unrestricted path.
func TestCreateDEKMatchesThisBuild(t *testing.T) {
	svc := newDEKTestService(t)

	resp, err := svc.CreateDEK(context.Background(), &CreateDEKRequest{
		Path:      "data/objects",
		Algorithm: KeyAlgChaCha20Poly1305,
	})

	if fips.Enabled() {
		require.Error(t, err, "a FIPS-activated build must refuse a ChaCha20 DEK")
		var notApproved *fips.AEADNotApprovedError
		assert.True(t, goerrors.As(err, &notApproved))
		return
	}
	require.NoError(t, err, "a build that is not FIPS-activated keeps ChaCha20 as a DEK option")
	assert.Equal(t, KeyAlgChaCha20Poly1305, resp.DEK.Algorithm)
}
