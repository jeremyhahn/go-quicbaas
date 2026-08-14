//go:build quic

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

// QRDB wire-format dispatch over QUIC.
//
// The qrdb SDK QUIC client uses a different wire protocol than QuicBaaS's
// native opcode protocol: framed [methodLen][method][requestID][payloadLen]
// [payload] with protobuf payloads. To serve qrdb RPCs against the
// quicbaasd QUIC listener with no SDK changes, this file implements the
// qrdb wire format on a per-connection basis when the connection negotiated
// the "qrdb-quic" ALPN.
//
// The method families riding this ALPN are the connect-time health probe
// (admin.Health), the barrier / ceremony lifecycle surface, the versioned
// credential surface, and the user-lookup + user-login-factor surface
// (handler_qrdb_users.go). Each delegates to the SAME service facades the
// gRPC and REST transports use, so behaviour is identical across every
// transport.

package quic

import (
	"context"
	"crypto/x509"
	"io"
	"runtime/debug"
	"time"

	quicgo "github.com/quic-go/quic-go"
	"google.golang.org/protobuf/proto"

	pb "github.com/jeremyhahn/go-qrdb/pkg/api/grpc/proto/qrdbv1"
	"github.com/jeremyhahn/go-qrdb/pkg/api/quic/wire"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// QRDBALPN is the ALPN identifier the qrdb QUIC SDK client negotiates.
// quicbaasd advertises this alongside "quicbaas-quic" so the qrdb SDK
// QUIC transport can reach the credential management surface without
// changing its hard-coded ALPN.
const QRDBALPN = "qrdb-quic"

// qrdbMethodAdminHealth is the qrdb-quic connect-time transport liveness
// probe. It is public on every transport (REST /health, gRPC Health, native
// QUIC OpHealth) and is therefore exempt from the verified-peer-certificate
// requirement below: the qrdb SDK client runs it at connect time and tears
// the connection down if it fails, and it exposes no data.
const qrdbMethodAdminHealth = "admin.Health"

// qrdbHealthStatusServing is the health status the connect-time probe
// reports back to the qrdb QUIC client. The client only requires a
// non-empty Status to consider the connection live; reporting "serving"
// keeps the wire shape consistent with go-qrdb's own admin health output.
const qrdbHealthStatusServing = "serving"

// qrdbMethodHandler is the function signature for qrdb wire-format
// dispatch. It mirrors qrdb's pkg/api/quic.methodHandler but lives in
// the quicbaas package so the QUIC server can route based on the
// connection's negotiated ALPN.
type qrdbMethodHandler func(ctx context.Context, payload []byte) ([]byte, error)

// QRDBCredentialNotConfiguredError indicates a credential RPC arrived on
// the qrdb-quic ALPN but no CredentialServicer was wired into the QUIC
// server (SetQRDBCredentialService not called or called with nil).
type QRDBCredentialNotConfiguredError struct{}

// Error implements the error interface.
func (*QRDBCredentialNotConfiguredError) Error() string {
	return "qrdb credential service not configured"
}

// QRDBBarrierNotConfiguredError indicates a barrier RPC arrived on the
// qrdb-quic ALPN but no BarrierServicer was wired into the QUIC server.
type QRDBBarrierNotConfiguredError struct{}

// Error implements the error interface.
func (*QRDBBarrierNotConfiguredError) Error() string {
	return "qrdb barrier service not configured"
}

// QRDBCeremonyNotConfiguredError indicates a ceremony RPC arrived on the
// qrdb-quic ALPN but no CeremonyServicer was wired into the QUIC server.
type QRDBCeremonyNotConfiguredError struct{}

// Error implements the error interface.
func (*QRDBCeremonyNotConfiguredError) Error() string {
	return "qrdb ceremony service not configured"
}

// QRDBUnknownMethodError indicates the qrdb wire frame referenced a
// method name that is not implemented on the quicbaasd qrdb-quic dispatch
// table.
type QRDBUnknownMethodError struct {
	Method string
}

// Error implements the error interface.
func (e *QRDBUnknownMethodError) Error() string {
	return "qrdb-quic: unknown method: " + e.Method
}

// QRDBAuthRequiredError indicates a qrdb-quic bridge method was rejected
// because authorization enforcement is enabled (RequireAuthz=true) and the
// connection carries no VERIFIED mTLS client certificate. The qrdb wire
// format carries no bearer token (qrdb-owned contract), so a verified
// client certificate on the QUIC connection is the only per-caller
// credential the bridge can enforce post-unseal. Written to the wire as
// StatusUnauthenticated.
type QRDBAuthRequiredError struct {
	Method string
}

// Error implements the error interface.
func (e *QRDBAuthRequiredError) Error() string {
	return "qrdb-quic: authentication required: method " + e.Method +
		" requires a verified mTLS client certificate on the connection (RequireAuthz is enabled)"
}

// QRDBUnmarshalError wraps a protobuf decode failure on a qrdb wire-format
// request payload.
type QRDBUnmarshalError struct {
	Err error
}

// Error implements the error interface.
func (e *QRDBUnmarshalError) Error() string {
	if e.Err == nil {
		return "qrdb-quic: payload unmarshal failed"
	}
	return "qrdb-quic: payload unmarshal failed: " + e.Err.Error()
}

// Unwrap exposes the underlying decode error for errors.Is/As traversal.
func (e *QRDBUnmarshalError) Unwrap() error { return e.Err }

// QRDBMarshalError wraps a protobuf encode failure on a qrdb wire-format
// response payload.
type QRDBMarshalError struct {
	Err error
}

// Error implements the error interface.
func (e *QRDBMarshalError) Error() string {
	if e.Err == nil {
		return "qrdb-quic: response marshal failed"
	}
	return "qrdb-quic: response marshal failed: " + e.Err.Error()
}

// Unwrap exposes the underlying encode error for errors.Is/As traversal.
func (e *QRDBMarshalError) Unwrap() error { return e.Err }

// buildQRDBHandlers returns the dispatch table for qrdb-quic methods.
//
// The qrdb SDK QUIC client emits several families of methods against this
// ALPN: a connect-time health probe (admin.Health), the barrier and
// ceremony lifecycle surface, the versioned credential surface, and the
// user-lookup + user-login-factor surface. Each
// handler marshals/unmarshals the canonical qrdbv1 protobuf shapes the
// qrdb SDK client expects and delegates to the matching QuicBaaS service
// facade — the SAME facades the gRPC and REST transports use, so behaviour
// is identical across every transport.
//
// Gating is per-method, not table-wide: admin.Health always succeeds (it
// is a transport liveness probe), credential.* require the credential
// facade, and barrier.* / ceremony.* require their respective facades.
// This is essential — the qrdb client runs admin.Health at connect time
// and tears the connection down if it fails, so the probe must never be
// blocked by an unrelated facade being nil.
//
// AUTHORIZATION POSTURE (owner-approved, fail-closed): the qrdb wire frame
// (go-qrdb/pkg/api/quic/wire.ReadRequest: method, database, requestID,
// payload) carries NO bearer token — a qrdb-owned contract this bridge does
// not change — so it cannot run the per-method platform-token PEP the
// native opcode path runs in authorizeAndInject. Enforcement on this ALPN
// is therefore:
//
//   - RequireAuthz=true (SetValidatorEnforcement installed): every method
//     dispatch except the public admin.Health liveness probe REQUIRES a
//     VERIFIED mTLS client certificate on the connection
//     (qrdbBridgeAuthRequired); cert-less callers are rejected with the
//     wire's StatusUnauthenticated / QRDBAuthRequiredError. This holds
//     even when the operator runs Security.TLSClientAuth=false, where the
//     TLS handshake merely solicits a certificate
//     (VerifyClientCertIfGiven) — lifecycle logs the cert-gated posture at
//     startup for that combination. The sealed-window unseal flow is
//     unaffected: while sealed, the allowlisted methods are admitted
//     without a certificate via the shared sealed-window bypass (audited),
//     because a sealed IAM plane cannot mint or validate any credential.
//   - RequireAuthz=false: legacy behaviour unchanged — connection-level
//     TLS client-certificate policy only, matching qrdb's own QUIC posture
//     where possession of a verified mTLS identity is the authorization.
//   - In both postures the sealed-window gate
//     (sealedWindowDeniesQRDBMethod) narrows the surface to the
//     unseal-flow allowlist while sealed.
func (s *Server) buildQRDBHandlers() map[string]qrdbMethodHandler {
	return map[string]qrdbMethodHandler{
		// Transport liveness probe (qrdb client connect-time health check).
		qrdbMethodAdminHealth: s.qrdbAdminHealth,

		// Credential management surface.
		"credential.Submit":          s.qrdbCredentialSubmit,
		"credential.Strategy":        s.qrdbCredentialStrategy,
		"credential.Delete":          s.qrdbCredentialDelete,
		"credential.List":            s.qrdbCredentialList,
		"credential.Rotate":          s.qrdbCredentialRotate,
		"credential.RetrieveVersion": s.qrdbCredentialRetrieveVersion,
		"credential.ListVersions":    s.qrdbCredentialListVersions,
		"credential.GCExpired":       s.qrdbCredentialGCExpired,

		// Barrier lifecycle surface.
		"barrier.Initialize":         s.qrdbBarrierInitialize,
		"barrier.Unseal":             s.qrdbBarrierUnseal,
		"barrier.Seal":               s.qrdbBarrierSeal,
		"barrier.Status":             s.qrdbBarrierStatus,
		"barrier.ShamirInitialize":   s.qrdbBarrierShamirInitialize,
		"barrier.ShamirUnsealShare":  s.qrdbBarrierShamirUnsealShare,
		"barrier.ShamirUnsealShares": s.qrdbBarrierShamirUnsealShares,
		"barrier.Rekey":              s.qrdbBarrierRekey,
		"barrier.RecoveryGenerate":   s.qrdbBarrierRecoveryGenerate,
		"barrier.RecoveryRecover":    s.qrdbBarrierRecoveryRecover,
		"barrier.RecoveryDelete":     s.qrdbBarrierRecoveryDelete,
		"barrier.RecoveryStatus":     s.qrdbBarrierRecoveryStatus,
		"barrier.RootToken":          s.qrdbBarrierRootToken,
		"barrier.EnableEncryption":   s.qrdbBarrierEnableEncryption,

		// Per-tenant barrier lifecycle surface.
		"tenant.BarrierInit":             s.qrdbTenantBarrierInit,
		"tenant.BarrierUnseal":           s.qrdbTenantBarrierUnseal,
		"tenant.BarrierUnsealShare":      s.qrdbTenantBarrierUnsealShare,
		"tenant.BarrierSeal":             s.qrdbTenantBarrierSeal,
		"tenant.BarrierStatus":           s.qrdbTenantBarrierStatus,
		"tenant.BarrierEnableEncryption": s.qrdbTenantBarrierEnableEncryption,
		"tenant.UpdateQuotas":            s.qrdbTenantUpdateQuotas,
		"tenant.UpdateEntitlements":      s.qrdbTenantUpdateEntitlements,

		// User lookup surface (read-only resolution; JSON payloads).
		"user.Get":                 s.qrdbUserGet,
		"user.GetByUsername":       s.qrdbUserGetByUsername,
		"user.GetByCertThumbprint": s.qrdbUserGetByCertThumbprint,

		// User-login factor surface (JSON payloads).
		"userauthfactor.Add":     s.qrdbUserFactorAdd,
		"userauthfactor.List":    s.qrdbUserFactorList,
		"userauthfactor.Get":     s.qrdbUserFactorGet,
		"userauthfactor.Remove":  s.qrdbUserFactorRemove,
		"userauthfactor.Rotate":  s.qrdbUserFactorRotate,
		"userauthfactor.Relabel": s.qrdbUserFactorRelabel,
		"userauthfactor.Login":   s.qrdbUserFactorLogin,

		// Initialization ceremony surface.
		"ceremony.Initialize":            s.qrdbCeremonyInitialize,
		"ceremony.Status":                s.qrdbCeremonyStatus,
		"ceremony.ClaimCertBegin":        s.qrdbCeremonyClaimCertBegin,
		"ceremony.ClaimCertComplete":     s.qrdbCeremonyClaimCertComplete,
		"ceremony.BeginClaimShare":       s.qrdbCeremonyBeginClaimShare,
		"ceremony.CompleteClaimShare":    s.qrdbCeremonyCompleteClaimShare,
		"ceremony.SignCSR":               s.qrdbCeremonySignCSR,
		"ceremony.EnrollExternalOfficer": s.qrdbCeremonyEnrollExternalOfficer,
	}
}

// handleQRDBStream serves a single QUIC stream for the qrdb-quic ALPN.
// peerCert is the VERIFIED mTLS client certificate captured once per
// connection from the TLS handshake's VerifiedChains (nil when the caller
// presented none), threaded here so qrdbBridgeAuthRequired can enforce the
// fail-closed peer-certificate requirement under RequireAuthz. Stream
// lifecycle (waitgroup / gauge / close) lives here; the wire-format request
// cycle is delegated to serveQRDBRequest.
func (s *Server) handleQRDBStream(stream *quicgo.Stream, peerCert *x509.Certificate) {
	defer s.wg.Done()
	defer s.activeStreams.Add(-1)
	defer func() { _ = stream.Close() }()

	s.serveQRDBRequest(stream, peerCert)
}

// qrdbBridgeAuthRequired reports whether the qrdb-quic bridge must reject
// method with the wire's auth-required status because authorization
// enforcement is enabled and the connection carries no VERIFIED mTLS client
// certificate.
//
// The posture signal is the installed validator (SetValidatorEnforcement is
// called by lifecycle ONLY when RequireAuthz=true); with RequireAuthz=false
// no validator is installed and the legacy connection-level behaviour is
// unchanged. The qrdb wire contract carries no bearer token, so a verified
// mTLS identity on the connection is the only per-caller credential
// available post-unseal — enforcement fail-closes on its absence.
//
// Exemptions, mirroring every other transport gate:
//
//   - admin.Health: public transport liveness probe (public on REST, gRPC
//     and the native opcode path alike);
//   - the sealed-window allowlist WHILE SEALED: the shared bypass predicate
//     admits the unseal-flow methods without a certificate (audited, reason
//     "sealed-window allowlist") — a sealed IAM plane cannot mint or
//     validate any credential. Unsealed, the bypass is inert. The
//     sealed-window deny gate runs BEFORE this check, so while sealed every
//     non-allowlisted method has already been rejected.
func (s *Server) qrdbBridgeAuthRequired(method string, peerCert *x509.Certificate) bool {
	if s.validatorAuthorizer == nil {
		// RequireAuthz=false: legacy connection-level posture unchanged.
		return false
	}
	if peerCert != nil {
		return false
	}
	if method == qrdbMethodAdminHealth {
		return false
	}
	s.svcMu.RLock()
	bypass := s.sealedBypass
	s.svcMu.RUnlock()
	return !bypass.BypassesQRDBQUIC(method)
}

// serveQRDBRequest reads one qrdb wire-format request from rw, dispatches to
// the appropriate handler, and writes the response. Panics are recovered and
// reported as wire-level errors so a misbehaving handler does not crash the
// listener. It is factored out of handleQRDBStream against io.ReadWriter so
// the full dispatch order (sealed gate -> peer-cert requirement -> handler)
// is unit-testable without a live QUIC stream.
//
// Facade gating is performed per-method inside each handler, not table-wide.
// admin.Health in particular must always be servable: the qrdb SDK client
// runs it at connect time and closes the connection if it fails, which would
// in turn fail every subsequent barrier/ceremony/credential RPC on that
// connection.
func (s *Server) serveQRDBRequest(rw io.ReadWriter, peerCert *x509.Certificate) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.WithFields(map[string]interface{}{
				"panic": r,
				"stack": string(debug.Stack()),
			}).Error("qrdb-quic: panic recovered")
			_ = wire.WriteResponse(rw, 0, wire.StatusError, []byte("internal server error"))
		}
	}()

	// qrdb's wire frame carries a `database` field (added for KV routing). These
	// control-plane bridge methods (credentials / tenant-barrier) are tenant-scoped
	// via the peer cert + request context, not database-scoped, so the wire
	// database name is intentionally not consulted here.
	method, _, requestID, payload, err := wire.ReadRequest(rw)
	if err != nil {
		s.totalErrors.Add(1)
		s.logger.WithError(err).Debug("qrdb-quic: failed to read request")
		return
	}

	start := time.Now()
	s.totalRequests.Add(1)

	// Sealed-window gate: while the barrier is sealed, every bridge method
	// not on the canonical allowlist is rejected with StatusUnavailable so
	// operators can still drive the unseal flow (barrier.* / ceremony.*)
	// and the connect-time liveness probe (admin.Health). Mirrors qrdb's
	// own native QUIC sealed-window gate.
	if s.sealedWindowDeniesQRDBMethod(method) {
		_ = wire.WriteResponse(rw, requestID, wire.StatusUnavailable, []byte(sealedWindowMessage))
		s.logQRDBRequest(method, requestID, wire.StatusUnavailable, start)
		return
	}

	// Fail-closed peer-certificate requirement (RequireAuthz=true): the qrdb
	// wire carries no token, so post-unseal method dispatch demands a
	// VERIFIED mTLS client certificate on the connection. Sealed-window
	// allowlisted methods stay admitted without one via the shared bypass
	// (audited), and admin.Health stays public. See qrdbBridgeAuthRequired.
	if s.qrdbBridgeAuthRequired(method, peerCert) {
		errPayload := (&QRDBAuthRequiredError{Method: method}).Error()
		_ = wire.WriteResponse(rw, requestID, wire.StatusUnauthenticated, []byte(errPayload))
		s.logQRDBRequest(method, requestID, wire.StatusUnauthenticated, start)
		return
	}

	handlers := s.buildQRDBHandlers()
	handler, ok := handlers[method]
	if !ok {
		errPayload := (&QRDBUnknownMethodError{Method: method}).Error()
		_ = wire.WriteResponse(rw, requestID, wire.StatusUnimplemented, []byte(errPayload))
		s.logQRDBRequest(method, requestID, wire.StatusUnimplemented, start)
		return
	}

	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()

	resp, handlerErr := handler(ctx, payload)
	if handlerErr != nil {
		_ = wire.WriteResponse(rw, requestID, wire.StatusError, []byte(handlerErr.Error()))
		s.logQRDBRequest(method, requestID, wire.StatusError, start)
		return
	}

	if writeErr := wire.WriteResponse(rw, requestID, wire.StatusOK, resp); writeErr != nil {
		s.totalErrors.Add(1)
		s.logger.WithError(writeErr).Error("qrdb-quic: failed to write response")
	}
	s.logQRDBRequest(method, requestID, wire.StatusOK, start)
}

// logQRDBRequest emits a single debug line for a completed qrdb-quic
// request, mirroring the upstream qrdb QUIC server's logging shape.
func (s *Server) logQRDBRequest(method string, requestID uint64, status uint16, start time.Time) {
	s.logger.WithFields(map[string]interface{}{
		"method":     method,
		"request_id": requestID,
		"status":     status,
		"duration":   time.Since(start),
		"alpn":       QRDBALPN,
	}).Debug("qrdb-quic: request")
}

// =============================================================================
// Facade accessors (snapshot under svcMu)
// =============================================================================

// qrdbCredentialService returns the currently wired credential facade, or
// nil if SetQRDBCredentialService has not been called with a non-nil value.
func (s *Server) qrdbCredentialService() qrdbsdk.ServiceCredentialServicer {
	s.svcMu.RLock()
	defer s.svcMu.RUnlock()
	return s.qrdbCredentialSvc
}

// qrdbBarrierService returns the currently wired barrier facade, or nil if
// SetBarrierService has not been called with a non-nil value.
func (s *Server) qrdbBarrierService() service.BarrierServicer {
	s.svcMu.RLock()
	defer s.svcMu.RUnlock()
	return s.barrierService
}

// qrdbCeremonyService returns the currently wired ceremony facade, or nil
// if SetCeremonyService has not been called with a non-nil value.
func (s *Server) qrdbCeremonyService() service.CeremonyServicer {
	s.svcMu.RLock()
	defer s.svcMu.RUnlock()
	return s.ceremonyService
}

// =============================================================================
// Admin handlers (qrdb wire format)
// =============================================================================

// qrdbAdminHealth answers the qrdb SDK client's connect-time health probe.
// quicbaasd is the in-process composed node fronting the qrdb-quic ALPN, so
// it always reports serving. The qrdb client only requires a non-empty
// Status field to consider the connection live.
func (s *Server) qrdbAdminHealth(_ context.Context, _ []byte) ([]byte, error) {
	resp, err := proto.Marshal(&pb.HealthResponse{
		Status: qrdbHealthStatusServing,
		Leader: true,
	})
	if err != nil {
		return nil, &QRDBMarshalError{Err: err}
	}
	return resp, nil
}

// =============================================================================
// Credential handlers (qrdb wire format)
// =============================================================================

func (s *Server) qrdbCredentialSubmit(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbCredentialService()
	if svc == nil {
		return nil, &QRDBCredentialNotConfiguredError{}
	}
	var req pb.CredentialSubmitRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	if err := svc.CredentialSubmit(ctx, req.GetName(), []byte(req.GetValue())); err != nil {
		return nil, err
	}
	return nil, nil
}

func (s *Server) qrdbCredentialStrategy(_ context.Context, _ []byte) ([]byte, error) {
	svc := s.qrdbCredentialService()
	if svc == nil {
		return nil, &QRDBCredentialNotConfiguredError{}
	}
	resp, err := proto.Marshal(&pb.CredentialStrategyResponse{
		Strategy: svc.CredentialStrategy(),
	})
	if err != nil {
		return nil, &QRDBMarshalError{Err: err}
	}
	return resp, nil
}

func (s *Server) qrdbCredentialDelete(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbCredentialService()
	if svc == nil {
		return nil, &QRDBCredentialNotConfiguredError{}
	}
	var req pb.CredentialDeleteRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	if err := svc.CredentialDelete(ctx, req.GetName()); err != nil {
		return nil, err
	}
	return nil, nil
}

func (s *Server) qrdbCredentialList(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbCredentialService()
	if svc == nil {
		return nil, &QRDBCredentialNotConfiguredError{}
	}
	var req pb.CredentialListRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	names, err := svc.CredentialList(ctx)
	if err != nil {
		return nil, err
	}
	resp, marshalErr := proto.Marshal(&pb.CredentialListResponse{Names: names})
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

func (s *Server) qrdbCredentialRotate(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbCredentialService()
	if svc == nil {
		return nil, &QRDBCredentialNotConfiguredError{}
	}
	var req pb.CredentialRotateRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	result, err := svc.CredentialRotate(ctx, req.GetName(), qrdbsdk.RotateOptions{
		NewValue:    []byte(req.GetNewValue()),
		GracePeriod: time.Duration(req.GetGracePeriodSeconds()) * time.Second,
	})
	if err != nil {
		return nil, err
	}
	out := &pb.CredentialRotateResponse{
		Name:            result.Name,
		PreviousVersion: result.PreviousVersion,
		NewVersion:      result.NewVersion,
	}
	if !result.GraceUntil.IsZero() {
		out.GraceUntilUnix = result.GraceUntil.Unix()
	}
	resp, marshalErr := proto.Marshal(out)
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

func (s *Server) qrdbCredentialRetrieveVersion(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbCredentialService()
	if svc == nil {
		return nil, &QRDBCredentialNotConfiguredError{}
	}
	var req pb.CredentialRetrieveVersionRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	value, err := svc.CredentialRetrieveVersion(ctx, req.GetName(), req.GetVersion())
	if err != nil {
		return nil, err
	}
	resp, marshalErr := proto.Marshal(&pb.CredentialRetrieveVersionResponse{Value: []byte(value)})
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

func (s *Server) qrdbCredentialListVersions(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbCredentialService()
	if svc == nil {
		return nil, &QRDBCredentialNotConfiguredError{}
	}
	var req pb.CredentialListVersionsRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	versions, err := svc.CredentialListVersions(ctx, req.GetName())
	if err != nil {
		return nil, err
	}
	out := &pb.CredentialListVersionsResponse{
		Name:     req.GetName(),
		Versions: make([]*pb.CredentialVersionInfo, 0, len(versions)),
	}
	for _, v := range versions {
		entry := &pb.CredentialVersionInfo{
			Version:   v.Version,
			IsCurrent: v.IsCurrent,
		}
		if !v.CreatedAt.IsZero() {
			entry.CreatedAtUnix = v.CreatedAt.Unix()
		}
		if !v.GraceUntil.IsZero() {
			entry.GraceUntilUnix = v.GraceUntil.Unix()
		}
		out.Versions = append(out.Versions, entry)
	}
	resp, marshalErr := proto.Marshal(out)
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

func (s *Server) qrdbCredentialGCExpired(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbCredentialService()
	if svc == nil {
		return nil, &QRDBCredentialNotConfiguredError{}
	}
	var req pb.CredentialGCRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	reaped, err := svc.CredentialGCExpired(ctx, req.GetName())
	if err != nil {
		return nil, err
	}
	resp, marshalErr := proto.Marshal(&pb.CredentialGCResponse{
		Reaped: int32(reaped), //nolint:gosec // reaped is a count, well within int32 range
	})
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

// =============================================================================
// Barrier handlers (qrdb wire format)
//
// Each handler unmarshals the canonical qrdbv1 request, delegates to the
// shared BarrierServicer facade (the same instance the gRPC/REST transports
// use), and marshals the canonical qrdbv1 response. Timestamps are encoded
// as RFC3339Nano to match go-qrdb's own QUIC server, which the qrdb client
// parses defensively as RFC3339.
// =============================================================================

func (s *Server) qrdbBarrierInitialize(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbBarrierService()
	if svc == nil {
		return nil, &QRDBBarrierNotConfiguredError{}
	}
	var req pb.BarrierInitializeRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	if err := svc.BarrierInitialize(ctx, req.GetSecret()); err != nil {
		return nil, err
	}
	return nil, nil
}

func (s *Server) qrdbBarrierUnseal(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbBarrierService()
	if svc == nil {
		return nil, &QRDBBarrierNotConfiguredError{}
	}
	var req pb.BarrierUnsealRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	if err := svc.BarrierUnseal(ctx, req.GetSecret()); err != nil {
		return nil, err
	}
	return nil, nil
}

func (s *Server) qrdbBarrierSeal(ctx context.Context, _ []byte) ([]byte, error) {
	svc := s.qrdbBarrierService()
	if svc == nil {
		return nil, &QRDBBarrierNotConfiguredError{}
	}
	if err := svc.BarrierSeal(ctx); err != nil {
		return nil, err
	}
	return nil, nil
}

func (s *Server) qrdbBarrierStatus(ctx context.Context, _ []byte) ([]byte, error) {
	svc := s.qrdbBarrierService()
	if svc == nil {
		return nil, &QRDBBarrierNotConfiguredError{}
	}
	info, err := svc.BarrierStatus(ctx)
	if err != nil {
		return nil, err
	}
	out := &pb.BarrierStatusResponse{
		Sealed:         info.Sealed,
		Strategy:       info.Strategy,
		HardwareBacked: info.HardwareBacked,
	}
	if !info.InitializedAt.IsZero() {
		out.InitializedAt = info.InitializedAt.Format(time.RFC3339Nano)
	}
	resp, marshalErr := proto.Marshal(out)
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

func (s *Server) qrdbBarrierShamirInitialize(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbBarrierService()
	if svc == nil {
		return nil, &QRDBBarrierNotConfiguredError{}
	}
	var req pb.BarrierShamirInitializeRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	info, err := svc.BarrierInitializeShamir(ctx, req.GetSecret(), int(req.GetThreshold()), int(req.GetTotalShares()))
	if err != nil {
		return nil, err
	}
	// Share-free contract: share bytes are never returned over the wire;
	// officers claim their shares individually via the share-claim flow.
	resp, marshalErr := proto.Marshal(&pb.BarrierShamirOperationResponse{
		Threshold:   int32(info.Threshold),   //nolint:gosec // threshold is a small count
		TotalShares: int32(info.TotalShares), //nolint:gosec // share count is small
		ShareCount:  int32(info.ShareCount),  //nolint:gosec // share count is small
	})
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

func (s *Server) qrdbBarrierShamirUnsealShare(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbBarrierService()
	if svc == nil {
		return nil, &QRDBBarrierNotConfiguredError{}
	}
	var req pb.BarrierShamirUnsealShareRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	progress, err := svc.BarrierUnsealWithShare(ctx, req.GetShare())
	if err != nil {
		return nil, err
	}
	resp, marshalErr := proto.Marshal(&pb.BarrierShamirUnsealShareResponse{
		Required:  int32(progress.Required),  //nolint:gosec // small count
		Submitted: int32(progress.Submitted), //nolint:gosec // small count
		Complete:  progress.Complete,
	})
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

func (s *Server) qrdbBarrierShamirUnsealShares(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbBarrierService()
	if svc == nil {
		return nil, &QRDBBarrierNotConfiguredError{}
	}
	var req pb.BarrierShamirUnsealSharesRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	if err := svc.BarrierUnsealWithShares(ctx, req.GetShares()); err != nil {
		return nil, err
	}
	return nil, nil
}

func (s *Server) qrdbBarrierRekey(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbBarrierService()
	if svc == nil {
		return nil, &QRDBBarrierNotConfiguredError{}
	}
	var req pb.BarrierRekeyRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	info, err := svc.BarrierRekey(ctx, int(req.GetThreshold()), int(req.GetTotal()))
	if err != nil {
		return nil, err
	}
	// Share-free contract: new shares are sealed to officer enclaves, never returned.
	resp, marshalErr := proto.Marshal(&pb.BarrierShamirOperationResponse{
		Threshold:   int32(info.Threshold),   //nolint:gosec // small count
		TotalShares: int32(info.TotalShares), //nolint:gosec // small count
		ShareCount:  int32(info.ShareCount),  //nolint:gosec // small count
	})
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

func (s *Server) qrdbBarrierRecoveryGenerate(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbBarrierService()
	if svc == nil {
		return nil, &QRDBBarrierNotConfiguredError{}
	}
	var req pb.BarrierRecoveryGenerateRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	info, err := svc.BarrierGenerateRecoveryKeys(ctx, int(req.GetThreshold()), int(req.GetTotal()))
	if err != nil {
		return nil, err
	}
	// Share-free contract: recovery keys are sealed to recovery-officer enclaves,
	// never returned; officers claim them via the recovery share-claim flow.
	resp, marshalErr := proto.Marshal(&pb.BarrierRecoveryKeysResponse{
		Threshold:  int32(info.Threshold),  //nolint:gosec // small count
		Total:      int32(info.Total),      //nolint:gosec // small count
		ShareCount: int32(info.ShareCount), //nolint:gosec // small count
	})
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

func (s *Server) qrdbBarrierRecoveryRecover(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbBarrierService()
	if svc == nil {
		return nil, &QRDBBarrierNotConfiguredError{}
	}
	var req pb.BarrierRecoveryRecoverRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	if err := svc.BarrierRecoverWithKeys(ctx, req.GetKeys()); err != nil {
		return nil, err
	}
	return nil, nil
}

func (s *Server) qrdbBarrierRecoveryDelete(ctx context.Context, _ []byte) ([]byte, error) {
	svc := s.qrdbBarrierService()
	if svc == nil {
		return nil, &QRDBBarrierNotConfiguredError{}
	}
	if err := svc.BarrierDeleteRecoveryKeys(ctx); err != nil {
		return nil, err
	}
	return nil, nil
}

func (s *Server) qrdbBarrierRecoveryStatus(ctx context.Context, _ []byte) ([]byte, error) {
	svc := s.qrdbBarrierService()
	if svc == nil {
		return nil, &QRDBBarrierNotConfiguredError{}
	}
	has, err := svc.BarrierHasRecoveryKeys(ctx)
	if err != nil {
		return nil, err
	}
	resp, marshalErr := proto.Marshal(&pb.BarrierRecoveryStatusResponse{HasRecoveryKeys: has})
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

func (s *Server) qrdbBarrierRootToken(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbBarrierService()
	if svc == nil {
		return nil, &QRDBBarrierNotConfiguredError{}
	}
	var req pb.BarrierRootTokenRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	info, err := svc.BarrierGenerateRootToken(ctx, req.GetShares())
	if err != nil {
		return nil, err
	}
	out := &pb.BarrierRootTokenResponse{Token: info.Token}
	if !info.CreatedAt.IsZero() {
		out.CreatedAt = info.CreatedAt.Format(time.RFC3339Nano)
	}
	resp, marshalErr := proto.Marshal(out)
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

// qrdbBarrierEnableEncryption handles the "barrier.EnableEncryption" qrdb-quic
// method. It migrates the system barrier from none to a keyed strategy by
// delegating to the BarrierServicer.EnableEncryption (which requires the
// embedded node migrator). The qrdb wire format uses
// BarrierEnableEncryptionRequest; tenant_id is empty for the system scope.
func (s *Server) qrdbBarrierEnableEncryption(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbBarrierService()
	if svc == nil {
		return nil, &QRDBBarrierNotConfiguredError{}
	}
	var req pb.BarrierEnableEncryptionRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}

	mech := req.GetMechanism()
	if mech == "" {
		mech = string(qrdbsdk.MechanismSwap)
	}
	avail := req.GetAvailability()
	if avail == "" {
		avail = string(qrdbsdk.AvailabilityOnline)
	}
	opts := qrdbsdk.MigrationOptions{
		TargetStrategy:  qrdbsdk.StrategyID(req.GetStrategy()),
		Mechanism:       qrdbsdk.MigrationMechanism(mech),
		Availability:    qrdbsdk.MigrationAvailability(avail),
		ShamirThreshold: int(req.GetShamirThreshold()),
		ShamirTotal:     int(req.GetShamirTotal()),
	}
	if p := req.GetPassphrase(); p != "" {
		opts.Credentials = qrdbsdk.Credentials{Secret: []byte(p)}
	}
	// tenant_id in the qrdb wire message selects system vs tenant scope.
	if err := svc.EnableEncryption(ctx, req.GetTenantId(), opts); err != nil {
		return nil, err
	}
	return nil, nil
}

// =============================================================================
// Ceremony handlers (qrdb wire format)
//
// Each handler unmarshals the canonical qrdbv1 request, delegates to the
// shared CeremonyServicer facade (the same instance the gRPC/REST transports
// use), and marshals the canonical qrdbv1 response.
// =============================================================================

func (s *Server) qrdbCeremonyInitialize(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbCeremonyService()
	if svc == nil {
		return nil, &QRDBCeremonyNotConfiguredError{}
	}
	var req pb.CeremonyInitializeRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	officers := make([]service.CeremonyOfficer, 0, len(req.GetOfficers()))
	for _, o := range req.GetOfficers() {
		officers = append(officers, service.CeremonyOfficer{
			Username: o.GetUsername(),
			CSRPEM:   o.GetCsrPem(),
		})
	}
	result, err := svc.CeremonyInitialize(ctx, &service.CeremonyInitRequest{
		SOPin:                  req.GetSoPin(),
		UserPin:                req.GetUserPin(),
		CredentialSealStrategy: req.GetCredentialSealStrategy(),
		Threshold:              int(req.GetThreshold()),
		Officers:               officers,
		Hostname:               req.GetHostname(),
		SANs:                   req.GetSans(),
	})
	if err != nil {
		return nil, err
	}
	resp, marshalErr := proto.Marshal(&pb.CeremonyInitializeResponse{
		SpkiPin:    result.SPKIPin,
		CaCertPem:  string(result.CACertPEM),
		TlsCertPem: string(result.TLSCertPEM),
		State:      result.State,
		ShareCount: int32(result.ShareCount), //nolint:gosec // small count
	})
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

func (s *Server) qrdbCeremonyStatus(ctx context.Context, _ []byte) ([]byte, error) {
	svc := s.qrdbCeremonyService()
	if svc == nil {
		return nil, &QRDBCeremonyNotConfiguredError{}
	}
	info, err := svc.CeremonyStatus(ctx)
	if err != nil {
		return nil, err
	}
	resp, marshalErr := proto.Marshal(&pb.CeremonyStatusResponse{
		State:   info.State,
		SpkiPin: info.SPKIPin,
	})
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

func (s *Server) qrdbCeremonyClaimCertBegin(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbCeremonyService()
	if svc == nil {
		return nil, &QRDBCeremonyNotConfiguredError{}
	}
	var req pb.CeremonyClaimCertBeginRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	challenge, err := svc.CeremonyBeginClaimCert(ctx, req.GetUsername())
	if err != nil {
		return nil, err
	}
	out := &pb.CeremonyClaimCertBeginResponse{NonceHex: challenge.NonceHex}
	if !challenge.ExpiresAt.IsZero() {
		out.ExpiresAt = challenge.ExpiresAt.Format(time.RFC3339Nano)
	}
	resp, marshalErr := proto.Marshal(out)
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

func (s *Server) qrdbCeremonyClaimCertComplete(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbCeremonyService()
	if svc == nil {
		return nil, &QRDBCeremonyNotConfiguredError{}
	}
	var req pb.CeremonyClaimCertCompleteRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	result, err := svc.CeremonyCompleteClaimCert(ctx, req.GetUsername(), req.GetNonceHex(), req.GetSignature())
	if err != nil {
		return nil, err
	}
	resp, marshalErr := proto.Marshal(&pb.CeremonyClaimCertCompleteResponse{
		CertPem:   string(result.CertPEM),
		CaCertPem: string(result.CACertPEM),
	})
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

func (s *Server) qrdbCeremonyBeginClaimShare(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbCeremonyService()
	if svc == nil {
		return nil, &QRDBCeremonyNotConfiguredError{}
	}
	var req pb.CeremonyBeginClaimShareRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	challenge, err := svc.CeremonyBeginClaimShare(ctx, req.GetUsername())
	if err != nil {
		return nil, err
	}
	out := &pb.CeremonyBeginClaimShareResponse{NonceHex: challenge.NonceHex}
	if !challenge.ExpiresAt.IsZero() {
		out.ExpiresAt = challenge.ExpiresAt.Format(time.RFC3339)
	}
	resp, marshalErr := proto.Marshal(out)
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

func (s *Server) qrdbCeremonyCompleteClaimShare(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbCeremonyService()
	if svc == nil {
		return nil, &QRDBCeremonyNotConfiguredError{}
	}
	var req pb.CeremonyCompleteClaimShareRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	result, err := svc.CeremonyCompleteClaimShare(ctx, req.GetUsername(), req.GetNonceHex(), req.GetSignature())
	if err != nil {
		return nil, err
	}
	resp, marshalErr := proto.Marshal(&pb.CeremonyCompleteClaimShareResponse{
		ShareIndex: int32(result.ShareIndex), //nolint:gosec // small index
		Share:      result.ShareJSON,
	})
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

func (s *Server) qrdbCeremonySignCSR(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbCeremonyService()
	if svc == nil {
		return nil, &QRDBCeremonyNotConfiguredError{}
	}
	var req pb.CeremonySignCSRRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	result, err := svc.CeremonySignCSR(ctx, req.GetUsername(), req.GetSoPin(), req.GetCsrPem(), req.GetRole())
	if err != nil {
		return nil, err
	}
	resp, marshalErr := proto.Marshal(&pb.CeremonySignCSRResponse{
		CertPem: string(result.CertPEM),
	})
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

func (s *Server) qrdbCeremonyEnrollExternalOfficer(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbCeremonyService()
	if svc == nil {
		return nil, &QRDBCeremonyNotConfiguredError{}
	}
	var req pb.CeremonyEnrollExternalOfficerRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	result, err := svc.CeremonyEnrollExternalOfficer(ctx, &service.CeremonyEnrollExternalOfficerRequest{
		Username:   req.GetUsername(),
		CertPEM:    req.GetCertPem(),
		PubKeyPEM:  req.GetPubkeyPem(),
		ShareIndex: int(req.GetShareIndex()),
		AuthFactor: req.GetAuthFactor(),
		ShareBytes: req.GetShareBytes(),
	})
	if err != nil {
		return nil, err
	}
	resp, marshalErr := proto.Marshal(&pb.CeremonyEnrollExternalOfficerResponse{
		SealedShare: result.SealedShare,
	})
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

// SetQRDBCredentialService installs the qrdb credential management facade
// used by the qrdb-quic ALPN dispatch path. Lifecycle wiring calls this
// once the credential service is online. Safe to call concurrently with
// request handling. Passing nil disables the surface (handlers respond
// with the not-configured error).
func (s *Server) SetQRDBCredentialService(svc qrdbsdk.ServiceCredentialServicer) {
	s.svcMu.Lock()
	s.qrdbCredentialSvc = svc
	s.svcMu.Unlock()
}
