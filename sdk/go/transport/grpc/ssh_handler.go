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
	"strings"
	"time"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	sshclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// isSSHService reports whether svc names the SSH service.
func isSSHService(svc string) bool {
	return strings.HasPrefix(svc, sshclient.ServiceName)
}

// sshHandlerFunc converts an SDK request to proto, invokes gRPC, and converts
// the proto response back to the SDK type.
type sshHandlerFunc func(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error)

// sshMethodMap gives O(1) dispatch from SDK method name to handler.
var sshMethodMap = map[string]sshHandlerFunc{
	sshclient.MethodGenerateKey:         handleSSHGenerateKey,
	sshclient.MethodImportKey:           handleSSHImportKey,
	sshclient.MethodGetKey:              handleSSHGetKey,
	sshclient.MethodListKeys:            handleSSHListKeys,
	sshclient.MethodDeleteKey:           handleSSHDeleteKey,
	sshclient.MethodExportPublicKey:     handleSSHExportPublicKey,
	sshclient.MethodSignDigest:          handleSSHSignDigest,
	sshclient.MethodAddKnownHost:        handleSSHAddKnownHost,
	sshclient.MethodListKnownHosts:      handleSSHListKnownHosts,
	sshclient.MethodRemoveKnownHost:     handleSSHRemoveKnownHost,
	sshclient.MethodAddAuthorizedKey:    handleSSHAddAuthorizedKey,
	sshclient.MethodListAuthorizedKeys:  handleSSHListAuthorizedKeys,
	sshclient.MethodRemoveAuthorizedKey: handleSSHRemoveAuthorizedKey,
	sshclient.MethodEmitAuthorizedKey:   handleSSHEmitAuthorizedKey,
}

// sshHandler adapts the SDK SSH surface to the generated SSHService client.
type sshHandler struct {
	conn *grpc.ClientConn
}

// newSSHHandler creates an SSH gRPC handler bound to conn.
func newSSHHandler(conn *grpc.ClientConn) *sshHandler {
	return &sshHandler{conn: conn}
}

// handleRequest dispatches an SSH method.
func (h *sshHandler) handleRequest(ctx context.Context, _, method string, req interface{}) (interface{}, error) {
	handler, ok := sshMethodMap[method]
	if !ok {
		return nil, errors.NewInvalidInput("grpc.ssh."+method, "unknown ssh method")
	}
	return handler(ctx, h.conn, req)
}

// =============================================================================
// Keys
// =============================================================================

func handleSSHGenerateKey(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.ssh.GenerateKey"
	sdkReq, ok := req.(*sshclient.GenerateRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSSHServiceClient(conn).GenerateSSHKey(ctx, &pb.SSHGenerateKeyRequest{
		Scope:     sdkReq.Scope,
		Owner:     sdkReq.Owner,
		Name:      sdkReq.Name,
		Algorithm: sdkReq.Algorithm,
		RsaBits:   int32(sdkReq.RSABits),
		Comment:   sdkReq.Comment,
		Backend:   sdkReq.Backend,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return sshKeyFromProto(op, resp.GetKey())
}

func handleSSHImportKey(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.ssh.ImportKey"
	sdkReq, ok := req.(*sshclient.ImportKeyRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSSHServiceClient(conn).ImportSSHKey(ctx, &pb.SSHImportKeyRequest{
		Scope:         sdkReq.Scope,
		Owner:         sdkReq.Owner,
		Name:          sdkReq.Name,
		Comment:       sdkReq.Comment,
		Pem:           sdkReq.PEM,
		PassphraseB64: sdkReq.PassphraseB64,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return sshKeyFromProto(op, resp.GetKey())
}

func handleSSHGetKey(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.ssh.GetKey"
	sdkReq, ok := req.(*sshclient.GetKeyRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSSHServiceClient(conn).GetSSHKey(ctx, &pb.SSHGetKeyRequest{
		Scope: sdkReq.Scope, Owner: sdkReq.Owner, Name: sdkReq.Name,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return sshKeyFromProto(op, resp.GetKey())
}

func handleSSHListKeys(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.ssh.ListKeys"
	sdkReq, ok := req.(*sshclient.ListKeysRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSSHServiceClient(conn).ListSSHKeys(ctx, &pb.SSHListKeysRequest{
		Scope: sdkReq.Scope, Owner: sdkReq.Owner,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	out := &sshclient.ListKeysResponse{Items: make([]*sshclient.Key, 0, len(resp.GetKeys()))}
	for _, k := range resp.GetKeys() {
		key, cerr := sshKeyFromProto(op, k)
		if cerr != nil {
			return nil, cerr
		}
		out.Items = append(out.Items, key)
	}
	return out, nil
}

func handleSSHDeleteKey(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.ssh.DeleteKey"
	sdkReq, ok := req.(*sshclient.DeleteKeyRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSSHServiceClient(conn).DeleteSSHKey(ctx, &pb.SSHDeleteKeyRequest{
		Scope: sdkReq.Scope, Owner: sdkReq.Owner, Name: sdkReq.Name,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &sshclient.AckResponse{Success: resp.GetSuccess()}, nil
}

func handleSSHExportPublicKey(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.ssh.ExportPublicKey"
	sdkReq, ok := req.(*sshclient.ExportRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSSHServiceClient(conn).ExportSSHPublicKey(ctx, &pb.SSHExportPublicKeyRequest{
		Scope: sdkReq.Scope, Owner: sdkReq.Owner, Name: sdkReq.Name,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &sshclient.PublicKey{
		Algorithm:   resp.GetAlgorithm(),
		Line:        resp.GetLine(),
		Fingerprint: resp.GetFingerprint(),
	}, nil
}

func handleSSHSignDigest(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.ssh.SignDigest"
	sdkReq, ok := req.(*sshclient.SignRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSSHServiceClient(conn).SignSSHDigest(ctx, &pb.SSHSignDigestRequest{
		Scope: sdkReq.Scope, Owner: sdkReq.Owner, Name: sdkReq.Name, Digest: sdkReq.Digest,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &sshclient.SignResponse{
		Format: resp.GetFormat(),
		Blob:   resp.GetBlob(),
		Rest:   resp.GetRest(),
	}, nil
}

// =============================================================================
// Known hosts
// =============================================================================

func handleSSHAddKnownHost(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.ssh.AddKnownHost"
	sdkReq, ok := req.(*sshclient.AddKnownHostRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSSHServiceClient(conn).AddSSHKnownHost(ctx, &pb.SSHAddKnownHostRequest{
		Scope:       sdkReq.Scope,
		Owner:       sdkReq.Owner,
		Host:        sdkReq.Host,
		Fingerprint: sdkReq.Fingerprint,
		Algorithm:   sdkReq.Algorithm,
		Source:      sdkReq.Source,
		PublicKey:   sdkReq.PublicKey,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &sshclient.AckResponse{Success: resp.GetSuccess()}, nil
}

func handleSSHListKnownHosts(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.ssh.ListKnownHosts"
	sdkReq, ok := req.(*sshclient.ListKnownHostsRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSSHServiceClient(conn).ListSSHKnownHosts(ctx, &pb.SSHListKnownHostsRequest{
		Scope: sdkReq.Scope, Owner: sdkReq.Owner,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	out := &sshclient.ListKnownHostsResponse{Items: make([]*sshclient.Pin, 0, len(resp.GetPins()))}
	for _, p := range resp.GetPins() {
		out.Items = append(out.Items, &sshclient.Pin{
			Scope:       p.GetScope(),
			Owner:       p.GetOwner(),
			Host:        p.GetHost(),
			Fingerprint: p.GetFingerprint(),
			Algorithm:   p.GetAlgorithm(),
			AddedAt:     time.Unix(p.GetAddedAtUnix(), 0).UTC(),
			Source:      p.GetSource(),
			PublicKey:   p.GetPublicKey(),
		})
	}
	return out, nil
}

func handleSSHRemoveKnownHost(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.ssh.RemoveKnownHost"
	sdkReq, ok := req.(*sshclient.RemoveKnownHostRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSSHServiceClient(conn).RemoveSSHKnownHost(ctx, &pb.SSHRemoveKnownHostRequest{
		Scope: sdkReq.Scope, Owner: sdkReq.Owner, Host: sdkReq.Host,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &sshclient.AckResponse{Success: resp.GetSuccess()}, nil
}

// =============================================================================
// Authorized keys
// =============================================================================

func handleSSHAddAuthorizedKey(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.ssh.AddAuthorizedKey"
	sdkReq, ok := req.(*sshclient.AddAuthorizedKeyRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSSHServiceClient(conn).AddSSHAuthorizedKey(ctx, &pb.SSHAddAuthorizedKeyRequest{
		Scope:   sdkReq.Scope,
		Owner:   sdkReq.Owner,
		Name:    sdkReq.Name,
		Line:    sdkReq.Line,
		Options: sdkReq.Options,
		Comment: sdkReq.Comment,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &sshclient.AckResponse{Success: resp.GetSuccess()}, nil
}

func handleSSHListAuthorizedKeys(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.ssh.ListAuthorizedKeys"
	sdkReq, ok := req.(*sshclient.ListAuthorizedKeysRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSSHServiceClient(conn).ListSSHAuthorizedKeys(ctx, &pb.SSHListAuthorizedKeysRequest{
		Scope: sdkReq.Scope, Owner: sdkReq.Owner,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	out := &sshclient.ListAuthorizedKeysResponse{
		Items: make([]*sshclient.AuthorizedEntry, 0, len(resp.GetEntries())),
	}
	for _, e := range resp.GetEntries() {
		out.Items = append(out.Items, &sshclient.AuthorizedEntry{
			Scope:   e.GetScope(),
			Owner:   e.GetOwner(),
			Name:    e.GetName(),
			Line:    e.GetLine(),
			Options: e.GetOptions(),
			Comment: e.GetComment(),
		})
	}
	return out, nil
}

func handleSSHRemoveAuthorizedKey(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.ssh.RemoveAuthorizedKey"
	sdkReq, ok := req.(*sshclient.RemoveAuthorizedKeyRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSSHServiceClient(conn).RemoveSSHAuthorizedKey(ctx, &pb.SSHRemoveAuthorizedKeyRequest{
		Scope: sdkReq.Scope, Owner: sdkReq.Owner, Name: sdkReq.Name,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &sshclient.AckResponse{Success: resp.GetSuccess()}, nil
}

func handleSSHEmitAuthorizedKey(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.ssh.EmitAuthorizedKey"
	sdkReq, ok := req.(*sshclient.EmitRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSSHServiceClient(conn).EmitSSHAuthorizedKey(ctx, &pb.SSHEmitAuthorizedKeyRequest{
		PublicKey: sdkReq.PublicKey,
		Options:   sdkReq.Options,
		Comment:   sdkReq.Comment,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &sshclient.EmitResponse{Line: resp.GetLine()}, nil
}

// =============================================================================
// Conversions
// =============================================================================

// sshKeyFromProto converts a proto SSHKeyInfo into the SDK Key DTO.
func sshKeyFromProto(op string, k *pb.SSHKeyInfo) (*sshclient.Key, error) {
	if k == nil {
		return nil, errors.NewInternal(op, "server returned no key")
	}
	return &sshclient.Key{
		Scope:        k.GetScope(),
		Owner:        k.GetOwner(),
		Name:         k.GetName(),
		Algorithm:    k.GetAlgorithm(),
		PublicKey:    k.GetPublicKey(),
		Fingerprint:  k.GetFingerprint(),
		CreatedAt:    time.Unix(k.GetCreatedAtUnix(), 0).UTC(),
		Comment:      k.GetComment(),
		Backend:      k.GetBackend(),
		BackendKeyID: k.GetBackendKeyId(),
	}, nil
}
