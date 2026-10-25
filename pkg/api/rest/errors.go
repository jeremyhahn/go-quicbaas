//go:build rest

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

package rest

import "errors"

// Sentinel errors for REST handler service availability checks.
var (
	// ErrSecretsServiceUnavailable indicates the secrets service is not available.
	ErrSecretsServiceUnavailable = errors.New("secrets service not available")

	// ErrStreamingNotSupported indicates streaming is not supported.
	ErrStreamingNotSupported = errors.New("streaming not supported")

	// ErrKMSServiceUnavailable indicates the KMS service is not available.
	ErrKMSServiceUnavailable = errors.New("KMS service not available")

	// ErrCryptoServiceUnavailable indicates the crypto service is not available.
	ErrCryptoServiceUnavailable = errors.New("crypto service not available")

	// ErrUnexpectedKMSResponse indicates an unexpected response type from KMS manager.
	ErrUnexpectedKMSResponse = errors.New("unexpected response type from KMS manager")

	// ErrKeyAttributesNil indicates key attributes cannot be nil.
	ErrKeyAttributesNil = errors.New("key attributes cannot be nil")

	// ErrDynamicSecretsUnavailable indicates the dynamic secrets service is not available.
	ErrDynamicSecretsUnavailable = errors.New("dynamic secrets service not available")

	// ErrKMSDEKServiceUnavailable indicates the KMS DEK service is not available.
	ErrKMSDEKServiceUnavailable = errors.New("KMS service not available")

	// ErrParameterStoreUnavailable indicates the parameter store service is not available.
	ErrParameterStoreUnavailable = errors.New("parameter store service not available")

	// ErrSchedulerServiceUnavailable indicates the scheduler service is not available.
	ErrSchedulerServiceUnavailable = errors.New("scheduler service not available")

	// ErrBarrierServiceUnavailable indicates the barrier facade is not yet
	// wired (e.g. before lifecycle setup completes). The handlers return
	// 503 Service Unavailable so callers can retry once the barrier is up.
	ErrBarrierServiceUnavailable = errors.New("barrier service not available")

	// ErrHolderClaimPeerCertRequired indicates a barrier holder share-claim
	// request arrived without a verified mTLS peer certificate. go-qrdb binds
	// a claim to the caller's certificate, so the handlers return 401
	// Unauthorized rather than forward an unbound claim.
	ErrHolderClaimPeerCertRequired = errors.New("share claim requires a verified mTLS client certificate")

	// ErrCeremonyServiceUnavailable indicates the ceremony facade is not
	// yet wired. The handlers return 503 Service Unavailable.
	ErrCeremonyServiceUnavailable = errors.New("ceremony service not available")

	// ErrBootstrapServiceUnavailable indicates the trust-bootstrap facade
	// is not yet wired. The handlers return 503 Service Unavailable.
	ErrBootstrapServiceUnavailable = errors.New("bootstrap service not available")

	// ErrTimeseriesServiceUnavailable indicates the timeseries service is not available.
	ErrTimeseriesServiceUnavailable = errors.New("timeseries service not available")

	// ErrCertificateServiceUnavailable indicates the certificate service is not available.
	ErrCertificateServiceUnavailable = errors.New("certificate service not available")

	// ErrTrustServiceUnavailable indicates the trust composition service
	// is not available (the server has no cross-sign / AIA capable CA).
	ErrTrustServiceUnavailable = errors.New("trust service not available")

	// ErrAttestationServiceUnavailable indicates the attestation service
	// is not available on this server build.
	ErrAttestationServiceUnavailable = errors.New("attestation service not available")

	// ErrRegistryServiceUnavailable indicates the service registry is not
	// configured on this server.
	ErrRegistryServiceUnavailable = errors.New("service registry not available")
)

// UnassignedRouteError refuses a RouteAuthRefused route: the verb is served
// by no permission go-qrdb or go-xkms assigns, so no grant can reach it.
type UnassignedRouteError struct {
	Method  string
	Pattern string
}

func (e *UnassignedRouteError) Error() string {
	return "no permission is assigned to REST route: " + e.Method + " " + e.Pattern
}
