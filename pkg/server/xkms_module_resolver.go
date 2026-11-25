package server

import (
	xkmslib "github.com/jeremyhahn/go-xkms/pkg/xkms"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// xkmsServiceForModule is the go-xkms ServiceModule's XKMS service resolver.
//
// go-xkms builds its keystore gRPC service on the first resolve that yields
// an XKMS service, binds the service's CA only if one is already present,
// and caches the result for the life of the process. The node's listeners
// start before initializeCA adopts the CA, so a call in that window would
// cache a keystore with no CA, and SignCSR and GetCACertificate would stay
// unavailable. The resolver therefore yields nothing (the module answers
// Unavailable and caches nothing) until initializeCA has returned. After
// that it binds an adopted CA the XKMS service does not hold yet, which
// covers a CA adopted before the XKMS service resolved.
func (s *Server) xkmsServiceForModule() *xkmslib.XKMSService {
	if !s.caSettled.Load() {
		return nil
	}
	svc, err := xkmssdk.Get()
	if err != nil || svc == nil {
		return nil
	}
	if svc.CA() == nil {
		s.bindCAToXKMSService()
	}
	return svc
}
