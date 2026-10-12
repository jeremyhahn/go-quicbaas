// Package http01 re-exports go-xkms's client-side ACME HTTP-01 challenge
// responder: the short-lived HTTP server an ACME client runs so the
// directory can fetch the key authorization.
package http01

import xkmsacme "github.com/jeremyhahn/go-xkms/sdk/go/acme"

// Responder serves the key authorization for one http-01 challenge.
type Responder = xkmsacme.HTTP01Responder

// ResponderConfig configures a Responder.
type ResponderConfig = xkmsacme.HTTP01ResponderConfig

// ResponderConfigError reports a ResponderConfig field that cannot be used.
type ResponderConfigError = xkmsacme.HTTP01ResponderConfigError

// ResponderBindError reports that the responder could not take its address.
type ResponderBindError = xkmsacme.HTTP01ResponderBindError

// ResponderShutdownError reports a graceful shutdown that missed its deadline.
type ResponderShutdownError = xkmsacme.HTTP01ResponderShutdownError

// NewResponder binds a listener and starts serving the key authorization at
// the well-known http-01 challenge path.
var NewResponder = xkmsacme.NewHTTP01Responder

const (
	DefaultShutdownTimeout = xkmsacme.DefaultHTTP01ResponderShutdownTimeout
	DefaultReadTimeout     = xkmsacme.DefaultHTTP01ResponderReadTimeout
	DefaultWriteTimeout    = xkmsacme.DefaultHTTP01ResponderWriteTimeout
	DefaultIdleTimeout     = xkmsacme.DefaultHTTP01ResponderIdleTimeout
)
