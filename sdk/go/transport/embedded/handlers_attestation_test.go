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

package embedded

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	attestationclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/attestation"
)

// errAttestation is a sentinel returned by the fake attestation service to
// drive error-propagation paths through the embedded handlers.
var errAttestation = errors.New("attestation: boom")

// fakeAttestationService is a configurable AttestationService. When failNext
// is set every method returns errAttestation; otherwise it echoes its input
// back so the handlers' field mapping can be asserted.
type fakeAttestationService struct {
	// The embedded interface supplies the Verifier verbs this stub does not
	// exercise; they are tested against the real service elsewhere.
	service.AttestationService

	failNext bool

	lastVerify      *service.VerifyAttestationRequest
	lastVerifyChain *service.VerifyAttestationChainRequest
	lastAttest      *service.AttestContainerRequest
}

func (f *fakeAttestationService) Verify(_ context.Context, req *service.VerifyAttestationRequest) (*service.VerifyAttestationResponse, error) {
	f.lastVerify = req
	if f.failNext {
		return nil, errAttestation
	}
	return &service.VerifyAttestationResponse{
		Valid:          true,
		Format:         req.Format,
		Backend:        req.Backend,
		ChainValid:     true,
		SignatureValid: true,
	}, nil
}

func (f *fakeAttestationService) VerifyChain(_ context.Context, req *service.VerifyAttestationChainRequest) (*service.VerifyAttestationChainResponse, error) {
	f.lastVerifyChain = req
	if f.failNext {
		return nil, errAttestation
	}
	return &service.VerifyAttestationChainResponse{Valid: true}, nil
}

func (f *fakeAttestationService) AttestContainer(_ context.Context, req *service.AttestContainerRequest) (*service.AttestContainerResponse, error) {
	f.lastAttest = req
	if f.failNext {
		return nil, errAttestation
	}
	return &service.AttestContainerResponse{
		Attested:        true,
		DeviceID:        req.DeviceID,
		ContainerID:     req.ContainerID,
		Image:           req.Image,
		ImageDigest:     req.ImageDigest,
		ConfigDigest:    "sha256:cfg",
		TrustLabel:      req.TrustLabel,
		TrustLevel:      req.TrustLevel,
		Measured:        true,
		MeasurementPCR:  16,
		MeasurementBank: "sha256",
		Quoted:          []byte("tpms-attest"),
		Signature:       []byte("sig"),
		Nonce:           []byte("nonce"),
		PCRs:            map[uint32][]byte{7: {0x01}},
		AttestedAt:      time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
	}, nil
}

// attestationMock embeds the base mock and overrides Attestation().
type attestationMock struct {
	*mockQuicBaaS
	attestation service.AttestationService
}

func (m *attestationMock) Attestation() service.AttestationService { return m.attestation }

func newAttestationTransport(t *testing.T, svc service.AttestationService) *Transport {
	t.Helper()
	tr, err := New(&attestationMock{mockQuicBaaS: &mockQuicBaaS{}, attestation: svc})
	require.NoError(t, err)
	return tr
}

func TestTransport_AttestationVerify(t *testing.T) {
	fake := &fakeAttestationService{}
	tr := newAttestationTransport(t, fake)

	resp, err := tr.Request(context.Background(), "quicbaas.Attestation", "Verify",
		&attestationclient.VerifyRequest{
			Format:          "tpm2",
			Backend:         "tpm2",
			ChainPEM:        [][]byte{[]byte("cert")},
			AttestedKeyDER:  []byte("key"),
			AttestationData: []byte("data"),
			Signature:       []byte("sig"),
			Options: &attestationclient.VerifyOptions{
				CheckFreshness:  true,
				AllowSelfSigned: true,
			},
		})
	require.NoError(t, err)

	typed, ok := resp.(*attestationclient.VerifyResponse)
	require.True(t, ok, "handler must return the leaf wire type")
	assert.True(t, typed.Valid)
	assert.True(t, typed.ChainValid)
	assert.True(t, typed.SignatureValid)
	assert.Equal(t, "tpm2", typed.Format)

	require.NotNil(t, fake.lastVerify)
	assert.Equal(t, []byte("data"), fake.lastVerify.AttestationData)
	require.NotNil(t, fake.lastVerify.Options, "options must reach the service")
	assert.True(t, fake.lastVerify.Options.CheckFreshness)
	assert.True(t, fake.lastVerify.Options.AllowSelfSigned)
}

// TestTransport_AttestationVerify_NilOptionsStayNil pins the secure-default
// contract across the boundary: an absent options block must NOT become an
// all-false struct, which would silently disable the freshness check the
// service applies by default.
func TestTransport_AttestationVerify_NilOptionsStayNil(t *testing.T) {
	fake := &fakeAttestationService{}
	tr := newAttestationTransport(t, fake)

	_, err := tr.Request(context.Background(), "quicbaas.Attestation", "Verify",
		&attestationclient.VerifyRequest{
			Format:         "tpm2",
			ChainPEM:       [][]byte{[]byte("cert")},
			AttestedKeyDER: []byte("key"),
			Signature:      []byte("sig"),
		})
	require.NoError(t, err)
	require.NotNil(t, fake.lastVerify)
	assert.Nil(t, fake.lastVerify.Options)
}

func TestTransport_AttestationVerify_Errors(t *testing.T) {
	t.Run("ServiceError", func(t *testing.T) {
		tr := newAttestationTransport(t, &fakeAttestationService{failNext: true})
		_, err := tr.Request(context.Background(), "quicbaas.Attestation", "Verify",
			&attestationclient.VerifyRequest{Format: "tpm2", ChainPEM: [][]byte{[]byte("c")}})
		require.ErrorIs(t, err, errAttestation)
	})

	t.Run("WrongRequestType", func(t *testing.T) {
		tr := newAttestationTransport(t, &fakeAttestationService{})
		_, err := tr.Request(context.Background(), "quicbaas.Attestation", "Verify",
			&attestationclient.VerifyChainRequest{})
		require.Error(t, err)
	})

	t.Run("NilRequest", func(t *testing.T) {
		tr := newAttestationTransport(t, &fakeAttestationService{})
		_, err := tr.Request(context.Background(), "quicbaas.Attestation", "Verify", nil)
		require.Error(t, err)
	})

	t.Run("ServiceNotConfigured", func(t *testing.T) {
		tr := newAttestationTransport(t, nil)
		_, err := tr.Request(context.Background(), "quicbaas.Attestation", "Verify",
			&attestationclient.VerifyRequest{Format: "tpm2", ChainPEM: [][]byte{[]byte("c")}})
		require.Error(t, err)
	})
}

func TestTransport_AttestationVerifyChain(t *testing.T) {
	fake := &fakeAttestationService{}
	tr := newAttestationTransport(t, fake)

	resp, err := tr.Request(context.Background(), "quicbaas.Attestation", "VerifyChain",
		&attestationclient.VerifyChainRequest{
			Format:   "tpm2",
			ChainPEM: [][]byte{[]byte("leaf"), []byte("root")},
		})
	require.NoError(t, err)

	typed, ok := resp.(*attestationclient.VerifyChainResponse)
	require.True(t, ok)
	assert.True(t, typed.Valid)

	require.NotNil(t, fake.lastVerifyChain)
	assert.Len(t, fake.lastVerifyChain.ChainPEM, 2)
}

func TestTransport_AttestationVerifyChain_Errors(t *testing.T) {
	t.Run("ServiceError", func(t *testing.T) {
		tr := newAttestationTransport(t, &fakeAttestationService{failNext: true})
		_, err := tr.Request(context.Background(), "quicbaas.Attestation", "VerifyChain",
			&attestationclient.VerifyChainRequest{ChainPEM: [][]byte{[]byte("c")}})
		require.ErrorIs(t, err, errAttestation)
	})

	t.Run("WrongRequestType", func(t *testing.T) {
		tr := newAttestationTransport(t, &fakeAttestationService{})
		_, err := tr.Request(context.Background(), "quicbaas.Attestation", "VerifyChain",
			&attestationclient.VerifyRequest{})
		require.Error(t, err)
	})
}

func TestTransport_AttestationAttestContainer(t *testing.T) {
	fake := &fakeAttestationService{}
	tr := newAttestationTransport(t, fake)

	resp, err := tr.Request(context.Background(), "quicbaas.Attestation", "AttestContainer",
		&attestationclient.AttestContainerRequest{
			DeviceID:     "device-1",
			ContainerID:  "container-1",
			Image:        "alpine@sha256:abc",
			ImageDigest:  "sha256:abc",
			TrustLabel:   "prod",
			TrustLevel:   "measured",
			ExpectedPCRs: map[uint32][]byte{7: {0x01}},
		})
	require.NoError(t, err)

	typed, ok := resp.(*attestationclient.AttestContainerResponse)
	require.True(t, ok)
	assert.True(t, typed.Attested)
	assert.True(t, typed.Measured)
	assert.Equal(t, "container-1", typed.ContainerID)
	assert.Equal(t, "sha256:cfg", typed.ConfigDigest)
	assert.Equal(t, "2026-08-09T12:00:00Z", typed.AttestedAt,
		"timestamp must cross the boundary as RFC 3339 so every transport agrees")
	assert.Equal(t, []byte{0x01}, typed.PCRs[7])

	require.NotNil(t, fake.lastAttest)
	assert.Equal(t, "device-1", fake.lastAttest.DeviceID)
	assert.Equal(t, "measured", fake.lastAttest.TrustLevel)
}

func TestTransport_AttestationAttestContainer_Errors(t *testing.T) {
	t.Run("NotConfiguredPropagates", func(t *testing.T) {
		// The real service returns ErrAttestationNoAttestor when no
		// attestor is registered; the handler must surface it unchanged
		// rather than flattening it into a generic failure.
		tr := newAttestationTransport(t, service.NewAttestationService(nil, nil))
		_, err := tr.Request(context.Background(), "quicbaas.Attestation", "AttestContainer",
			&attestationclient.AttestContainerRequest{Image: "alpine@sha256:abc"})
		require.ErrorIs(t, err, service.ErrAttestationNoAttestor)
	})

	t.Run("WrongRequestType", func(t *testing.T) {
		tr := newAttestationTransport(t, &fakeAttestationService{})
		_, err := tr.Request(context.Background(), "quicbaas.Attestation", "AttestContainer",
			&attestationclient.VerifyRequest{})
		require.Error(t, err)
	})

	t.Run("ServiceError", func(t *testing.T) {
		tr := newAttestationTransport(t, &fakeAttestationService{failNext: true})
		_, err := tr.Request(context.Background(), "quicbaas.Attestation", "AttestContainer",
			&attestationclient.AttestContainerRequest{Image: "alpine@sha256:abc"})
		require.ErrorIs(t, err, errAttestation)
	})
}

func TestTransport_AttestationUnknownMethod(t *testing.T) {
	tr := newAttestationTransport(t, &fakeAttestationService{})
	_, err := tr.Request(context.Background(), "quicbaas.Attestation", "Quarantine", nil)
	require.Error(t, err, "Tier 3 verbs are not part of the QuicBaaS surface")
}

func TestTypeNameOf(t *testing.T) {
	assert.Equal(t, "<nil>", typeNameOf(nil))
	assert.Equal(t, "*attestation.VerifyRequest", typeNameOf(&attestationclient.VerifyRequest{}))
}
