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

package embedded

import (
	"context"
	"reflect"

	sshclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// SSH Handlers
// =============================================================================
//
// The SSH surface composes the go-xkms SSH SDK. These handlers delegate to
// the service-layer SSHService — the same service every other transport
// uses — and return the sdk/go/client/ssh wire types the SDK client
// type-asserts on. No SSH logic lives here.

// sshService builds the SSH service from the composed QuicBaaS service. A
// deployment without an SSH store yields a service whose verbs all report
// *service.SSHNotConfiguredError, which is the intended "SSH is off" signal.
func sshService(svc service.QuicBaaS) service.SSHService {
	return service.SSHServiceFor(svc)
}

// sshTypeError builds the standard invalid-request-type error for method.
func sshTypeError(method, want string, got interface{}) error {
	gotName := "<nil>"
	if got != nil {
		gotName = reflect.TypeOf(got).String()
	}
	return NewInvalidRequestTypeError(sshclient.ServiceName, method, want, gotName)
}

// =============================================================================
// Keys
// =============================================================================

func handleSSHGenerateKey(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.GenerateRequest)
	if !ok {
		return nil, sshTypeError(sshclient.MethodGenerateKey, "*ssh.GenerateRequest", req)
	}
	return sshService(svc).GenerateKey(ctx, *sdkReq)
}

func handleSSHImportKey(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.ImportKeyRequest)
	if !ok {
		return nil, sshTypeError(sshclient.MethodImportKey, "*ssh.ImportKeyRequest", req)
	}
	return sshService(svc).ImportKey(ctx, service.SSHImportKeyRequest{
		Scope:         sdkReq.Scope,
		Owner:         sdkReq.Owner,
		Name:          sdkReq.Name,
		Comment:       sdkReq.Comment,
		PEM:           sdkReq.PEM,
		PassphraseB64: sdkReq.PassphraseB64,
	})
}

func handleSSHGetKey(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.GetKeyRequest)
	if !ok {
		return nil, sshTypeError(sshclient.MethodGetKey, "*ssh.GetKeyRequest", req)
	}
	return sshService(svc).GetKey(ctx, *sdkReq)
}

func handleSSHListKeys(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.ListKeysRequest)
	if !ok {
		return nil, sshTypeError(sshclient.MethodListKeys, "*ssh.ListKeysRequest", req)
	}
	items, err := sshService(svc).ListKeys(ctx, *sdkReq)
	if err != nil {
		return nil, err
	}
	return &sshclient.ListKeysResponse{Items: items}, nil
}

func handleSSHDeleteKey(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.DeleteKeyRequest)
	if !ok {
		return nil, sshTypeError(sshclient.MethodDeleteKey, "*ssh.DeleteKeyRequest", req)
	}
	if err := sshService(svc).DeleteKey(ctx, *sdkReq); err != nil {
		return nil, err
	}
	return &sshclient.AckResponse{Success: true}, nil
}

func handleSSHExportPublicKey(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.ExportRequest)
	if !ok {
		return nil, sshTypeError(sshclient.MethodExportPublicKey, "*ssh.ExportRequest", req)
	}
	return sshService(svc).ExportPublicKey(ctx, *sdkReq)
}

func handleSSHSignDigest(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.SignRequest)
	if !ok {
		return nil, sshTypeError(sshclient.MethodSignDigest, "*ssh.SignRequest", req)
	}
	sig, err := sshService(svc).SignDigest(ctx, service.SSHSignRequest{
		Scope:  sdkReq.Scope,
		Owner:  sdkReq.Owner,
		Name:   sdkReq.Name,
		Digest: sdkReq.Digest,
	})
	if err != nil {
		return nil, err
	}
	return &sshclient.SignResponse{Format: sig.Format, Blob: sig.Blob, Rest: sig.Rest}, nil
}

// =============================================================================
// Known hosts
// =============================================================================

func handleSSHAddKnownHost(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.AddKnownHostRequest)
	if !ok {
		return nil, sshTypeError(sshclient.MethodAddKnownHost, "*ssh.AddKnownHostRequest", req)
	}
	if err := sshService(svc).AddKnownHost(ctx, *sdkReq); err != nil {
		return nil, err
	}
	return &sshclient.AckResponse{Success: true}, nil
}

func handleSSHListKnownHosts(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.ListKnownHostsRequest)
	if !ok {
		return nil, sshTypeError(sshclient.MethodListKnownHosts, "*ssh.ListKnownHostsRequest", req)
	}
	items, err := sshService(svc).ListKnownHosts(ctx, *sdkReq)
	if err != nil {
		return nil, err
	}
	return &sshclient.ListKnownHostsResponse{Items: items}, nil
}

func handleSSHRemoveKnownHost(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.RemoveKnownHostRequest)
	if !ok {
		return nil, sshTypeError(sshclient.MethodRemoveKnownHost, "*ssh.RemoveKnownHostRequest", req)
	}
	if err := sshService(svc).RemoveKnownHost(ctx, *sdkReq); err != nil {
		return nil, err
	}
	return &sshclient.AckResponse{Success: true}, nil
}

// =============================================================================
// Authorized keys
// =============================================================================

func handleSSHAddAuthorizedKey(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.AddAuthorizedKeyRequest)
	if !ok {
		return nil, sshTypeError(sshclient.MethodAddAuthorizedKey, "*ssh.AddAuthorizedKeyRequest", req)
	}
	if err := sshService(svc).AddAuthorizedKey(ctx, *sdkReq); err != nil {
		return nil, err
	}
	return &sshclient.AckResponse{Success: true}, nil
}

func handleSSHListAuthorizedKeys(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.ListAuthorizedKeysRequest)
	if !ok {
		return nil, sshTypeError(sshclient.MethodListAuthorizedKeys, "*ssh.ListAuthorizedKeysRequest", req)
	}
	items, err := sshService(svc).ListAuthorizedKeys(ctx, *sdkReq)
	if err != nil {
		return nil, err
	}
	return &sshclient.ListAuthorizedKeysResponse{Items: items}, nil
}

func handleSSHRemoveAuthorizedKey(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.RemoveAuthorizedKeyRequest)
	if !ok {
		return nil, sshTypeError(sshclient.MethodRemoveAuthorizedKey, "*ssh.RemoveAuthorizedKeyRequest", req)
	}
	if err := sshService(svc).RemoveAuthorizedKey(ctx, service.SSHAuthorizedKeyRef{
		Scope: sdkReq.Scope, Owner: sdkReq.Owner, Name: sdkReq.Name,
	}); err != nil {
		return nil, err
	}
	return &sshclient.AckResponse{Success: true}, nil
}

func handleSSHEmitAuthorizedKey(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.EmitRequest)
	if !ok {
		return nil, sshTypeError(sshclient.MethodEmitAuthorizedKey, "*ssh.EmitRequest", req)
	}
	line, err := sshService(svc).EmitAuthorizedKey(ctx, *sdkReq)
	if err != nil {
		return nil, err
	}
	return &sshclient.EmitResponse{Line: line}, nil
}
