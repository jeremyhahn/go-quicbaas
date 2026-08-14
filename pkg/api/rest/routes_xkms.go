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

import "github.com/go-chi/chi/v5"

// registerXKMSRoutes registers all xkms-compatible REST API routes.
// These routes expose the go-xkms XKMSService API surface at /api/v1/xkms/*
// so that the go-xkms SDK REST transport client can connect to quicbaasd.
func registerXKMSRoutes(s *Server, r chi.Router) {
	r.Route("/xkms", func(r chi.Router) {
		// Health
		r.Get("/health", s.HandleXKMSHealth)

		// Backend routes
		r.Get("/backends", s.HandleXKMSListBackends)
		r.Get("/backends/{id}", s.HandleXKMSGetBackend)

		// Key management routes
		r.Post("/keys", s.HandleXKMSGenerateKey)
		r.Get("/keys", s.HandleXKMSListKeys)
		r.Get("/keys/{id}", s.HandleXKMSGetKey)
		r.Delete("/keys/{id}", s.HandleXKMSDeleteKey)
		r.Post("/keys/{id}/rotate", s.HandleXKMSRotateKey)

		// Crypto routes (sign, verify, encrypt, decrypt)
		r.Post("/keys/{id}/sign", s.HandleXKMSSign)
		r.Post("/keys/{id}/verify", s.HandleXKMSVerify)
		r.Post("/keys/{id}/encrypt", s.HandleXKMSEncrypt)
		r.Post("/keys/{id}/decrypt", s.HandleXKMSDecrypt)

		// Key version routes
		r.Get("/keys/{id}/versions", s.HandleXKMSListKeyVersions)
		r.Post("/keys/{id}/versions/{version}/enable", s.HandleXKMSEnableKeyVersion)
		r.Post("/keys/{id}/versions/{version}/disable", s.HandleXKMSDisableKeyVersion)
		r.Post("/keys/{id}/versions/enable-all", s.HandleXKMSEnableAllKeyVersions)
		r.Post("/keys/{id}/versions/disable-all", s.HandleXKMSDisableAllKeyVersions)

		// Certificate routes
		r.Post("/certs", s.HandleXKMSSaveCert)
		r.Get("/certs", s.HandleXKMSListCerts)
		r.Get("/certs/{id}", s.HandleXKMSGetCert)
		r.Delete("/certs/{id}", s.HandleXKMSDeleteCert)
		r.Head("/certs/{id}", s.HandleXKMSCertExists)

		// Seal routes
		r.Post("/seal", s.HandleXKMSSeal)
		r.Post("/unseal", s.HandleXKMSUnseal)
		r.Get("/seal/capability", s.HandleXKMSCanSeal)

		// FIDO2 routes (tenant-scoped WebAuthn ceremony over the xkms
		// FIDO2Service wire shape; authz-gated — pre-auth passkey login
		// rides the canonical public login surfaces instead)
		r.Post("/fido2/register/begin", s.HandleXKMSFIDO2BeginRegistration)
		r.Post("/fido2/register/finish", s.HandleXKMSFIDO2FinishRegistration)
		r.Post("/fido2/authenticate/begin", s.HandleXKMSFIDO2BeginAuthentication)
		r.Post("/fido2/authenticate/finish", s.HandleXKMSFIDO2FinishAuthentication)

		// PIV routes
		r.Get("/piv/slots", s.HandleXKMSListPIVSlots)
		r.Get("/piv/slots/{slot}/cert", s.HandleXKMSGetPIVCertificate)
		r.Put("/piv/slots/{slot}/cert", s.HandleXKMSStorePIVCertificate)
		r.Delete("/piv/slots/{slot}/cert", s.HandleXKMSDeletePIVCertificate)
		r.Post("/piv/slots/{slot}/key", s.HandleXKMSGeneratePIVKey)
		r.Post("/piv/slots/{slot}/csr", s.HandleXKMSGeneratePIVCSR)

		// CA routes
		r.Get("/ca/bundle", s.HandleXKMSGetCABundle)
		r.Get("/ca/cert", s.HandleXKMSGetCACertificate)
		r.Post("/ca/sign", s.HandleXKMSSignCSR)
		r.Post("/ca/issue", s.HandleXKMSIssueCertificate)
		r.Post("/ca/revoke", s.HandleXKMSRevokeCertificate)
		r.Post("/ca/crl", s.HandleXKMSGenerateCRL)
		r.Get("/ca/revoked/{serial}", s.HandleXKMSIsRevoked)

		// PIN routes
		r.Post("/pin/so/set", s.HandleXKMSSetSOPIN)
		r.Post("/pin/user/set", s.HandleXKMSSetUserPIN)
		r.Post("/pin/so/change", s.HandleXKMSChangeSOPIN)
		r.Post("/pin/user/change", s.HandleXKMSChangeUserPIN)
		r.Post("/pin/so/verify", s.HandleXKMSVerifySOPIN)
		r.Post("/pin/user/verify", s.HandleXKMSVerifyUserPIN)
		r.Get("/pin/lockout", s.HandleXKMSGetLockoutStatus)
		r.Post("/pin/lockout/reset", s.HandleXKMSResetLockout)
	})
}
