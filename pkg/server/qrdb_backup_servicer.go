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

package server

import (
	"context"

	"github.com/jeremyhahn/go-qrdb/pkg/api/errclass"
	qrdbgrpc "github.com/jeremyhahn/go-qrdb/pkg/api/grpc"
	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
)

// qrdbBackupServicerProxy serves the qrdb.v1.QRDBService Backup* RPCs on
// QuicBaaS's own gRPC servers (the Unix socket listener and the sealed-window
// server) from the embedded qrdb node's backup service, the same one the
// node's TCP transports serve.
//
// Registration runs before the node is built and unsealed, so the servicer is
// resolved PER CALL instead of being captured at registration time. While it
// is absent, every call fails closed with go-qrdb's
// ServiceNotConfiguredError: go-qrdb classifies delegate errors, so a typed
// error keeps the answer Unimplemented, exactly as an unregistered backup
// service answers, rather than a raw status coming out as Internal.
//
// CreateBackup is refused while no durable backup sink is configured, because
// the node's backup service would keep the backup in memory only.
type qrdbBackupServicerProxy struct {
	resolve func() qrdbservice.BackupServicer
	// durableSink reports that the node's backup service writes to a
	// durable backup sink. No quicbaas node configures one, so
	// newQRDBBackupServicerProxy leaves it false and CreateBackup is
	// refused.
	durableSink bool
}

var _ qrdbservice.BackupServicer = (*qrdbBackupServicerProxy)(nil)

// newQRDBBackupServicerProxy returns a proxy that resolves s's embedded node
// backup service per call. A nil s yields a proxy that always fails closed.
func newQRDBBackupServicerProxy(s *Server) *qrdbBackupServicerProxy {
	p := &qrdbBackupServicerProxy{}
	if s != nil {
		p.resolve = s.GetQRDBBackupServicer
	}
	return p
}

func (p *qrdbBackupServicerProxy) servicer() (qrdbservice.BackupServicer, error) {
	if p.resolve != nil {
		if svc := p.resolve(); svc != nil {
			return svc, nil
		}
	}
	return nil, &qrdbgrpc.ServiceNotConfiguredError{
		Service: "backup",
		Hint:    "embedded qrdb node absent or sealed",
	}
}

// qrdbBackupEncryptionUnsupportedError refuses a backup create that asks for
// encryption, or a restore that carries a decryption key. QuicBaaS does not
// support encrypted backups, so key material never reaches the node through
// this surface, whoever the gRPC caller is. Its class is Unimplemented, so
// go-qrdb answers codes.Unimplemented.
type qrdbBackupEncryptionUnsupportedError struct {
	Op string
}

func (e *qrdbBackupEncryptionUnsupportedError) Error() string {
	return "backup " + e.Op + ": backup encryption is not supported; use a plaintext backup"
}

// ErrorClass classes the refusal as Unimplemented.
func (e *qrdbBackupEncryptionUnsupportedError) ErrorClass() errclass.Class {
	return errclass.Unimplemented
}

// qrdbBackupNotDurableError refuses a backup create while the node's
// backup service has no durable sink: go-qrdb would keep the backup in
// memory only, so it would not survive a restart. Its class is
// Unimplemented, so go-qrdb answers codes.Unimplemented.
type qrdbBackupNotDurableError struct{}

func (*qrdbBackupNotDurableError) Error() string {
	return "backup create: this node's qrdb backup service keeps backups in memory only, " +
		"so the backup would not survive a restart; use the QuicBaaS backup service for durable backups"
}

// ErrorClass classes the refusal as Unimplemented.
func (*qrdbBackupNotDurableError) ErrorClass() errclass.Class {
	return errclass.Unimplemented
}

// CreateBackup delegates to the node's backup service. A request for
// encryption is refused before the node is resolved; an absent node answers
// ServiceNotConfiguredError; a create while no durable sink is configured is
// refused and never reaches the node.
func (p *qrdbBackupServicerProxy) CreateBackup(ctx context.Context, opts qrdbservice.BackupOptions) (*qrdbservice.BackupInfo, error) {
	if (opts.Encryption != "" && opts.Encryption != qrdbservice.EncryptionNone) || len(opts.EncryptionKey) > 0 {
		return nil, &qrdbBackupEncryptionUnsupportedError{Op: "create"}
	}
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	if !p.durableSink {
		return nil, &qrdbBackupNotDurableError{}
	}
	return svc.CreateBackup(ctx, opts)
}

// ListBackups delegates to the node's backup service.
func (p *qrdbBackupServicerProxy) ListBackups(ctx context.Context) ([]qrdbservice.BackupInfo, error) {
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return svc.ListBackups(ctx)
}

// GetBackup delegates to the node's backup service.
func (p *qrdbBackupServicerProxy) GetBackup(ctx context.Context, id string) (*qrdbservice.BackupInfo, error) {
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return svc.GetBackup(ctx, id)
}

// DeleteBackup delegates to the node's backup service.
func (p *qrdbBackupServicerProxy) DeleteBackup(ctx context.Context, id string) error {
	svc, err := p.servicer()
	if err != nil {
		return err
	}
	return svc.DeleteBackup(ctx, id)
}

// RestoreBackup delegates to the node's backup service. A request carrying a
// decryption key is refused before the node is resolved.
func (p *qrdbBackupServicerProxy) RestoreBackup(ctx context.Context, opts qrdbservice.RestoreOptions) (*qrdbservice.RestoreResult, error) {
	if len(opts.DecryptionKey) > 0 {
		return nil, &qrdbBackupEncryptionUnsupportedError{Op: "restore"}
	}
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return svc.RestoreBackup(ctx, opts)
}

// VerifyBackup delegates to the node's backup service.
func (p *qrdbBackupServicerProxy) VerifyBackup(ctx context.Context, id string) (*qrdbservice.VerifyResult, error) {
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return svc.VerifyBackup(ctx, id)
}
