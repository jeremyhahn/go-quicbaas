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

// Package certificate provides the certificate management client for the QuicBaaS SDK.
//
// The Certificate client supports X.509 certificate lifecycle management including
// issuance, signing, revocation, and verification of certificates.
//
// # Certificate Issuance
//
//	client := certificate.NewClient(transport)
//
//	// Issue a new server certificate
//	resp, err := client.IssueCertificate(ctx, &certificate.IssueCertificateRequest{
//	    CommonName:   "api.example.com",
//	    Organization: "Example Corp",
//	    DNSNames:     []string{"api.example.com", "*.api.example.com"},
//	    Usage:        "server",
//	    ValidityDays: 365,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Certificate issued: %s\n", resp.SerialNumber)
//
// # CSR Signing
//
// Sign a Certificate Signing Request:
//
//	resp, err := client.SignCSR(ctx, &certificate.SignCSRRequest{
//	    CSRPEM:       csrPEM,
//	    ValidityDays: 365,
//	    Usage:        "client",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// # Certificate Verification
//
// Verify a certificate against the CA:
//
//	resp, err := client.VerifyCertificate(ctx, &certificate.VerifyCertificateRequest{
//	    CertificatePEM: certPEM,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	if !resp.Valid {
//	    fmt.Printf("Certificate invalid: %s\n", resp.ErrorMessage)
//	}
//
// # Certificate Revocation
//
// Revoke a compromised or expired certificate:
//
//	resp, err := client.RevokeCertificate(ctx, &certificate.RevokeCertificateRequest{
//	    SerialNumber: serialNumber,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// # CA Certificate
//
// Retrieve the CA certificate for trust configuration:
//
//	resp, err := client.GetCACertificate(ctx, &certificate.GetCACertificateRequest{})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	caCert := resp.CACertificatePEM
//
// # TLS Configuration
//
// Get a pre-configured TLS configuration:
//
//	resp, err := client.GetTLSConfig(ctx, &certificate.GetTLSConfigRequest{
//	    ServerName:        "api.example.com",
//	    RequireClientCert: true,
//	    IsServer:          true,
//	    MinVersion:        "1.3",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	tlsConfig := resp.Config
//
// # Certificate Usage Types
//
// Certificates can be issued for specific purposes:
//   - "client": Client authentication only
//   - "server": Server authentication only
//   - "both": Both client and server authentication
//
// # Key Algorithms
//
// Supported key algorithms for certificate generation:
//   - "ecdsa": ECDSA (default, recommended)
//   - "rsa": RSA (specify KeySize for 2048, 4096, etc.)
package certificate
