// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package grpc

import (
	"context"
	stderrors "errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// SSHServer exposes the SSH surface over gRPC (and, via the Unix domain
// socket listener, over Unix as well — the Unix transport is gRPC).
//
// Every method delegates to service.SSHService, the single server-side entry
// point that composes the go-xkms SSH SDK. No SSH logic lives here.
//
// SECURITY INVARIANT: no response message carries private key material.
// Import accepts a PEM inbound; Sign accepts a digest inbound and returns a
// signature. Neither ever echoes the key back.
type SSHServer struct {
	pb.UnimplementedSSHServiceServer
	sshSvc service.SSHService
	logger *logger.Logger
}

// NewSSHServer creates the gRPC SSH server bound to sshSvc.
func NewSSHServer(sshSvc service.SSHService, log *logger.Logger) *SSHServer {
	return &SSHServer{
		sshSvc: sshSvc,
		logger: log.WithField("component", "ssh-grpc-server"),
	}
}

// =============================================================================
// Keys
// =============================================================================

// GenerateSSHKey handles the GenerateSSHKey RPC.
func (s *SSHServer) GenerateSSHKey(ctx context.Context, req *pb.SSHGenerateKeyRequest) (*pb.SSHKeyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	key, err := s.sshSvc.GenerateKey(ctx, xkmsssh.GenerateRequest{
		Scope:     req.Scope,
		Owner:     req.Owner,
		Name:      req.Name,
		Algorithm: req.Algorithm,
		RSABits:   int(req.RsaBits),
		Comment:   req.Comment,
		Backend:   req.Backend,
	})
	if err != nil {
		return nil, convertSSHError(err)
	}
	return &pb.SSHKeyResponse{Key: sshKeyToProto(key)}, nil
}

// ImportSSHKey handles the ImportSSHKey RPC.
func (s *SSHServer) ImportSSHKey(ctx context.Context, req *pb.SSHImportKeyRequest) (*pb.SSHKeyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if len(req.Pem) == 0 {
		return nil, status.Error(codes.InvalidArgument, "pem is required")
	}
	key, err := s.sshSvc.ImportKey(ctx, service.SSHImportKeyRequest{
		Scope:         req.Scope,
		Owner:         req.Owner,
		Name:          req.Name,
		Comment:       req.Comment,
		PEM:           req.Pem,
		PassphraseB64: req.PassphraseB64,
	})
	if err != nil {
		return nil, convertSSHError(err)
	}
	return &pb.SSHKeyResponse{Key: sshKeyToProto(key)}, nil
}

// GetSSHKey handles the GetSSHKey RPC.
func (s *SSHServer) GetSSHKey(ctx context.Context, req *pb.SSHGetKeyRequest) (*pb.SSHKeyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	key, err := s.sshSvc.GetKey(ctx, xkmsssh.GetKeyRequest{
		Scope: req.Scope, Owner: req.Owner, Name: req.Name,
	})
	if err != nil {
		return nil, convertSSHError(err)
	}
	return &pb.SSHKeyResponse{Key: sshKeyToProto(key)}, nil
}

// ListSSHKeys handles the ListSSHKeys RPC.
func (s *SSHServer) ListSSHKeys(ctx context.Context, req *pb.SSHListKeysRequest) (*pb.SSHListKeysResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	keys, err := s.sshSvc.ListKeys(ctx, xkmsssh.ListKeysRequest{Scope: req.Scope, Owner: req.Owner})
	if err != nil {
		return nil, convertSSHError(err)
	}
	out := &pb.SSHListKeysResponse{Keys: make([]*pb.SSHKeyInfo, 0, len(keys))}
	for _, k := range keys {
		out.Keys = append(out.Keys, sshKeyToProto(k))
	}
	return out, nil
}

// DeleteSSHKey handles the DeleteSSHKey RPC.
func (s *SSHServer) DeleteSSHKey(ctx context.Context, req *pb.SSHDeleteKeyRequest) (*pb.SSHAckResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if err := s.sshSvc.DeleteKey(ctx, xkmsssh.DeleteKeyRequest{
		Scope: req.Scope, Owner: req.Owner, Name: req.Name,
	}); err != nil {
		return nil, convertSSHError(err)
	}
	return &pb.SSHAckResponse{Success: true}, nil
}

// ExportSSHPublicKey handles the ExportSSHPublicKey RPC.
func (s *SSHServer) ExportSSHPublicKey(ctx context.Context, req *pb.SSHExportPublicKeyRequest) (*pb.SSHPublicKeyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	pub, err := s.sshSvc.ExportPublicKey(ctx, xkmsssh.ExportRequest{
		Scope: req.Scope, Owner: req.Owner, Name: req.Name,
	})
	if err != nil {
		return nil, convertSSHError(err)
	}
	if pub == nil {
		return nil, status.Error(codes.Internal, "ssh: no public key returned")
	}
	return &pb.SSHPublicKeyResponse{
		Algorithm:   pub.Algorithm,
		Line:        pub.Line,
		Fingerprint: pub.Fingerprint,
	}, nil
}

// SignSSHDigest handles the SignSSHDigest RPC.
func (s *SSHServer) SignSSHDigest(ctx context.Context, req *pb.SSHSignDigestRequest) (*pb.SSHSignDigestResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if len(req.Digest) == 0 {
		return nil, status.Error(codes.InvalidArgument, "digest is required")
	}
	sig, err := s.sshSvc.SignDigest(ctx, service.SSHSignRequest{
		Scope: req.Scope, Owner: req.Owner, Name: req.Name, Digest: req.Digest,
	})
	if err != nil {
		return nil, convertSSHError(err)
	}
	return &pb.SSHSignDigestResponse{Format: sig.Format, Blob: sig.Blob, Rest: sig.Rest}, nil
}

// =============================================================================
// Known hosts
// =============================================================================

// AddSSHKnownHost handles the AddSSHKnownHost RPC.
func (s *SSHServer) AddSSHKnownHost(ctx context.Context, req *pb.SSHAddKnownHostRequest) (*pb.SSHAckResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.Host == "" {
		return nil, status.Error(codes.InvalidArgument, "host is required")
	}
	if req.Fingerprint == "" {
		return nil, status.Error(codes.InvalidArgument, "fingerprint is required")
	}
	if err := s.sshSvc.AddKnownHost(ctx, xkmsssh.AddKnownHostRequest{
		Scope:       req.Scope,
		Owner:       req.Owner,
		Host:        req.Host,
		Fingerprint: req.Fingerprint,
		Algorithm:   req.Algorithm,
		Source:      req.Source,
	}); err != nil {
		return nil, convertSSHError(err)
	}
	return &pb.SSHAckResponse{Success: true}, nil
}

// ListSSHKnownHosts handles the ListSSHKnownHosts RPC.
func (s *SSHServer) ListSSHKnownHosts(ctx context.Context, req *pb.SSHListKnownHostsRequest) (*pb.SSHListKnownHostsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	pins, err := s.sshSvc.ListKnownHosts(ctx, xkmsssh.ListKnownHostsRequest{
		Scope: req.Scope, Owner: req.Owner,
	})
	if err != nil {
		return nil, convertSSHError(err)
	}
	out := &pb.SSHListKnownHostsResponse{Pins: make([]*pb.SSHHostPin, 0, len(pins))}
	for _, p := range pins {
		out.Pins = append(out.Pins, sshPinToProto(p))
	}
	return out, nil
}

// RemoveSSHKnownHost handles the RemoveSSHKnownHost RPC.
func (s *SSHServer) RemoveSSHKnownHost(ctx context.Context, req *pb.SSHRemoveKnownHostRequest) (*pb.SSHAckResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.Host == "" {
		return nil, status.Error(codes.InvalidArgument, "host is required")
	}
	if err := s.sshSvc.RemoveKnownHost(ctx, xkmsssh.RemoveKnownHostRequest{
		Scope: req.Scope, Owner: req.Owner, Host: req.Host,
	}); err != nil {
		return nil, convertSSHError(err)
	}
	return &pb.SSHAckResponse{Success: true}, nil
}

// =============================================================================
// Authorized keys
// =============================================================================

// AddSSHAuthorizedKey handles the AddSSHAuthorizedKey RPC.
func (s *SSHServer) AddSSHAuthorizedKey(ctx context.Context, req *pb.SSHAddAuthorizedKeyRequest) (*pb.SSHAckResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if req.Line == "" {
		return nil, status.Error(codes.InvalidArgument, "line is required")
	}
	if err := s.sshSvc.AddAuthorizedKey(ctx, xkmsssh.AddAuthorizedKeyRequest{
		Scope:   req.Scope,
		Owner:   req.Owner,
		Name:    req.Name,
		Line:    req.Line,
		Options: req.Options,
		Comment: req.Comment,
	}); err != nil {
		return nil, convertSSHError(err)
	}
	return &pb.SSHAckResponse{Success: true}, nil
}

// ListSSHAuthorizedKeys handles the ListSSHAuthorizedKeys RPC.
func (s *SSHServer) ListSSHAuthorizedKeys(ctx context.Context, req *pb.SSHListAuthorizedKeysRequest) (*pb.SSHListAuthorizedKeysResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	entries, err := s.sshSvc.ListAuthorizedKeys(ctx, xkmsssh.ListAuthorizedKeysRequest{
		Scope: req.Scope, Owner: req.Owner,
	})
	if err != nil {
		return nil, convertSSHError(err)
	}
	out := &pb.SSHListAuthorizedKeysResponse{Entries: make([]*pb.SSHAuthorizedEntry, 0, len(entries))}
	for _, e := range entries {
		out.Entries = append(out.Entries, sshAuthorizedToProto(e))
	}
	return out, nil
}

// RemoveSSHAuthorizedKey handles the RemoveSSHAuthorizedKey RPC.
func (s *SSHServer) RemoveSSHAuthorizedKey(ctx context.Context, req *pb.SSHRemoveAuthorizedKeyRequest) (*pb.SSHAckResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if err := s.sshSvc.RemoveAuthorizedKey(ctx, service.SSHAuthorizedKeyRef{
		Scope: req.Scope, Owner: req.Owner, Name: req.Name,
	}); err != nil {
		return nil, convertSSHError(err)
	}
	return &pb.SSHAckResponse{Success: true}, nil
}

// EmitSSHAuthorizedKey handles the EmitSSHAuthorizedKey RPC.
func (s *SSHServer) EmitSSHAuthorizedKey(ctx context.Context, req *pb.SSHEmitAuthorizedKeyRequest) (*pb.SSHEmitAuthorizedKeyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.PublicKey == "" {
		return nil, status.Error(codes.InvalidArgument, "public_key is required")
	}
	line, err := s.sshSvc.EmitAuthorizedKey(ctx, xkmsssh.EmitRequest{
		PublicKey: req.PublicKey,
		Options:   req.Options,
		Comment:   req.Comment,
	})
	if err != nil {
		return nil, convertSSHError(err)
	}
	return &pb.SSHEmitAuthorizedKeyResponse{Line: line}, nil
}

// =============================================================================
// Conversions
// =============================================================================

// sshKeyToProto converts an xkms Key DTO to its proto form.
func sshKeyToProto(k *xkmsssh.Key) *pb.SSHKeyInfo {
	if k == nil {
		return nil
	}
	return &pb.SSHKeyInfo{
		Scope:         k.Scope,
		Owner:         k.Owner,
		Name:          k.Name,
		Algorithm:     k.Algorithm,
		PublicKey:     k.PublicKey,
		Fingerprint:   k.Fingerprint,
		CreatedAtUnix: k.CreatedAt.Unix(),
		Comment:       k.Comment,
		Backend:       k.Backend,
		BackendKeyId:  k.BackendKeyID,
	}
}

// sshPinToProto converts an xkms Pin DTO to its proto form.
func sshPinToProto(p *xkmsssh.Pin) *pb.SSHHostPin {
	if p == nil {
		return nil
	}
	return &pb.SSHHostPin{
		Scope:       p.Scope,
		Owner:       p.Owner,
		Host:        p.Host,
		Fingerprint: p.Fingerprint,
		Algorithm:   p.Algorithm,
		AddedAtUnix: p.AddedAt.Unix(),
		Source:      p.Source,
	}
}

// sshAuthorizedToProto converts an xkms AuthorizedEntry to its proto form.
func sshAuthorizedToProto(e *xkmsssh.AuthorizedEntry) *pb.SSHAuthorizedEntry {
	if e == nil {
		return nil
	}
	return &pb.SSHAuthorizedEntry{
		Scope:   e.Scope,
		Owner:   e.Owner,
		Name:    e.Name,
		Line:    e.Line,
		Options: e.Options,
		Comment: e.Comment,
	}
}

// convertSSHError maps a service-layer SSH error onto a gRPC status. The
// xkms SDK error taxonomy is the source of truth for not-found / conflict /
// invalid-input; the service adds "not configured" and "bad digest".
func convertSSHError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok && status.Code(err) != codes.Unknown {
		return err
	}

	var notConfigured *service.SSHNotConfiguredError
	if stderrors.As(err, &notConfigured) {
		return status.Error(codes.Unavailable, err.Error())
	}
	var missingDigest *service.SSHMissingDigestError
	if stderrors.As(err, &missingDigest) {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	var badPassphrase *service.SSHPassphraseDecodeError
	if stderrors.As(err, &badPassphrase) {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	var notFound *xkmsssh.NotFoundError
	if stderrors.As(err, &notFound) {
		return status.Error(codes.NotFound, err.Error())
	}
	var duplicate *xkmsssh.DuplicateError
	if stderrors.As(err, &duplicate) {
		return status.Error(codes.AlreadyExists, err.Error())
	}
	var invalid *xkmsssh.InvalidRequestError
	if stderrors.As(err, &invalid) {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	var nilInput *xkmsssh.NilInputError
	if stderrors.As(err, &nilInput) {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	var mismatch *xkmsssh.MismatchError
	if stderrors.As(err, &mismatch) {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	return status.Error(codes.Internal, err.Error())
}
