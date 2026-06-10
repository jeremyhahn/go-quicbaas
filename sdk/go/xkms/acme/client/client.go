// Package client re-exports go-xkms's ACME RFC 8555 client types.
package client

import xkmsacme "github.com/jeremyhahn/go-xkms/sdk/go/acme"

type (
	Client             = xkmsacme.ACMEClient
	CertificateRequest = xkmsacme.ACMECertificateRequest
)
