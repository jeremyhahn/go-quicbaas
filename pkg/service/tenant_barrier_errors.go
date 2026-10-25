package service

import (
	"strconv"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// TenantBarrierInitStatementUnsupportedError refuses a tenant barrier init
// that would reach go-qrdb without the barrier owner's signed init statement
// for a raft-isolated tenant. go-qrdb builds that statement from
// TenantBarrierInitRequest.Signer, a crypto.Signer that no remote caller can
// send: go-qrdb tags it json:"-", and its network init/begin + init/complete
// proof (transport.ProveTenantBarrierInit) signs the TLS exporter of the
// caller's own connection to the qrdb node, which a quicbaas transport does
// not have. go-qrdb sdk/go publishes no wire form for a pre-signed statement,
// so the remote transports refuse here instead of calling init without one.
// The supported path is the CLI's `quicbaas iam tenant barrier init`: its go-qrdb
// SDK tenant client signs the statement with the caller's own mTLS key.
type TenantBarrierInitStatementUnsupportedError struct {
	TenantID string
}

func (e *TenantBarrierInitStatementUnsupportedError) Error() string {
	return "tenant barrier init for " + strconv.Quote(e.TenantID) +
		" is not supported on this transport: the tenant is raft-isolated, so go-qrdb needs the" +
		" barrier owner's signed init statement, and go-qrdb sdk/go has no wire form for it" +
		" (TenantBarrierInitRequest.Signer is not serialized); run `quicbaas iam tenant barrier init`," +
		" which signs it with your mTLS key"
}

// ErrorCode classes the refusal as not implemented, so every transport
// answers it as unsupported with this text rather than as a server failure.
func (e *TenantBarrierInitStatementUnsupportedError) ErrorCode() errors.ErrorCode {
	return errors.ErrNotImplemented
}
