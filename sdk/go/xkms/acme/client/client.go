// Package client re-exports go-xkms's ACME RFC 8555 client types.
package client

import xkmsac "github.com/jeremyhahn/go-xkms/pkg/acme/client"

type (
	Client             = xkmsac.Client
	CertificateRequest = xkmsac.CertificateRequest
)
