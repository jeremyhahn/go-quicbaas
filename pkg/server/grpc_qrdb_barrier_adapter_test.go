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

	officerRegisterResp *transport.OfficerRegisterResponse
	officerRegisterErr  error

	officerListResp []*transport.OfficerStatus
	officerListErr  error

	claimStatusResp []*transport.OfficerStatus
	claimStatusErr  error

	claimBeginResp *transport.ShareClaimBeginResponse
	claimBeginErr  error

	claimCompleteResp *transport.ShareClaimCompleteResponse
	claimCompleteErr  error

	sealVoteResp *transport.ShamirSealVoteResponse
	sealVoteErr  error

	recoveryRegisterResp *transport.OfficerRegisterResponse
	recoveryRegisterErr  error

	recoveryClaimBeginResp *transport.ShareClaimBeginResponse
	recoveryClaimBeginErr  error

	recoveryClaimCompleteResp *transport.ShareClaimCompleteResponse
	recoveryClaimCompleteErr  error

	lastInitSecret       string
	lastUnsealSecret     string
	lastOfficerRegister  *transport.OfficerRegisterRequest
	lastClaimBegin       *transport.ShareClaimBeginRequest
	lastClaimComplete    *transport.ShareClaimCompleteRequest
	lastSealVoteShare    string
	lastRecoveryRegister *transport.OfficerRegisterRequest
	lastRecoveryBegin    *transport.ShareClaimBeginRequest
	lastRecoveryComplete *transport.ShareClaimCompleteRequest
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

func (s *stubBarrierClient) OfficerRegister(_ context.Context, req *transport.OfficerRegisterRequest) (*transport.OfficerRegisterResponse, error) {
	s.lastOfficerRegister = req
	return s.officerRegisterResp, s.officerRegisterErr
}

func (s *stubBarrierClient) OfficerList(_ context.Context) ([]*transport.OfficerStatus, error) {
	return s.officerListResp, s.officerListErr
}

func (s *stubBarrierClient) OfficerClaimStatus(_ context.Context) ([]*transport.OfficerStatus, error) {
	return s.claimStatusResp, s.claimStatusErr
}

func (s *stubBarrierClient) ShareClaimBegin(_ context.Context, req *transport.ShareClaimBeginRequest) (*transport.ShareClaimBeginResponse, error) {
	s.lastClaimBegin = req
	return s.claimBeginResp, s.claimBeginErr
}

func (s *stubBarrierClient) ShareClaimComplete(_ context.Context, req *transport.ShareClaimCompleteRequest) (*transport.ShareClaimCompleteResponse, error) {
	s.lastClaimComplete = req
	return s.claimCompleteResp, s.claimCompleteErr
}

func (s *stubBarrierClient) ShamirSealVote(_ context.Context, req *transport.ShamirSealVoteRequest) (*transport.ShamirSealVoteResponse, error) {
	s.lastSealVoteShare = req.Share
	return s.sealVoteResp, s.sealVoteErr
}

func (s *stubBarrierClient) RecoveryOfficerRegister(_ context.Context, req *transport.OfficerRegisterRequest) (*transport.OfficerRegisterResponse, error) {
	s.lastRecoveryRegister = req
	return s.recoveryRegisterResp, s.recoveryRegisterErr
}

func (s *stubBarrierClient) RecoveryShareClaimBegin(_ context.Context, req *transport.ShareClaimBeginRequest) (*transport.ShareClaimBeginResponse, error) {
	s.lastRecoveryBegin = req
	return s.recoveryClaimBeginResp, s.recoveryClaimBeginErr
}

func (s *stubBarrierClient) RecoveryShareClaimComplete(_ context.Context, req *transport.ShareClaimCompleteRequest) (*transport.ShareClaimCompleteResponse, error) {
	s.lastRecoveryComplete = req
	return s.recoveryClaimCompleteResp, s.recoveryClaimCompleteErr
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
		Threshold: 2, TotalShares: 3, ShareCount: 3,
	}}
	s := newQRDBBarrierClientServicer(c)
	info, err := s.BarrierInitializeShamir(ctx, "secret", 2, 3)
	if err != nil {
		t.Fatalf("BarrierInitializeShamir: %v", err)
	}
	if info.Threshold != 2 || info.TotalShares != 3 || info.ShareCount != 3 {
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
		Threshold: 1, TotalShares: 1, ShareCount: 1,
	}}
	s := newQRDBBarrierClientServicer(c)
	info, err := s.BarrierRekey(ctx, 1, 1)
	if err != nil {
		t.Fatalf("BarrierRekey: %v", err)
	}
	if info.Threshold != 1 || info.TotalShares != 1 || info.ShareCount != 1 {
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
		Threshold: 2, Total: 3, ShareCount: 3,
	}}
	s := newQRDBBarrierClientServicer(c)
	info, err := s.BarrierGenerateRecoveryKeys(ctx, 2, 3)
	if err != nil {
		t.Fatalf("BarrierGenerateRecoveryKeys: %v", err)
	}
	if info.Threshold != 2 || info.Total != 3 || info.ShareCount != 3 {
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

func TestQRDBBarrierClientServicer_OfficerRegister(t *testing.T) {
	ctx := context.Background()

	c := &stubBarrierClient{officerRegisterResp: &transport.OfficerRegisterResponse{
		CertPEM: []byte("cert"), ShareIndex: 2,
	}}
	s := newQRDBBarrierClientServicer(c)
	result, err := s.BarrierOfficerRegister(ctx, &qrdbsdk.BarrierOfficerRegisterRequest{
		Username:          "so-1",
		CSRPEM:            []byte("csr"),
		AuthenticatorType: "piv",
		AuthenticatorRef:  "slot-9a",
	})
	if err != nil {
		t.Fatalf("BarrierOfficerRegister: %v", err)
	}
	if string(result.CertPEM) != "cert" || result.ShareIndex != 2 {
		t.Fatalf("register mapping wrong: %+v", result)
	}
	req := c.lastOfficerRegister
	if req.Username != "so-1" || string(req.CSRPEM) != "csr" ||
		req.AuthenticatorType != "piv" || req.AuthenticatorRef != "slot-9a" {
		t.Fatalf("register request not propagated: %+v", req)
	}

	want := errors.New("register fail")
	c2 := &stubBarrierClient{officerRegisterErr: want}
	s2 := newQRDBBarrierClientServicer(c2)
	if _, err := s2.BarrierOfficerRegister(ctx, &qrdbsdk.BarrierOfficerRegisterRequest{Username: "x"}); !errors.Is(err, want) {
		t.Fatalf("expected err, got %v", err)
	}
}

func TestQRDBBarrierClientServicer_OfficerListClaimStatus(t *testing.T) {
	ctx := context.Background()

	enrolled := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	claimed := enrolled.Add(time.Hour)
	officers := []*transport.OfficerStatus{
		nil, // nil entries are skipped, never dereferenced
		{
			Username:       "so-1",
			CertThumbprint: "thumb",
			ShareIndex:     1,
			AuthFactor:     "piv",
			EnrolledAt:     enrolled,
			ClaimedAt:      claimed,
			SharePending:   true,
		},
	}

	c := &stubBarrierClient{officerListResp: officers, claimStatusResp: officers}
	s := newQRDBBarrierClientServicer(c)

	for name, call := range map[string]func() ([]*qrdbsdk.BarrierOfficerStatus, error){
		"OfficerList":        func() ([]*qrdbsdk.BarrierOfficerStatus, error) { return s.BarrierOfficerList(ctx) },
		"OfficerClaimStatus": func() ([]*qrdbsdk.BarrierOfficerStatus, error) { return s.BarrierOfficerClaimStatus(ctx) },
	} {
		got, err := call()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(got) != 1 {
			t.Fatalf("%s: expected nil entry skipped, got %d entries", name, len(got))
		}
		o := got[0]
		if o.Username != "so-1" || o.CertThumbprint != "thumb" || o.ShareIndex != 1 ||
			o.AuthFactor != "piv" || !o.EnrolledAt.Equal(enrolled) ||
			!o.ClaimedAt.Equal(claimed) || !o.SharePending {
			t.Fatalf("%s mapping wrong: %+v", name, o)
		}
	}

	want := errors.New("list fail")
	c2 := &stubBarrierClient{officerListErr: want, claimStatusErr: want}
	s2 := newQRDBBarrierClientServicer(c2)
	if _, err := s2.BarrierOfficerList(ctx); !errors.Is(err, want) {
		t.Fatalf("list err: %v", err)
	}
	if _, err := s2.BarrierOfficerClaimStatus(ctx); !errors.Is(err, want) {
		t.Fatalf("claim status err: %v", err)
	}
}

func TestQRDBBarrierClientServicer_ShareClaimBegin(t *testing.T) {
	ctx := context.Background()

	expires := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	c := &stubBarrierClient{claimBeginResp: &transport.ShareClaimBeginResponse{
		NonceHex: "abcd", ExpiresAt: expires,
	}}
	s := newQRDBBarrierClientServicer(c)
	challenge, err := s.BarrierShareClaimBegin(ctx, "so-1", "local-thumbprint-ignored")
	if err != nil {
		t.Fatalf("BarrierShareClaimBegin: %v", err)
	}
	if challenge.NonceHex != "abcd" || !challenge.ExpiresAt.Equal(expires) {
		t.Fatalf("claim begin mapping wrong: %+v", challenge)
	}
	if c.lastClaimBegin.Username != "so-1" {
		t.Fatalf("username not propagated: %+v", c.lastClaimBegin)
	}

	want := errors.New("begin fail")
	c2 := &stubBarrierClient{claimBeginErr: want}
	s2 := newQRDBBarrierClientServicer(c2)
	if _, err := s2.BarrierShareClaimBegin(ctx, "so-1", ""); !errors.Is(err, want) {
		t.Fatalf("expected err, got %v", err)
	}
}

func TestQRDBBarrierClientServicer_ShareClaimComplete(t *testing.T) {
	ctx := context.Background()

	c := &stubBarrierClient{claimCompleteResp: &transport.ShareClaimCompleteResponse{
		ShareIndex: 3, ShareBytes: []byte("share"),
	}}
	s := newQRDBBarrierClientServicer(c)
	result, err := s.BarrierShareClaimComplete(ctx, "so-1", "abcd", []byte("sig"), "local-thumbprint-ignored")
	if err != nil {
		t.Fatalf("BarrierShareClaimComplete: %v", err)
	}
	if result.ShareIndex != 3 || string(result.ShareBytes) != "share" {
		t.Fatalf("claim complete mapping wrong: %+v", result)
	}
	req := c.lastClaimComplete
	if req.Username != "so-1" || req.NonceHex != "abcd" || string(req.Signature) != "sig" {
		t.Fatalf("claim complete request not propagated: %+v", req)
	}

	want := errors.New("complete fail")
	c2 := &stubBarrierClient{claimCompleteErr: want}
	s2 := newQRDBBarrierClientServicer(c2)
	if _, err := s2.BarrierShareClaimComplete(ctx, "so-1", "n", nil, ""); !errors.Is(err, want) {
		t.Fatalf("expected err, got %v", err)
	}
}

func TestQRDBBarrierClientServicer_ShamirSealVote(t *testing.T) {
	ctx := context.Background()

	c := &stubBarrierClient{sealVoteResp: &transport.ShamirSealVoteResponse{
		Required: 3, Submitted: 2, Complete: false,
	}}
	s := newQRDBBarrierClientServicer(c)
	prog, err := s.BarrierShamirSealVote(ctx, "share-1")
	if err != nil {
		t.Fatalf("BarrierShamirSealVote: %v", err)
	}
	if prog.Required != 3 || prog.Submitted != 2 || prog.Complete {
		t.Fatalf("seal vote mapping wrong: %+v", prog)
	}
	if c.lastSealVoteShare != "share-1" {
		t.Fatalf("share not propagated: %q", c.lastSealVoteShare)
	}

	want := errors.New("vote fail")
	c2 := &stubBarrierClient{sealVoteErr: want}
	s2 := newQRDBBarrierClientServicer(c2)
	if _, err := s2.BarrierShamirSealVote(ctx, "x"); !errors.Is(err, want) {
		t.Fatalf("expected err, got %v", err)
	}
}

func TestQRDBBarrierClientServicer_RecoveryOfficerRegister(t *testing.T) {
	ctx := context.Background()

	c := &stubBarrierClient{recoveryRegisterResp: &transport.OfficerRegisterResponse{
		CertPEM: []byte("rcert"), ShareIndex: 4,
	}}
	s := newQRDBBarrierClientServicer(c)
	result, err := s.BarrierRecoveryOfficerRegister(ctx, &qrdbsdk.BarrierOfficerRegisterRequest{
		Username: "ro-1", CSRPEM: []byte("rcsr"),
	})
	if err != nil {
		t.Fatalf("BarrierRecoveryOfficerRegister: %v", err)
	}
	if string(result.CertPEM) != "rcert" || result.ShareIndex != 4 {
		t.Fatalf("recovery register mapping wrong: %+v", result)
	}
	if c.lastRecoveryRegister.Username != "ro-1" || string(c.lastRecoveryRegister.CSRPEM) != "rcsr" {
		t.Fatalf("recovery register request not propagated: %+v", c.lastRecoveryRegister)
	}

	want := errors.New("rr fail")
	c2 := &stubBarrierClient{recoveryRegisterErr: want}
	s2 := newQRDBBarrierClientServicer(c2)
	if _, err := s2.BarrierRecoveryOfficerRegister(ctx, &qrdbsdk.BarrierOfficerRegisterRequest{Username: "x"}); !errors.Is(err, want) {
		t.Fatalf("expected err, got %v", err)
	}
}

func TestQRDBBarrierClientServicer_RecoveryShareClaim(t *testing.T) {
	ctx := context.Background()

	expires := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	c := &stubBarrierClient{
		recoveryClaimBeginResp: &transport.ShareClaimBeginResponse{
			NonceHex: "ef01", ExpiresAt: expires,
		},
		recoveryClaimCompleteResp: &transport.ShareClaimCompleteResponse{
			ShareIndex: 5, ShareBytes: []byte("rshare"),
		},
	}
	s := newQRDBBarrierClientServicer(c)

	challenge, err := s.BarrierRecoveryShareClaimBegin(ctx, "ro-1", "local-thumbprint-ignored")
	if err != nil {
		t.Fatalf("BarrierRecoveryShareClaimBegin: %v", err)
	}
	if challenge.NonceHex != "ef01" || !challenge.ExpiresAt.Equal(expires) {
		t.Fatalf("recovery begin mapping wrong: %+v", challenge)
	}
	if c.lastRecoveryBegin.Username != "ro-1" {
		t.Fatalf("recovery begin username not propagated: %+v", c.lastRecoveryBegin)
	}

	result, err := s.BarrierRecoveryShareClaimComplete(ctx, "ro-1", "ef01", []byte("rsig"), "local-thumbprint-ignored")
	if err != nil {
		t.Fatalf("BarrierRecoveryShareClaimComplete: %v", err)
	}
	if result.ShareIndex != 5 || string(result.ShareBytes) != "rshare" {
		t.Fatalf("recovery complete mapping wrong: %+v", result)
	}
	req := c.lastRecoveryComplete
	if req.Username != "ro-1" || req.NonceHex != "ef01" || string(req.Signature) != "rsig" {
		t.Fatalf("recovery complete request not propagated: %+v", req)
	}

	want := errors.New("recovery claim fail")
	c2 := &stubBarrierClient{recoveryClaimBeginErr: want, recoveryClaimCompleteErr: want}
	s2 := newQRDBBarrierClientServicer(c2)
	if _, err := s2.BarrierRecoveryShareClaimBegin(ctx, "ro-1", ""); !errors.Is(err, want) {
		t.Fatalf("recovery begin err: %v", err)
	}
	if _, err := s2.BarrierRecoveryShareClaimComplete(ctx, "ro-1", "n", nil, ""); !errors.Is(err, want) {
		t.Fatalf("recovery complete err: %v", err)
	}
}
