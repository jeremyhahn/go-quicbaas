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

//go:build !linux
// +build !linux

package grpc

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// PeerCredentials contains kernel-verified peer identity extracted via SO_PEERCRED.
// On non-Linux platforms, this is a stub that returns an error.
type PeerCredentials struct {
	PID int32  // Process ID of the connecting process
	UID uint32 // User ID of the connecting process
	GID uint32 // Group ID of the connecting process
}

// IsRoot returns true if the peer process is running as root (UID 0).
func (c *PeerCredentials) IsRoot() bool {
	return c.UID == 0
}

// String returns a string representation of the credentials.
func (c *PeerCredentials) String() string {
	return fmt.Sprintf("PeerCredentials{PID: %d, UID: %d, GID: %d}", c.PID, c.UID, c.GID)
}

// PeerCredentialsAuthInfo implements credentials.AuthInfo for Unix socket peer credentials.
type PeerCredentialsAuthInfo struct {
	Credentials *PeerCredentials
}

// AuthType returns the authentication type.
func (a *PeerCredentialsAuthInfo) AuthType() string {
	return "unix_peercred"
}

// PeerCredentialsUnavailableError is returned when SO_PEERCRED cannot be retrieved.
type PeerCredentialsUnavailableError struct {
	Cause error
}

// Error implements the error interface.
func (e *PeerCredentialsUnavailableError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("peer credentials unavailable: %v", e.Cause)
	}
	return "peer credentials unavailable"
}

// Unwrap returns the underlying error.
func (e *PeerCredentialsUnavailableError) Unwrap() error {
	return e.Cause
}

// PeerCredentialsVerificationError is returned when credential verification fails.
type PeerCredentialsVerificationError struct {
	Reason string
}

// Error implements the error interface.
func (e *PeerCredentialsVerificationError) Error() string {
	return fmt.Sprintf("peer credentials verification failed: %s", e.Reason)
}

// UnixTransportCredentials implements credentials.TransportCredentials for Unix sockets.
// On non-Linux platforms, this is a stub that does not extract credentials.
type UnixTransportCredentials struct{}

// NewUnixTransportCredentials creates a new UnixTransportCredentials instance.
func NewUnixTransportCredentials() credentials.TransportCredentials {
	return &UnixTransportCredentials{}
}

// ClientHandshake is not used for Unix sockets (client-side).
func (u *UnixTransportCredentials) ClientHandshake(ctx context.Context, authority string, conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return conn, nil, nil
}

// ServerHandshake returns an error on non-Linux platforms as SO_PEERCRED is not available.
func (u *UnixTransportCredentials) ServerHandshake(conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return conn, nil, &PeerCredentialsUnavailableError{
		Cause: ErrPeerCredUnsupported,
	}
}

// Info returns protocol info for the credentials.
func (u *UnixTransportCredentials) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{
		SecurityProtocol: "unix_peercred",
		SecurityVersion:  "1.0",
		ServerName:       "",
	}
}

// Clone creates a copy of the credentials.
func (u *UnixTransportCredentials) Clone() credentials.TransportCredentials {
	return &UnixTransportCredentials{}
}

// OverrideServerName is not applicable for Unix sockets.
func (u *UnixTransportCredentials) OverrideServerName(serverName string) error {
	return nil
}

// PeerCredentialVerifier defines the interface for verifying peer credentials.
type PeerCredentialVerifier interface {
	Verify(cred *PeerCredentials, method string) error
}

// AllowAllVerifier allows all credentials.
type AllowAllVerifier struct{}

// Verify always returns nil.
func (v *AllowAllVerifier) Verify(cred *PeerCredentials, method string) error {
	return nil
}

// RootOnlyVerifier only allows root.
type RootOnlyVerifier struct{}

// Verify returns an error if not root.
func (v *RootOnlyVerifier) Verify(cred *PeerCredentials, method string) error {
	if !cred.IsRoot() {
		return &PeerCredentialsVerificationError{Reason: "root privileges required"}
	}
	return nil
}

// UIDVerifier allows specific UIDs.
type UIDVerifier struct {
	AllowedUIDs map[uint32]bool
}

// NewUIDVerifier creates a new UIDVerifier.
func NewUIDVerifier(uids ...uint32) *UIDVerifier {
	allowed := make(map[uint32]bool)
	for _, uid := range uids {
		allowed[uid] = true
	}
	return &UIDVerifier{AllowedUIDs: allowed}
}

// Verify checks if UID is allowed.
func (v *UIDVerifier) Verify(cred *PeerCredentials, method string) error {
	if cred.IsRoot() {
		return nil
	}
	if !v.AllowedUIDs[cred.UID] {
		return &PeerCredentialsVerificationError{Reason: "user not authorized"}
	}
	return nil
}

// GIDVerifier allows specific GIDs.
type GIDVerifier struct {
	AllowedGIDs map[uint32]bool
}

// NewGIDVerifier creates a new GIDVerifier.
func NewGIDVerifier(gids ...uint32) *GIDVerifier {
	allowed := make(map[uint32]bool)
	for _, gid := range gids {
		allowed[gid] = true
	}
	return &GIDVerifier{AllowedGIDs: allowed}
}

// Verify checks if GID is allowed.
func (v *GIDVerifier) Verify(cred *PeerCredentials, method string) error {
	if cred.IsRoot() {
		return nil
	}
	if !v.AllowedGIDs[cred.GID] {
		return &PeerCredentialsVerificationError{Reason: "group not authorized"}
	}
	return nil
}

// ChainVerifier chains multiple verifiers.
type ChainVerifier struct {
	Verifiers []PeerCredentialVerifier
}

// NewChainVerifier creates a new ChainVerifier.
func NewChainVerifier(verifiers ...PeerCredentialVerifier) *ChainVerifier {
	return &ChainVerifier{Verifiers: verifiers}
}

// Verify runs all verifiers in chain.
func (v *ChainVerifier) Verify(cred *PeerCredentials, method string) error {
	for _, verifier := range v.Verifiers {
		if err := verifier.Verify(cred, method); err != nil {
			return err
		}
	}
	return nil
}

// peerCredentialsContextKey is the context key for storing peer credentials.
type peerCredentialsContextKey struct{}

// PeerCredentialsFromContext extracts peer credentials from the context.
// On non-Linux platforms, this always returns nil.
func PeerCredentialsFromContext(ctx context.Context) *PeerCredentials {
	if creds, ok := ctx.Value(peerCredentialsContextKey{}).(*PeerCredentials); ok {
		return creds
	}
	return nil
}

// ContextWithPeerCredentials adds peer credentials to the context.
func ContextWithPeerCredentials(ctx context.Context, creds *PeerCredentials) context.Context {
	return context.WithValue(ctx, peerCredentialsContextKey{}, creds)
}

// UnaryPeerCredentialsInterceptor is a no-op on non-Linux platforms.
func UnaryPeerCredentialsInterceptor(verifier PeerCredentialVerifier, log *logger.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// No-op on non-Linux platforms
		return handler(ctx, req)
	}
}

// StreamPeerCredentialsInterceptor is a no-op on non-Linux platforms.
func StreamPeerCredentialsInterceptor(verifier PeerCredentialVerifier, log *logger.Logger) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// No-op on non-Linux platforms
		return handler(srv, ss)
	}
}

// IsPeerCredentialsAvailable returns false on non-Linux platforms.
func IsPeerCredentialsAvailable(ctx context.Context) bool {
	return false
}
