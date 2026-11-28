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

package server

import (
	"context"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-qrdb/pkg/service"
)

// errQRDBCredentialNotConfigured is the answer every credential call gives
// while no credential servicer is published. go-qrdb classifies the typed
// CapabilityUnsupportedError as Unimplemented on every transport.
var errQRDBCredentialNotConfigured error = &service.CapabilityUnsupportedError{Message: "credential service is not configured"}

// qrdbCredentialServicerProxy is the qrdb.v1 credential servicer registered
// on the seal-off node's transports. It loads the servicer published by
// startService on every call, because the listeners are built before the
// bootstrap credential service exists.
type qrdbCredentialServicerProxy struct {
	s *Server
}

var _ qrdbsdk.ServiceCredentialServicer = (*qrdbCredentialServicerProxy)(nil)

func newQRDBCredentialServicerProxy(s *Server) *qrdbCredentialServicerProxy {
	return &qrdbCredentialServicerProxy{s: s}
}

// credentialServicerRef boxes the published credential servicer so it can
// sit in an atomic.Pointer.
type credentialServicerRef struct {
	servicer qrdbsdk.ServiceCredentialServicer
}

// publishCredentialServicer makes cs the servicer the credential proxy
// serves; a nil cs clears it.
func (s *Server) publishCredentialServicer(cs qrdbsdk.ServiceCredentialServicer) {
	if cs == nil {
		s.credentialServicer.Store(nil)
		return
	}
	s.credentialServicer.Store(&credentialServicerRef{servicer: cs})
}

// servicer returns the credential servicer published at the time of the
// call, or errQRDBCredentialNotConfigured when there is none.
func (p *qrdbCredentialServicerProxy) servicer() (qrdbsdk.ServiceCredentialServicer, error) {
	if p.s == nil {
		return nil, errQRDBCredentialNotConfigured
	}
	if ref := p.s.credentialServicer.Load(); ref != nil && ref.servicer != nil {
		return ref.servicer, nil
	}
	return nil, errQRDBCredentialNotConfigured
}

func (p *qrdbCredentialServicerProxy) CredentialStore(ctx context.Context, name string, value []byte) error {
	cs, err := p.servicer()
	if err != nil {
		return err
	}
	return cs.CredentialStore(ctx, name, value)
}

func (p *qrdbCredentialServicerProxy) CredentialRetrieve(ctx context.Context, name string) ([]byte, error) {
	cs, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return cs.CredentialRetrieve(ctx, name)
}

func (p *qrdbCredentialServicerProxy) CredentialSubmit(ctx context.Context, name string, value []byte) error {
	cs, err := p.servicer()
	if err != nil {
		return err
	}
	return cs.CredentialSubmit(ctx, name, value)
}

func (p *qrdbCredentialServicerProxy) CredentialDelete(ctx context.Context, name string) error {
	cs, err := p.servicer()
	if err != nil {
		return err
	}
	return cs.CredentialDelete(ctx, name)
}

// CredentialStrategy returns "" while no credential servicer is published.
func (p *qrdbCredentialServicerProxy) CredentialStrategy() string {
	cs, err := p.servicer()
	if err != nil {
		return ""
	}
	return cs.CredentialStrategy()
}

func (p *qrdbCredentialServicerProxy) CredentialList(ctx context.Context) ([]string, error) {
	cs, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return cs.CredentialList(ctx)
}

func (p *qrdbCredentialServicerProxy) CredentialRotate(ctx context.Context, name string, opts qrdbsdk.RotateOptions) (*qrdbsdk.RotateResult, error) {
	cs, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return cs.CredentialRotate(ctx, name, opts)
}

func (p *qrdbCredentialServicerProxy) CredentialRetrieveVersion(ctx context.Context, name string, version uint64) ([]byte, error) {
	cs, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return cs.CredentialRetrieveVersion(ctx, name, version)
}

func (p *qrdbCredentialServicerProxy) CredentialListVersions(ctx context.Context, name string) ([]qrdbsdk.CredentialVersion, error) {
	cs, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return cs.CredentialListVersions(ctx, name)
}

func (p *qrdbCredentialServicerProxy) CredentialGCExpired(ctx context.Context, name string) (int, error) {
	cs, err := p.servicer()
	if err != nil {
		return 0, err
	}
	return cs.CredentialGCExpired(ctx, name)
}
