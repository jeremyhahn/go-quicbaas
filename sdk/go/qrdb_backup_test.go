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

package quicbaas

import (
	"context"
	"testing"

	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// stubBackupService is a go-qrdb BackupServicer that records the restore it
// was asked for and lists one backup.
type stubBackupService struct {
	created      *qrdbservice.BackupOptions
	restored     qrdbservice.RestoreOptions
	restoreCalls int
}

func (s *stubBackupService) CreateBackup(_ context.Context, opts qrdbservice.BackupOptions) (*qrdbservice.BackupInfo, error) {
	s.created = &opts
	return &qrdbservice.BackupInfo{ID: "bk-new", Destination: opts.Destination}, nil
}

func (s *stubBackupService) ListBackups(context.Context) ([]qrdbservice.BackupInfo, error) {
	return []qrdbservice.BackupInfo{{ID: "bk-1", ShardCount: 3}}, nil
}

func (s *stubBackupService) GetBackup(_ context.Context, id string) (*qrdbservice.BackupInfo, error) {
	return &qrdbservice.BackupInfo{ID: id}, nil
}

func (s *stubBackupService) DeleteBackup(context.Context, string) error { return nil }

func (s *stubBackupService) RestoreBackup(_ context.Context, opts qrdbservice.RestoreOptions) (*qrdbservice.RestoreResult, error) {
	s.restored = opts
	s.restoreCalls++
	return &qrdbservice.RestoreResult{BackupID: opts.BackupID, ShardsRestored: 3}, nil
}

func (s *stubBackupService) VerifyBackup(_ context.Context, id string) (*qrdbservice.VerifyResult, error) {
	return &qrdbservice.VerifyResult{Valid: true, ChecksumMatch: true}, nil
}

// TestStorageQRDBBackup_RoutesThroughEmbeddedTransport proves go-qrdb's
// BackupClient is re-exported through sdk/go: Storage().QRDBBackup() returns
// the qrdb client, and its list and restore reach the qrdb backup service.
func TestStorageQRDBBackup_RoutesThroughEmbeddedTransport(t *testing.T) {
	svc := &stubBackupService{}
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{
		KV:     stubKVService{},
		Admin:  stubAdminService{},
		Backup: svc,
	})

	var bc QRDBBackupClient
	bc, err := c.Storage().QRDBBackup()
	if err != nil {
		t.Fatalf("Storage().QRDBBackup(): %v", err)
	}
	if bc == nil {
		t.Fatal("expected a non-nil QRDBBackupClient")
	}

	list, err := bc.ListBackups(context.Background())
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(list) != 1 || list[0].ID != "bk-1" || list[0].ShardCount != 3 {
		t.Fatalf("ListBackups: got %+v, want one backup bk-1 with 3 shards", list)
	}

	var res *QRDBRestoreResult
	res, err = bc.RestoreBackup(context.Background(), &QRDBRestoreRequest{BackupID: "bk-1", ValidateFirst: true})
	if err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	if res.BackupID != "bk-1" || res.ShardsRestored != 3 {
		t.Errorf("RestoreBackup: got %+v, want bk-1 with 3 shards restored", res)
	}
	if svc.restored.BackupID != "bk-1" || !svc.restored.ValidateFirst {
		t.Errorf("backup service got restore %+v, want bk-1 validated first", svc.restored)
	}

	var vr *QRDBVerifyResult
	if vr, err = bc.VerifyBackup(context.Background(), "bk-1"); err != nil || !vr.Valid {
		t.Errorf("VerifyBackup: got %+v, %v; want valid", vr, err)
	}
}

// TestStorageQRDBBackup_RefusesEncryption: the pinned go-qrdb backup service
// stores plaintext whatever encryption is asked for and restores with any
// key, so QRDBBackup's client refuses encryption and decryption keys with
// Unimplemented before the service sees the call; plaintext requests pass.
func TestStorageQRDBBackup_RefusesEncryption(t *testing.T) {
	svc := &stubBackupService{}
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{
		KV:     stubKVService{},
		Admin:  stubAdminService{},
		Backup: svc,
	})
	bc, err := c.Storage().QRDBBackup()
	if err != nil {
		t.Fatalf("Storage().QRDBBackup(): %v", err)
	}
	ctx := context.Background()

	refused := map[string]*QRDBBackupRequest{
		"encryption type and key": {Destination: "/b", Encryption: "aes256", EncryptionKey: []byte("k")},
		"encryption key only":     {Destination: "/b", EncryptionKey: []byte("k")},
	}
	for name, req := range refused {
		t.Run("CreateBackup/"+name, func(t *testing.T) {
			if _, err := bc.CreateBackup(ctx, req); !sdkerrors.IsUnimplemented(err) {
				t.Errorf("CreateBackup: got %v, want Unimplemented", err)
			}
			if svc.created != nil {
				t.Errorf("backup service saw CreateBackup %+v, want no call", *svc.created)
			}
		})
	}

	t.Run("RestoreBackup/decryption key", func(t *testing.T) {
		_, err := bc.RestoreBackup(ctx, &QRDBRestoreRequest{BackupID: "bk-1", DecryptionKey: []byte("k")})
		if !sdkerrors.IsUnimplemented(err) {
			t.Errorf("RestoreBackup: got %v, want Unimplemented", err)
		}
		if svc.restoreCalls != 0 {
			t.Errorf("backup service saw %d RestoreBackup calls, want 0", svc.restoreCalls)
		}
	})

	t.Run("CreateBackup/plaintext", func(t *testing.T) {
		if _, err := bc.CreateBackup(ctx, &QRDBBackupRequest{Destination: "/b"}); err != nil {
			t.Fatalf("CreateBackup: %v", err)
		}
		if svc.created == nil || svc.created.Destination != "/b" {
			t.Fatalf("backup service got %+v, want destination /b", svc.created)
		}
		svc.created = nil
		if _, err := bc.CreateBackup(ctx, &QRDBBackupRequest{Destination: "/b", Encryption: "none"}); err != nil {
			t.Fatalf("CreateBackup with encryption none: %v", err)
		}
		if svc.created == nil {
			t.Fatal("backup service saw no CreateBackup with encryption none")
		}
	})
}

// TestClient_QRDBBackup_PropagatesInitError: a QRDB client that cannot be
// built yields its error, never a nil client with a nil error.
func TestClient_QRDBBackup_PropagatesInitError(t *testing.T) {
	c := newBrokenEmbeddedClient(t)
	bc, err := c.QRDBBackup()
	if err == nil {
		t.Fatal("expected error from misconfigured embedded qrdb")
	}
	if bc != nil {
		t.Errorf("expected a nil client with the error, got %T", bc)
	}
	if !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected the invalid-input build error, got %v", err)
	}
}

// TestStorageQRDBBackup_EmbeddedWithoutBackupServiceIsUnimplemented: valid
// embedded KV and Admin without a Backup servicer yield Unimplemented and a
// nil client, not a client that fails on first use.
func TestStorageQRDBBackup_EmbeddedWithoutBackupServiceIsUnimplemented(t *testing.T) {
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{
		KV:    stubKVService{},
		Admin: stubAdminService{},
	})
	bc, err := c.Storage().QRDBBackup()
	if err == nil {
		t.Fatal("expected an error without a backup servicer")
	}
	if bc != nil {
		t.Errorf("expected a nil client with the error, got %T", bc)
	}
	if !sdkerrors.IsUnimplemented(err) {
		t.Errorf("expected an Unimplemented error, got %v", err)
	}
}
