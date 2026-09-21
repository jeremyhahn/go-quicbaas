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

//go:build linux
// +build linux

package grpc

import (
	"context"
	"fmt"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// PeerCredentials contains kernel-verified peer identity extracted via SO_PEERCRED.
// These credentials are verified by the kernel and cannot be spoofed.
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
// It extracts SO_PEERCRED on connection and makes it available as AuthInfo.
type UnixTransportCredentials struct{}

// NewUnixTransportCredentials creates a new UnixTransportCredentials instance.
func NewUnixTransportCredentials() credentials.TransportCredentials {
	return &UnixTransportCredentials{}
}

// ClientHandshake is not used for Unix sockets (client-side).
func (u *UnixTransportCredentials) ClientHandshake(ctx context.Context, authority string, conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	// For client side, we don't extract credentials
	return conn, nil, nil
}

// ServerHandshake extracts SO_PEERCRED from the Unix socket connection.
func (u *UnixTransportCredentials) ServerHandshake(conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		// Not a Unix socket, return without credentials
		return conn, nil, nil
	}

	creds, err := extractPeerCredentials(unixConn)
	if err != nil {
		// Return the connection but with an error for the credentials
		return conn, nil, err
	}

	return conn, &PeerCredentialsAuthInfo{Credentials: creds}, nil
}

// Info returns protocol info for the credentials.
func (u *UnixTransportCredentials) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{
		SecurityProtocol: "unix_peercred",
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

// extractPeerCredentials retrieves SO_PEERCRED from a Unix domain socket connection.
// The credentials are kernel-verified and cannot be forged by the peer.
func extractPeerCredentials(conn *net.UnixConn) (*PeerCredentials, error) {
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return nil, &PeerCredentialsUnavailableError{Cause: err}
	}

	var cred *unix.Ucred
	var credErr error

	controlErr := rawConn.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	})

	if controlErr != nil {
		return nil, &PeerCredentialsUnavailableError{Cause: controlErr}
	}

	if credErr != nil {
		return nil, &PeerCredentialsUnavailableError{Cause: credErr}
	}

	return &PeerCredentials{
		PID: cred.Pid,
		UID: cred.Uid,
		GID: cred.Gid,
	}, nil
}

// PeerCredentialVerifier defines the interface for verifying peer credentials.
type PeerCredentialVerifier interface {
	// Verify checks if the credentials are authorized for the given method.
	// Returns nil if authorized, an error otherwise.
	Verify(cred *PeerCredentials, method string) error
}

// AllowAllVerifier allows all credentials (for testing or open environments).
type AllowAllVerifier struct{}

// Verify always returns nil (allows all operations).
func (v *AllowAllVerifier) Verify(cred *PeerCredentials, method string) error {
	return nil
}

// RootOnlyVerifier only allows root (UID 0) to perform operations.
type RootOnlyVerifier struct{}

// Verify returns an error if the credentials are not root.
func (v *RootOnlyVerifier) Verify(cred *PeerCredentials, method string) error {
	if !cred.IsRoot() {
		return &PeerCredentialsVerificationError{Reason: "root privileges required"}
	}
	return nil
}

// UIDVerifier allows specific UIDs to perform operations.
type UIDVerifier struct {
	AllowedUIDs map[uint32]bool
}

// NewUIDVerifier creates a new UIDVerifier with the specified allowed UIDs.
func NewUIDVerifier(uids ...uint32) *UIDVerifier {
	allowed := make(map[uint32]bool)
	for _, uid := range uids {
		allowed[uid] = true
	}
	return &UIDVerifier{AllowedUIDs: allowed}
}

// Verify returns an error if the credentials UID is not in the allowed list.
func (v *UIDVerifier) Verify(cred *PeerCredentials, method string) error {
	if cred.IsRoot() {
		return nil
	}
	if !v.AllowedUIDs[cred.UID] {
		return &PeerCredentialsVerificationError{Reason: "user not authorized"}
	}
	return nil
}

// GIDVerifier allows specific GIDs to perform operations.
type GIDVerifier struct {
	AllowedGIDs map[uint32]bool
}

// NewGIDVerifier creates a new GIDVerifier with the specified allowed GIDs.
func NewGIDVerifier(gids ...uint32) *GIDVerifier {
	allowed := make(map[uint32]bool)
	for _, gid := range gids {
		allowed[gid] = true
	}
	return &GIDVerifier{AllowedGIDs: allowed}
}

// Verify returns an error if the credentials GID is not in the allowed list.
func (v *GIDVerifier) Verify(cred *PeerCredentials, method string) error {
	if cred.IsRoot() {
		return nil
	}
	if !v.AllowedGIDs[cred.GID] {
		return &PeerCredentialsVerificationError{Reason: "group not authorized"}
	}
	return nil
}

// ChainVerifier chains multiple verifiers together (all must pass).
type ChainVerifier struct {
	Verifiers []PeerCredentialVerifier
}

// NewChainVerifier creates a new ChainVerifier with the specified verifiers.
func NewChainVerifier(verifiers ...PeerCredentialVerifier) *ChainVerifier {
	return &ChainVerifier{Verifiers: verifiers}
}

// Verify returns an error if any verifier in the chain fails.
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
// Returns nil if no credentials are available.
func PeerCredentialsFromContext(ctx context.Context) *PeerCredentials {
	if creds, ok := ctx.Value(peerCredentialsContextKey{}).(*PeerCredentials); ok {
		return creds
	}

	// Also try to extract from gRPC peer info
	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil
	}

	authInfo, ok := p.AuthInfo.(*PeerCredentialsAuthInfo)
	if !ok {
		return nil
	}

	return authInfo.Credentials
}

// ContextWithPeerCredentials adds peer credentials to the context.
func ContextWithPeerCredentials(ctx context.Context, creds *PeerCredentials) context.Context {
	return context.WithValue(ctx, peerCredentialsContextKey{}, creds)
}

// UnaryPeerCredentialsInterceptor extracts peer credentials from Unix socket connections
// and optionally verifies them using the provided verifier.
func UnaryPeerCredentialsInterceptor(verifier PeerCredentialVerifier, log *logger.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		creds := extractCredsFromContext(ctx)
		if creds != nil {
			// Add credentials to context for downstream handlers
			ctx = ContextWithPeerCredentials(ctx, creds)

			// Verify if a verifier is provided
			if verifier != nil {
				if err := verifier.Verify(creds, info.FullMethod); err != nil {
					log.WithFields(map[string]interface{}{
						"method": info.FullMethod,
						"uid":    creds.UID,
						"gid":    creds.GID,
						"pid":    creds.PID,
						"error":  err.Error(),
					}).Warn("Peer credentials verification failed")

					return nil, status.Errorf(codes.PermissionDenied, "peer credentials verification failed: %v", err)
				}

				log.WithFields(map[string]interface{}{
					"method": info.FullMethod,
					"uid":    creds.UID,
					"gid":    creds.GID,
					"pid":    creds.PID,
				}).Debug("Peer credentials verified")
			}
		}

		return handler(ctx, req)
	}
}

// StreamPeerCredentialsInterceptor extracts peer credentials from Unix socket connections
// and optionally verifies them using the provided verifier.
func StreamPeerCredentialsInterceptor(verifier PeerCredentialVerifier, log *logger.Logger) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()
		creds := extractCredsFromContext(ctx)
		if creds != nil {
			// Add credentials to context for downstream handlers
			ctx = ContextWithPeerCredentials(ctx, creds)

			// Verify if a verifier is provided
			if verifier != nil {
				if err := verifier.Verify(creds, info.FullMethod); err != nil {
					log.WithFields(map[string]interface{}{
						"method": info.FullMethod,
						"uid":    creds.UID,
						"gid":    creds.GID,
						"pid":    creds.PID,
						"error":  err.Error(),
					}).Warn("Peer credentials verification failed")

					return status.Errorf(codes.PermissionDenied, "peer credentials verification failed: %v", err)
				}

				log.WithFields(map[string]interface{}{
					"method": info.FullMethod,
					"uid":    creds.UID,
					"gid":    creds.GID,
					"pid":    creds.PID,
				}).Debug("Peer credentials verified")
			}

			// Wrap the stream with the modified context
			ss = wrapServerStream(ss, ctx)
		}

		return handler(srv, ss)
	}
}

// extractCredsFromContext extracts PeerCredentials from the gRPC peer context.
func extractCredsFromContext(ctx context.Context) *PeerCredentials {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil
	}

	authInfo, ok := p.AuthInfo.(*PeerCredentialsAuthInfo)
	if !ok {
		return nil
	}

	return authInfo.Credentials
}

// IsPeerCredentialsAvailable returns true if peer credentials can be extracted
// from the current context (i.e., the connection is a Unix socket).
func IsPeerCredentialsAvailable(ctx context.Context) bool {
	return PeerCredentialsFromContext(ctx) != nil
}
