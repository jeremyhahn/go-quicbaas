// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package server

import (
	"context"
	"errors"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// stubBarrierClient is a fully-controllable transport.BarrierClient for testing
// the qrdbBarrierClientServicer adapter. Each method returns the pre-seeded
// response/error and records the request it received.
type stubBarrierClient struct {
	initErr error

	unsealErr error

	sealErr error

	statusResp *transport.BarrierStatusResponse
	statusErr  error

	shamirResp *transport.BarrierInitializeShamirResponse
	shamirErr  error

	shareResp *transport.BarrierUnsealShareResponse
	shareErr  error

	unsealSharesErr error

	rekeyResp *transport.BarrierRekeyResponse
	rekeyErr  error

	recoveryResp *transport.BarrierRecoveryKeysResponse
	recoveryErr  error

	recoverErr error

	deleteRecoveryErr error

	hasRecoveryResp *transport.BarrierHasRecoveryKeysResponse
	hasRecoveryErr  error

	rootTokenResp *transport.BarrierRootTokenResponse
	rootTokenErr  error

	lastInitSecret   string
	lastUnsealSecret string
}

func (s *stubBarrierClient) BarrierInitialize(_ context.Context, req *transport.BarrierInitializeRequest) error {
	s.lastInitSecret = req.Secret
	return s.initErr
}

func (s *stubBarrierClient) BarrierUnseal(_ context.Context, req *transport.BarrierUnsealRequest) error {
	s.lastUnsealSecret = req.Secret
	return s.unsealErr
}

func (s *stubBarrierClient) BarrierSeal(_ context.Context) error { return s.sealErr }

func (s *stubBarrierClient) BarrierStatus(_ context.Context) (*transport.BarrierStatusResponse, error) {
	return s.statusResp, s.statusErr
}

func (s *stubBarrierClient) BarrierInitializeShamir(_ context.Context, _ *transport.BarrierInitializeShamirRequest) (*transport.BarrierInitializeShamirResponse, error) {
	return s.shamirResp, s.shamirErr
}

func (s *stubBarrierClient) BarrierUnsealWithShare(_ context.Context, _ *transport.BarrierUnsealShareRequest) (*transport.BarrierUnsealShareResponse, error) {
	return s.shareResp, s.shareErr
}

func (s *stubBarrierClient) BarrierUnsealWithShares(_ context.Context, _ *transport.BarrierUnsealSharesRequest) error {
	return s.unsealSharesErr
}

func (s *stubBarrierClient) BarrierRekey(_ context.Context, _ *transport.BarrierRekeyRequest) (*transport.BarrierRekeyResponse, error) {
	return s.rekeyResp, s.rekeyErr
}

func (s *stubBarrierClient) BarrierGenerateRecoveryKeys(_ context.Context, _ *transport.BarrierGenerateRecoveryKeysRequest) (*transport.BarrierRecoveryKeysResponse, error) {
	return s.recoveryResp, s.recoveryErr
}

func (s *stubBarrierClient) BarrierRecoverWithKeys(_ context.Context, _ *transport.BarrierRecoverWithKeysRequest) error {
	return s.recoverErr
}

func (s *stubBarrierClient) BarrierDeleteRecoveryKeys(_ context.Context) error {
	return s.deleteRecoveryErr
}

func (s *stubBarrierClient) BarrierHasRecoveryKeys(_ context.Context) (*transport.BarrierHasRecoveryKeysResponse, error) {
	return s.hasRecoveryResp, s.hasRecoveryErr
}

func (s *stubBarrierClient) BarrierGenerateRootToken(_ context.Context, _ *transport.BarrierGenerateRootTokenRequest) (*transport.BarrierRootTokenResponse, error) {
	return s.rootTokenResp, s.rootTokenErr
}

func TestQRDBBarrierClientServicer_Initialize(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		c := &stubBarrierClient{}
		s := newQRDBBarrierClientServicer(c)
		if err := s.BarrierInitialize(ctx, "secret"); err != nil {
			t.Fatalf("BarrierInitialize: %v", err)
		}
		if c.lastInitSecret != "secret" {
			t.Fatalf("secret not propagated: %q", c.lastInitSecret)
		}
	})

	t.Run("already initialized treated as success", func(t *testing.T) {
		c := &stubBarrierClient{initErr: qrdbsdk.ErrAlreadyInitialized}
		s := newQRDBBarrierClientServicer(c)
		if err := s.BarrierInitialize(ctx, "x"); err != nil {
			t.Fatalf("expected nil for already-initialized, got %v", err)
		}
	})

	t.Run("error propagated", func(t *testing.T) {
		want := errors.New("boom")
		c := &stubBarrierClient{initErr: want}
		s := newQRDBBarrierClientServicer(c)
		if err := s.BarrierInitialize(ctx, "x"); !errors.Is(err, want) {
			t.Fatalf("expected %v, got %v", want, err)
		}
	})
}

func TestQRDBBarrierClientServicer_UnsealSeal(t *testing.T) {
	ctx := context.Background()

	c := &stubBarrierClient{}
	s := newQRDBBarrierClientServicer(c)
	if err := s.BarrierUnseal(ctx, "k"); err != nil {
		t.Fatalf("BarrierUnseal: %v", err)
	}
	if c.lastUnsealSecret != "k" {
		t.Fatalf("unseal secret not propagated")
	}
	if err := s.BarrierSeal(ctx); err != nil {
		t.Fatalf("BarrierSeal: %v", err)
	}

	want := errors.New("seal fail")
	c2 := &stubBarrierClient{unsealErr: want, sealErr: want}
	s2 := newQRDBBarrierClientServicer(c2)
	if err := s2.BarrierUnseal(ctx, "k"); !errors.Is(err, want) {
		t.Fatalf("unseal err: %v", err)
	}
	if err := s2.BarrierSeal(ctx); !errors.Is(err, want) {
		t.Fatalf("seal err: %v", err)
	}
}

func TestQRDBBarrierClientServicer_Status(t *testing.T) {
	ctx := context.Background()

	t.Run("parses rfc3339", func(t *testing.T) {
		ts := "2026-01-02T15:04:05Z"
		c := &stubBarrierClient{statusResp: &transport.BarrierStatusResponse{
			Sealed: true, Strategy: "shamir", HardwareBacked: true, InitializedAt: ts,
		}}
		s := newQRDBBarrierClientServicer(c)
		info, err := s.BarrierStatus(ctx)
		if err != nil {
			t.Fatalf("BarrierStatus: %v", err)
		}
		if !info.Sealed || info.Strategy != "shamir" || !info.HardwareBacked {
			t.Fatalf("status mapping wrong: %+v", info)
		}
		want, _ := time.Parse(time.RFC3339, ts)
		if !info.InitializedAt.Equal(want) {
			t.Fatalf("timestamp not parsed: %v", info.InitializedAt)
		}
	})

	t.Run("parses rfc3339nano fallback", func(t *testing.T) {
		ts := "2026-01-02T15:04:05.123456789Z"
		c := &stubBarrierClient{statusResp: &transport.BarrierStatusResponse{InitializedAt: ts}}
		s := newQRDBBarrierClientServicer(c)
		info, err := s.BarrierStatus(ctx)
		if err != nil {
			t.Fatalf("BarrierStatus: %v", err)
		}
		if info.InitializedAt.IsZero() {
			t.Fatalf("nano timestamp not parsed")
		}
	})

	t.Run("invalid timestamp ignored", func(t *testing.T) {
		c := &stubBarrierClient{statusResp: &transport.BarrierStatusResponse{InitializedAt: "not-a-time"}}
		s := newQRDBBarrierClientServicer(c)
		info, err := s.BarrierStatus(ctx)
		if err != nil {
			t.Fatalf("BarrierStatus: %v", err)
		}
		if !info.InitializedAt.IsZero() {
			t.Fatalf("invalid timestamp should be zero")
		}
	})

	t.Run("error", func(t *testing.T) {
		want := errors.New("nope")
		c := &stubBarrierClient{statusErr: want}
		s := newQRDBBarrierClientServicer(c)
		if _, err := s.BarrierStatus(ctx); !errors.Is(err, want) {
			t.Fatalf("expected err, got %v", err)
		}
	})
}

func TestQRDBBarrierClientServicer_Shamir(t *testing.T) {
	ctx := context.Background()

	c := &stubBarrierClient{shamirResp: &transport.BarrierInitializeShamirResponse{
		Shares: []string{"a", "b"}, Threshold: 2, TotalShares: 3,
	}}
	s := newQRDBBarrierClientServicer(c)
	info, err := s.BarrierInitializeShamir(ctx, "secret", 2, 3)
	if err != nil {
		t.Fatalf("BarrierInitializeShamir: %v", err)
	}
	if len(info.Shares) != 2 || info.Threshold != 2 || info.TotalShares != 3 {
		t.Fatalf("shamir mapping wrong: %+v", info)
	}

	want := errors.New("shamir fail")
	c2 := &stubBarrierClient{shamirErr: want}
	s2 := newQRDBBarrierClientServicer(c2)
	if _, err := s2.BarrierInitializeShamir(ctx, "s", 1, 1); !errors.Is(err, want) {
		t.Fatalf("expected err, got %v", err)
	}
}

func TestQRDBBarrierClientServicer_UnsealWithShare(t *testing.T) {
	ctx := context.Background()

	c := &stubBarrierClient{shareResp: &transport.BarrierUnsealShareResponse{
		Required: 3, Submitted: 1, Complete: false,
	}}
	s := newQRDBBarrierClientServicer(c)
	prog, err := s.BarrierUnsealWithShare(ctx, "share")
	if err != nil {
		t.Fatalf("BarrierUnsealWithShare: %v", err)
	}
	if prog.Required != 3 || prog.Submitted != 1 || prog.Complete {
		t.Fatalf("progress mapping wrong: %+v", prog)
	}

	want := errors.New("share fail")
	c2 := &stubBarrierClient{shareErr: want}
	s2 := newQRDBBarrierClientServicer(c2)
	if _, err := s2.BarrierUnsealWithShare(ctx, "x"); !errors.Is(err, want) {
		t.Fatalf("expected err, got %v", err)
	}
}

func TestQRDBBarrierClientServicer_UnsealWithShares(t *testing.T) {
	ctx := context.Background()

	c := &stubBarrierClient{}
	s := newQRDBBarrierClientServicer(c)
	if err := s.BarrierUnsealWithShares(ctx, []string{"a", "b"}); err != nil {
		t.Fatalf("BarrierUnsealWithShares: %v", err)
	}

	want := errors.New("fail")
	c2 := &stubBarrierClient{unsealSharesErr: want}
	s2 := newQRDBBarrierClientServicer(c2)
	if err := s2.BarrierUnsealWithShares(ctx, nil); !errors.Is(err, want) {
		t.Fatalf("expected err, got %v", err)
	}
}

func TestQRDBBarrierClientServicer_Rekey(t *testing.T) {
	ctx := context.Background()

	c := &stubBarrierClient{rekeyResp: &transport.BarrierRekeyResponse{
		Shares: []string{"x"}, Threshold: 1, TotalShares: 1,
	}}
	s := newQRDBBarrierClientServicer(c)
	info, err := s.BarrierRekey(ctx, 1, 1)
	if err != nil {
		t.Fatalf("BarrierRekey: %v", err)
	}
	if len(info.Shares) != 1 || info.Threshold != 1 || info.TotalShares != 1 {
		t.Fatalf("rekey mapping wrong: %+v", info)
	}

	want := errors.New("rekey fail")
	c2 := &stubBarrierClient{rekeyErr: want}
	s2 := newQRDBBarrierClientServicer(c2)
	if _, err := s2.BarrierRekey(ctx, 1, 1); !errors.Is(err, want) {
		t.Fatalf("expected err, got %v", err)
	}
}

func TestQRDBBarrierClientServicer_RecoveryKeys(t *testing.T) {
	ctx := context.Background()

	c := &stubBarrierClient{recoveryResp: &transport.BarrierRecoveryKeysResponse{
		Keys: []string{"r1", "r2"}, Threshold: 2, Total: 3,
	}}
	s := newQRDBBarrierClientServicer(c)
	info, err := s.BarrierGenerateRecoveryKeys(ctx, 2, 3)
	if err != nil {
		t.Fatalf("BarrierGenerateRecoveryKeys: %v", err)
	}
	if len(info.Keys) != 2 || info.Threshold != 2 || info.Total != 3 {
		t.Fatalf("recovery mapping wrong: %+v", info)
	}

	want := errors.New("rec fail")
	c2 := &stubBarrierClient{recoveryErr: want}
	s2 := newQRDBBarrierClientServicer(c2)
	if _, err := s2.BarrierGenerateRecoveryKeys(ctx, 1, 1); !errors.Is(err, want) {
		t.Fatalf("expected err, got %v", err)
	}
}

func TestQRDBBarrierClientServicer_RecoverDeleteHas(t *testing.T) {
	ctx := context.Background()

	c := &stubBarrierClient{hasRecoveryResp: &transport.BarrierHasRecoveryKeysResponse{HasKeys: true}}
	s := newQRDBBarrierClientServicer(c)
	if err := s.BarrierRecoverWithKeys(ctx, []string{"a"}); err != nil {
		t.Fatalf("BarrierRecoverWithKeys: %v", err)
	}
	if err := s.BarrierDeleteRecoveryKeys(ctx); err != nil {
		t.Fatalf("BarrierDeleteRecoveryKeys: %v", err)
	}
	has, err := s.BarrierHasRecoveryKeys(ctx)
	if err != nil || !has {
		t.Fatalf("BarrierHasRecoveryKeys: has=%v err=%v", has, err)
	}

	want := errors.New("fail")
	c2 := &stubBarrierClient{recoverErr: want, deleteRecoveryErr: want, hasRecoveryErr: want}
	s2 := newQRDBBarrierClientServicer(c2)
	if err := s2.BarrierRecoverWithKeys(ctx, nil); !errors.Is(err, want) {
		t.Fatalf("recover err: %v", err)
	}
	if err := s2.BarrierDeleteRecoveryKeys(ctx); !errors.Is(err, want) {
		t.Fatalf("delete err: %v", err)
	}
	if has, err := s2.BarrierHasRecoveryKeys(ctx); err == nil || has {
		t.Fatalf("has err expected, got has=%v err=%v", has, err)
	}
}

func TestQRDBBarrierClientServicer_GenerateRootToken(t *testing.T) {
	ctx := context.Background()

	ts := "2026-03-04T05:06:07Z"
	c := &stubBarrierClient{rootTokenResp: &transport.BarrierRootTokenResponse{Token: "tok", CreatedAt: ts}}
	s := newQRDBBarrierClientServicer(c)
	info, err := s.BarrierGenerateRootToken(ctx, []string{"s1"})
	if err != nil {
		t.Fatalf("BarrierGenerateRootToken: %v", err)
	}
	if info.Token != "tok" || info.CreatedAt.IsZero() {
		t.Fatalf("root token mapping wrong: %+v", info)
	}

	// nano fallback + invalid path
	c.rootTokenResp = &transport.BarrierRootTokenResponse{Token: "t", CreatedAt: "2026-03-04T05:06:07.5Z"}
	if info, err := s.BarrierGenerateRootToken(ctx, nil); err != nil || info.CreatedAt.IsZero() {
		t.Fatalf("nano path: info=%+v err=%v", info, err)
	}
	c.rootTokenResp = &transport.BarrierRootTokenResponse{Token: "t", CreatedAt: "bad"}
	if info, err := s.BarrierGenerateRootToken(ctx, nil); err != nil || !info.CreatedAt.IsZero() {
		t.Fatalf("bad timestamp path: info=%+v err=%v", info, err)
	}

	want := errors.New("rt fail")
	c2 := &stubBarrierClient{rootTokenErr: want}
	s2 := newQRDBBarrierClientServicer(c2)
	if _, err := s2.BarrierGenerateRootToken(ctx, nil); !errors.Is(err, want) {
		t.Fatalf("expected err, got %v", err)
	}
}
