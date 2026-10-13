// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

//go:build integration && rest

package login

// The attestation keys (attest_level, attest_aaguid) on an auth factor are
// server-owned: they record what an enrollment ceremony proved. A raw factor
// write that asserts them is refused with a typed error that unwraps to the
// invalid-factor sentinel, which go-qrdb's gRPC mapper reports as
// InvalidArgument. These tests cross the real gRPC wire so a mapper change
// that turned the refusal into Internal (an outage, inviting retries that can
// never succeed) fails here instead of in production.

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	qrdbgrpc "github.com/jeremyhahn/go-qrdb/sdk/go/transport/grpc"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// reservedAttestationKeys are the metadata keys only an enrollment ceremony
// may write.
var reservedAttestationKeys = []string{
	iam.AuthFactorMetadataAttestLevel,
	iam.AuthFactorMetadataAttestAAGUID,
}

func TestAuthFactorGRPC_AddRefusesReservedAttestationMetadata(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client := newQRDBGRPCClient(ctx, t, mtlsTLSConfig(t))
	userID := createFactorTestUser(ctx, t, client)

	for _, key := range reservedAttestationKeys {
		t.Run(key, func(t *testing.T) {
			req := mtlsFactorRequest(t, userID)
			req.Metadata = map[string]string{key: "hardware"}

			_, err := client.UserAuthFactors().AddAuthFactor(ctx, req)
			requireInvalidArgumentNaming(t, err, key)
		})
	}
}

func TestAuthFactorGRPC_RotateRefusesReservedAttestationMetadata(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client := newQRDBGRPCClient(ctx, t, mtlsTLSConfig(t))
	factors := client.UserAuthFactors()
	userID := createFactorTestUser(ctx, t, client)

	added, err := factors.AddAuthFactor(ctx, mtlsFactorRequest(t, userID))
	require.NoError(t, err, "add the factor to rotate")
	oldID := added.Factor.ID

	for _, key := range reservedAttestationKeys {
		t.Run(key, func(t *testing.T) {
			newFactor := mtlsFactorRequest(t, userID)
			newFactor.Metadata = map[string]string{key: "hardware"}

			_, err := factors.RotateAuthFactor(ctx, &qrdbtransport.AuthFactorRotateRequest{
				OldFactorID: oldID,
				NewFactor:   *newFactor,
			})
			requireInvalidArgumentNaming(t, err, key)

			// A refused rotation must leave the old factor active: the
			// add-then-revoke is atomic, so nothing of it may have happened.
			got, err := factors.GetAuthFactor(ctx, &qrdbtransport.AuthFactorGetRequest{FactorID: oldID})
			require.NoError(t, err, "read back the old factor")
			require.Nil(t, got.Factor.RevokedAt, "a refused rotation revoked the old factor")
		})
	}
}

func TestAuthFactorGRPC_AddWithoutReservedMetadataStampsAttestLevelNone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client := newQRDBGRPCClient(ctx, t, mtlsTLSConfig(t))
	factors := client.UserAuthFactors()
	userID := createFactorTestUser(ctx, t, client)

	req := mtlsFactorRequest(t, userID)
	req.Metadata = map[string]string{"cert_cn": "attestation-it"}
	added, err := factors.AddAuthFactor(ctx, req)
	require.NoError(t, err, "a factor with no reserved metadata must be accepted")

	got, err := factors.GetAuthFactor(ctx, &qrdbtransport.AuthFactorGetRequest{FactorID: added.Factor.ID})
	require.NoError(t, err, "read back the accepted factor")
	require.Equal(t, string(iam.AttestLevelNone), got.Factor.Metadata[iam.AuthFactorMetadataAttestLevel],
		"a factor written outside a ceremony must record that nothing was attested")
	require.Equal(t, "attestation-it", got.Factor.Metadata["cert_cn"],
		"caller-supplied metadata must survive the write")
}

// requireInvalidArgumentNaming asserts err carries the gRPC InvalidArgument
// code and a message naming the refused metadata key.
func requireInvalidArgumentNaming(t *testing.T, err error, key string) {
	t.Helper()
	require.Error(t, err, "reserved attestation metadata %q was accepted", key)
	st := status.Convert(err)
	require.Equal(t, codes.InvalidArgument, st.Code(),
		"the refusal of %q must reach the caller as invalid input, not a server fault: %v", key, err)
	require.Contains(t, st.Message(), key, "the refusal must name the field the caller may not set")
}

// newQRDBGRPCClient dials the go-qrdb SDK gRPC transport at the quicbaas gRPC
// listener, where qrdb.v1.UserAuthFactorService is registered.
func newQRDBGRPCClient(ctx context.Context, t *testing.T, tlsCfg *tls.Config) *qrdbsdk.Client {
	t.Helper()
	endpoints := helpers.GetClusterEndpoints()
	require.NotEmpty(t, endpoints, "no gRPC endpoints")

	tr, err := qrdbgrpc.New(
		qrdbtransport.WithAddress(endpoints[0]),
		qrdbtransport.WithTLSConfig(tlsCfg),
		qrdbtransport.WithTimeout(30*time.Second),
	)
	require.NoError(t, err, "build qrdb gRPC transport")
	// Connect probes the QRDBService.Health RPC, which quicbaas does not
	// export; the connection is usable regardless, as in sdk/go/qrdb.go.
	if cerr := tr.Connect(ctx); cerr != nil && !strings.Contains(cerr.Error(), "health check failed") {
		require.NoError(t, cerr, "connect qrdb gRPC transport")
	}
	client, err := qrdbsdk.NewClient(tr)
	require.NoError(t, err, "build qrdb client")
	t.Cleanup(func() {
		if cerr := client.Close(); cerr != nil {
			t.Logf("close qrdb client: %v", cerr)
		}
	})
	return client
}

// createFactorTestUser creates a throwaway system-tenant user so the factors
// these tests write never attach to the admin principal other tests log in as.
func createFactorTestUser(ctx context.Context, t *testing.T, client *qrdbsdk.Client) uint64 {
	t.Helper()
	user, err := client.Users().CreateUser(ctx, &qrdbtransport.CreateUserRequest{
		Username:             "attest-it-" + hex.EncodeToString(randomBytes(t, 6)),
		TenantID:             systemTenantID,
		WithoutEmailLoginKey: true,
	})
	require.NoError(t, err, "create factor test user")
	t.Cleanup(func() {
		if derr := client.Users().DeleteUser(context.Background(), user.ID); derr != nil {
			t.Logf("delete factor test user %d: %v", user.ID, derr)
		}
	})
	return user.ID
}

// mtlsFactorRequest returns an mtls factor write with a fresh credential.
func mtlsFactorRequest(t *testing.T, userID uint64) *qrdbtransport.AuthFactorAddRequest {
	t.Helper()
	return &qrdbtransport.AuthFactorAddRequest{
		UserID:     userID,
		Type:       string(qrdbtransport.FactorTypeMTLS),
		Label:      "attestation-it",
		Credential: randomBytes(t, 32),
		CreatedBy:  "admin",
	}
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	_, err := rand.Read(b)
	require.NoError(t, err, "read random bytes")
	return b
}
