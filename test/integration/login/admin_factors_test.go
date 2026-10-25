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

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	webauthnclient "github.com/jeremyhahn/go-xkms/pkg/webauthn/client"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/fido2"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"

	"github.com/stretchr/testify/require"
)

// adminFactorRPID is the relying party the integration server is configured
// with; Enroll derives the WebAuthn origin https://localhost from it.
const adminFactorRPID = "localhost"

// adminFactorOrigin is the WebAuthn origin the software authenticator signs
// into its client data. The integration server lists it in
// iam.webauthn.rp_origins, and go-xkms's SoftwareAdapter refuses a ceremony
// without one.
const adminFactorOrigin = "https://" + adminFactorRPID

// SoftwarePasskeyError reports a software authenticator ceremony failure.
type SoftwarePasskeyError struct {
	Op    string
	Cause error
}

func (e *SoftwarePasskeyError) Error() string {
	return "software passkey: " + e.Op + ": " + e.Cause.Error()
}

func (e *SoftwarePasskeyError) Unwrap() error { return e.Cause }

// softwarePasskey drives go-xkms's headless CTAP2 software authenticator
// through the AdminPasskeyAuthenticator seam Enroll uses for a USB key.
type softwarePasskey struct {
	adapter *webauthnclient.SoftwareAdapter
}

func (p *softwarePasskey) ListDevices() ([]fido2.Device, error) {
	return []fido2.Device{{Path: "software"}}, nil
}

func (p *softwarePasskey) BindDevice(string) {}

// RegisterWebAuthn re-encodes the parsed options as the creation options
// document the software adapter consumes, and reports the credential it made.
func (p *softwarePasskey) RegisterWebAuthn(opts fido2.WebAuthnRegistrationOptions) (*fido2.WebAuthnAttestation, error) {
	userVerification := "discouraged"
	if opts.RequireUserVerification {
		userVerification = "required"
	}
	optionsJSON, err := json.Marshal(map[string]any{
		"challenge": base64.RawURLEncoding.EncodeToString(opts.Challenge),
		"rp":        map[string]string{"id": opts.RPID, "name": opts.RPName},
		"user": map[string]string{
			"id":          base64.RawURLEncoding.EncodeToString(opts.UserID),
			"name":        opts.UserName,
			"displayName": opts.UserDisplayName,
		},
		"pubKeyCredParams":       []map[string]any{{"type": "public-key", "alg": -7}},
		"attestation":            "none",
		"authenticatorSelection": map[string]string{"userVerification": userVerification},
	})
	if err != nil {
		return nil, &SoftwarePasskeyError{Op: "encode creation options", Cause: err}
	}
	credential, err := p.adapter.MakeCredential(context.Background(), &webauthnclient.CeremonyOptions{
		OptionsJSON: optionsJSON,
		Origin:      opts.Origin,
	})
	if err != nil {
		return nil, &SoftwarePasskeyError{Op: "make credential", Cause: err}
	}
	encodedID, err := credentialIDFromWebAuthnDocument(credential)
	if err != nil {
		return nil, &SoftwarePasskeyError{Op: "read credential id", Cause: err}
	}
	credentialID, err := base64.RawURLEncoding.DecodeString(encodedID)
	if err != nil {
		return nil, &SoftwarePasskeyError{Op: "decode credential id", Cause: err}
	}
	return &fido2.WebAuthnAttestation{CredentialID: credentialID, PublicKeyCredentialJSON: credential}, nil
}

// passkeyLogin runs the unified FIDO2 login with the software authenticator.
func passkeyLogin(ctx context.Context, c quicbaas.Client, adapter *webauthnclient.SoftwareAdapter) (*quicbaas.FinishLoginResponse, error) {
	auth := c.Security().Auth()
	begin, err := auth.BeginLogin(ctx, qrdbtransport.BeginLoginRequest{
		Type:     qrdbtransport.FactorTypeFIDO2,
		Hint:     adminSubject,
		TenantID: systemTenantID,
	})
	if err != nil {
		return nil, err
	}
	assertion, err := adapter.GetAssertion(ctx, &webauthnclient.CeremonyOptions{
		OptionsJSON: []byte(begin.ChallengeMetadata["options"]),
		Origin:      adminFactorOrigin,
	})
	if err != nil {
		return nil, &SoftwarePasskeyError{Op: "get assertion", Cause: err}
	}
	credentialID, err := credentialIDFromWebAuthnDocument(assertion)
	if err != nil {
		return nil, err
	}
	return auth.FinishLogin(ctx, qrdbtransport.FinishLoginRequest{
		SessionID: begin.SessionID,
		Type:      qrdbtransport.FactorTypeFIDO2,
		LookupKey: []byte(credentialID),
		Signature: assertion,
		MethodMetadata: map[string]string{
			"session_id":    begin.ChallengeMetadata["session_id"],
			"credential_id": credentialID,
		},
	})
}

// signWith returns a nonce signer for the unified PIV login.
func signWith(key crypto.Signer) func(nonce []byte) ([]byte, error) {
	return func(nonce []byte) ([]byte, error) {
		digest := sha256.Sum256(nonce)
		return key.Sign(rand.Reader, digest[:], crypto.SHA256)
	}
}

// adminCallerOptions returns the client options that make tr's caller the
// admin. A verified client certificate makes the admin the caller on every
// transport but REST, which takes the caller only from a bearer, so on REST
// the admin logs in with the same certificate first.
func adminCallerOptions(t *testing.T, tr loginTransport) []quicbaas.Option {
	t.Helper()
	if tr != transportREST {
		return nil
	}
	endpoints := helpers.GetRESTEndpoints()
	require.NotEmpty(t, endpoints, "REST endpoint")
	certPath, keyPath, caPath := adminCertPaths()
	bearer, err := helpers.AuthPIVContext(context.Background(), endpoints[0], certPath, keyPath, caPath)
	require.NoError(t, err, "admin PIV login")
	return []quicbaas.Option{quicbaas.WithAuthToken(bearer)}
}

// A passkey the administrator enrolls through AdminFactors().Enroll is the
// credential the unified login resolves to that administrator, on every
// transport. Without a FIDO2 handler Enroll refuses before the server is asked.
func TestAdminFactors_PasskeyEnrollsThenLogsIn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	for _, tr := range allLoginTransports() {
		t.Run(string(tr), func(t *testing.T) {
			c, err := newLoginClient(t, tr, mtlsTLSConfig(t), adminCallerOptions(t, tr)...)
			require.NoError(t, err, "[%s] build client", tr)
			defer c.Close()

			adapter := webauthnclient.NewSoftwareAdapter()
			results, err := c.Security().Auth().AdminFactors().Enroll(ctx, &quicbaas.AdminFactorEnrollRequest{
				Factors:        []string{"fido2"},
				Subject:        adminSubject,
				TenantID:       systemTenantID,
				RelyingPartyID: adminFactorRPID,
				FIDO2Handler:   &softwarePasskey{adapter: adapter},
			})
			require.NoError(t, err, "[%s] fido2 admin enrollment", tr)
			require.Len(t, results, 1, "[%s] one enrollment", tr)
			require.Equal(t, qrdbsdk.FactorFIDO2, results[0].Factor, "[%s] enrolled factor", tr)
			credentialID := results[0].Enrollment.Metadata["credential_id"]
			require.NotEmpty(t, credentialID, "[%s] the server must report the stored credential", tr)

			finish, err := passkeyLogin(ctx, c, adapter)
			require.NoError(t, err, "[%s] login with the enrolled passkey", tr)
			require.NotEmpty(t, finish.Bearer, "[%s] login must mint a bearer", tr)
			require.NotNil(t, finish.Identity, "[%s] login must resolve an identity", tr)
			require.Equal(t, adminSubject, finish.Identity.Subject, "[%s] the passkey must log in as the admin", tr)
			require.Equal(t, systemTenantID, finish.Identity.TenantID, "[%s] resolved tenant", tr)

			_, err = c.Security().Auth().AdminFactors().Enroll(ctx, &quicbaas.AdminFactorEnrollRequest{
				Factors:        []string{"fido2"},
				Subject:        adminSubject,
				TenantID:       systemTenantID,
				RelyingPartyID: adminFactorRPID,
			})
			require.Equal(t, sdkerrors.CodePreconditionFailed, sdkerrors.Code(err),
				"[%s] fido2 without a handler must be refused at preflight, got %v", tr, err)
		})
	}
}

// A PIV key the administrator enrolls through AdminFactors().Enroll holds a
// certificate the server CA signed, and the unified login accepts a nonce
// signed by that key, on every transport. Any other key is refused.
func TestAdminFactors_PIVEnrollsThenLogsIn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	for _, tr := range allLoginTransports() {
		t.Run(string(tr), func(t *testing.T) {
			c, err := newLoginClient(t, tr, mtlsTLSConfig(t))
			require.NoError(t, err, "[%s] build client", tr)
			defer c.Close()

			mem, err := xkmssdk.NewMemoryBackend()
			require.NoError(t, err, "memory backend")
			daos, err := xkmssdk.NewBackendDAOFactory(mem)
			require.NoError(t, err, "DAO factory")
			slots, err := xkmssdk.NewPIVFileBackend(&xkmssdk.PIVFileStorageConfig{DAOs: daos})
			require.NoError(t, err, "PIV certificate store")
			card := xkmssdk.NewSoftwarePIVKeyGenerator()

			results, err := c.Security().Auth().AdminFactors().Enroll(ctx, &quicbaas.AdminFactorEnrollRequest{
				Factors:      []string{"piv"},
				Subject:      adminSubject,
				TenantID:     systemTenantID,
				PIVGenerator: card,
				PIVCertStore: slots,
			})
			require.NoError(t, err, "[%s] piv admin enrollment", tr)
			require.Len(t, results, 1, "[%s] one enrollment", tr)
			require.Equal(t, qrdbsdk.FactorPIV, results[0].Factor, "[%s] enrolled factor", tr)

			metadata := results[0].Enrollment.Metadata
			slot := xkmssdk.PIVSlot(metadata["slot"])
			slotCert, err := slots.Retrieve(slot)
			require.NoError(t, err, "[%s] slot %s must hold the issued certificate", tr, slot)
			signer, err := card.GetPIVSigner(slot, metadata["cn"])
			require.NoError(t, err, "[%s] card key", tr)
			lookupKey := sha256.Sum256(slotCert.Raw)
			slotMeta := map[string]string{"slot": metadata["slot"]}

			res, err := runSDKLogin(ctx, c, qrdbtransport.FactorTypePIV, adminSubject,
				lookupKey[:], signWith(signer), slotMeta)
			require.NoError(t, err, "[%s] login with the enrolled PIV key", tr)
			require.NotEmpty(t, res.bearer, "[%s] login must mint a bearer", tr)
			require.Equal(t, adminSubject, res.subject, "[%s] the PIV key must log in as the admin", tr)
			require.Equal(t, systemTenantID, res.tenant, "[%s] resolved tenant", tr)

			other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			require.NoError(t, err, "generate impostor key")
			_, err = runSDKLogin(ctx, c, qrdbtransport.FactorTypePIV, adminSubject,
				lookupKey[:], signWith(other), slotMeta)
			require.Error(t, err, "[%s] a key other than the card's must not log in", tr)
		})
	}
}
