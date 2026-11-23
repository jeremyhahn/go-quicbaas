// Package client re-exports go-xkms's ACME RFC 8555 client types, its ACME
// Renewal Information (ARI, RFC 9773) helpers and its cross-signing client.
// Every name is an alias of go-xkms's pkg/acme/client; go-quicbaas adds
// nothing. The ARI verbs themselves are Client methods (SupportsARI,
// GetRenewalInfo, CreateOrderWithReplaces), as are the cross-sign verbs
// (CrossSign, CrossSignWithChallenge).
package client

import (
	xkmsacme "github.com/jeremyhahn/go-xkms/sdk/go/acme"

	acmeclient "github.com/jeremyhahn/go-xkms/pkg/acme/client"
)

type (
	Client             = xkmsacme.ACMEClient
	CertificateRequest = xkmsacme.ACMECertificateRequest

	// Params and Config build a Client through NewClient.
	Params = acmeclient.Params
	Config = acmeclient.Config

	// RenewalInfo is the server's suggested renewal window for a
	// certificate (RFC 9773 section 4).
	RenewalInfo = xkmsacme.ACMERenewalInfo

	// CrossSignConfig configures a cross-signing client.
	CrossSignConfig = xkmsacme.ACMECrossSignConfig
)

var (
	// NewClient builds an ACME client.
	NewClient = acmeclient.NewClient

	// DefaultConfig is the client's default configuration.
	DefaultConfig = acmeclient.DefaultConfig

	// CrossSignerFromClient derives a cross-signing client from a client.
	CrossSignerFromClient = xkmsacme.ACMECrossSignerFromClient

	// GetCertificateID computes a certificate's ARI certificate ID:
	// base64url(AKI) "." base64url(serial).
	GetCertificateID = acmeclient.GetCertificateID

	// GetCertificateIDFromHash computes the ARI certificate ID with the AKI
	// derived from the issuer's key hash when the extension is absent.
	GetCertificateIDFromHash = acmeclient.GetCertificateIDFromHash

	// ParseCertificateID splits an ARI certificate ID into AKI and serial.
	ParseCertificateID = acmeclient.ParseCertificateID

	// ValidateCertificateID reports whether an ARI certificate ID is
	// well formed.
	ValidateCertificateID = acmeclient.ValidateCertificateID

	// BuildRenewalInfoURL joins the directory's renewalInfo URL and a
	// certificate ID.
	BuildRenewalInfoURL = acmeclient.BuildRenewalInfoURL

	// ComputeRenewalTime is the client-side renewal time when the server
	// offers no ARI window.
	ComputeRenewalTime = acmeclient.ComputeRenewalTime

	// NeedsRenewal reports whether a certificate is past its renewal
	// threshold.
	NeedsRenewal = acmeclient.NeedsRenewal

	// The auto-renewal error classifiers.
	IsAutoRenewalCanceled               = acmeclient.IsAutoRenewalCanceled
	IsAutoRenewalCancellationInvalid    = acmeclient.IsAutoRenewalCancellationInvalid
	IsAutoRenewalExpired                = acmeclient.IsAutoRenewalExpired
	IsAutoRenewalRevocationNotSupported = acmeclient.IsAutoRenewalRevocationNotSupported
)

// The ARI sentinel errors.
var (
	// ErrARINotSupported: the server's directory advertises no renewalInfo.
	ErrARINotSupported = acmeclient.ErrARINotSupported

	// ErrRenewalInfoFetchFailed: the renewalInfo request failed.
	ErrRenewalInfoFetchFailed = acmeclient.ErrRenewalInfoFetchFailed
)
