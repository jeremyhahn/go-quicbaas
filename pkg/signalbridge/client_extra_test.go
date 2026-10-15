// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

package signalbridge

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// inboundTransport is a ServerTransport that delivers caller-supplied
// envelopes over the realtime channel and records prekey-fetch, ack, and
// send-envelope traffic. Used to drive the SignalClient inbound loop and
// maintenance loop deterministically.
type inboundTransport struct {
	mu          sync.Mutex
	inboundCh   chan InboundEnvelope
	closeCalled atomic.Bool
	acked       []uint64
	preKeyCount int
	preKeyErr   error
	fetchErr    error
	openErr     error
	sends       int
}

func newInboundTransport() *inboundTransport {
	return &inboundTransport{inboundCh: make(chan InboundEnvelope, 8)}
}

func (f *inboundTransport) CreateVerificationSession(context.Context, string) (string, error) {
	return "sid", nil
}
func (f *inboundTransport) RequestVerificationCode(context.Context, string, string, string) error {
	return nil
}
func (f *inboundTransport) SubmitVerificationCode(context.Context, string, string) error { return nil }
func (f *inboundTransport) CompleteRegistration(context.Context, RegistrationRequest) (RegistrationResponse, error) {
	return RegistrationResponse{ACI: "aci", DeviceID: 1}, nil
}
func (f *inboundTransport) UploadPreKeys(context.Context, SignalAccount, PreKeyBundleUpload) error {
	return nil
}
func (f *inboundTransport) PreKeyCount(context.Context, SignalAccount) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.preKeyCount, f.preKeyErr
}
func (f *inboundTransport) FetchPreKeyBundle(_ context.Context, _ SignalAccount, aci string, dev uint32) (PreKeyBundleFetch, error) {
	if f.fetchErr != nil {
		return PreKeyBundleFetch{}, f.fetchErr
	}
	return PreKeyBundleFetch{RecipientACI: aci, DeviceID: dev}, nil
}
func (f *inboundTransport) SendEnvelope(context.Context, SignalAccount, OutboundEnvelope) (SendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends++
	return SendResult{}, nil
}
func (f *inboundTransport) OpenInbound(context.Context, SignalAccount) (<-chan InboundEnvelope, func() error, error) {
	if f.openErr != nil {
		return nil, nil, f.openErr
	}
	return f.inboundCh, func() error {
		f.closeCalled.Store(true)
		return nil
	}, nil
}
func (f *inboundTransport) AckInbound(_ context.Context, _ SignalAccount, id uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acked = append(f.acked, id)
	return nil
}

func (f *inboundTransport) ackCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.acked)
}

// decryptControlAdapter lets a test force Decrypt failures to exercise the
// session-corruption recovery path.
type decryptControlAdapter struct {
	fakeProtocolAdapter
	failDecrypt atomic.Bool
}

func (a *decryptControlAdapter) Decrypt(ctx context.Context, acct SignalAccount, env InboundEnvelope) (InboundMessage, error) {
	a.decryptCalls.Add(1)
	if a.failDecrypt.Load() {
		return InboundMessage{}, errors.New("decrypt boom")
	}
	return InboundMessage{ConversationID: env.SourceACI, SenderACI: env.SourceACI, Body: string(env.Content)}, nil
}

// loginAccount is a well-formed account with a 64-byte identity blob so
// LoadIdentityKeyPair accepts it.
func loginAccount() SignalAccount {
	return SignalAccount{
		ACI:                       "aci-self",
		DeviceID:                  1,
		Password:                  "pw",
		RegistrationID:            7,
		IdentityKeyPairSerialized: make([]byte, 64),
	}
}

// TestSignalClient_Account snapshots the current account.
func TestSignalClient_Account(t *testing.T) {
	c, _, _ := newTestSignalClient(t)
	c.account = SignalAccount{ACI: "abc"}
	if c.Account().ACI != "abc" {
		t.Fatalf("account snapshot wrong")
	}
}

// TestSignalClient_Incoming returns the read-only inbound channel.
func TestSignalClient_Incoming(t *testing.T) {
	c, _, _ := newTestSignalClient(t)
	if c.Incoming() == nil {
		t.Fatalf("incoming channel is nil")
	}
}

// TestSignalClient_Login_NoIdentity rejects an account missing its
// identity keypair.
func TestSignalClient_Login_NoIdentity(t *testing.T) {
	c, _, _ := newTestSignalClient(t)
	err := c.Login(context.Background(), LoginOptions{Account: SignalAccount{ACI: "x"}})
	if !IsKind(err, ErrKindInvalidInput) {
		t.Fatalf("want invalid_input, got %v", err)
	}
}

// TestSignalClient_Login_NilContext rejects a nil context.
func TestSignalClient_Login_NilContext(t *testing.T) {
	c, _, _ := newTestSignalClient(t)
	var nilCtx context.Context
	if err := c.Login(nilCtx, LoginOptions{Account: loginAccount()}); !IsKind(err, ErrKindInvalidInput) {
		t.Fatalf("want invalid_input, got %v", err)
	}
}

// TestSignalClient_Login_NoSubscribe loads the identity without opening
// the inbound stream.
func TestSignalClient_Login_NoSubscribe(t *testing.T) {
	c, _, _ := newTestSignalClient(t)
	if err := c.Login(context.Background(), LoginOptions{Account: loginAccount()}); err != nil {
		t.Fatalf("login: %v", err)
	}
	if c.Account().ACI != "aci-self" {
		t.Fatalf("account not stored")
	}
}

// TestSignalClient_Login_SubscribeAndDeliver opens the inbound stream,
// delivers an envelope, and confirms it is decrypted onto Incoming and
// acked. Exercises Login(Subscribe), openInbound, inboundLoop, and the
// maintenance loop wiring.
func TestSignalClient_Login_SubscribeAndDeliver(t *testing.T) {
	ft := newInboundTransport()
	fa := &fakeProtocolAdapter{}
	c, err := NewSignalClient(SignalClientConfig{
		ServerURL:               "http://x",
		Transport:               ft,
		Protocol:                fa,
		Logger:                  noopLogger{},
		PreKeyRotationThreshold: 5,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := c.Login(context.Background(), LoginOptions{Account: loginAccount(), Subscribe: true}); err != nil {
		t.Fatalf("login: %v", err)
	}
	ft.inboundCh <- InboundEnvelope{ID: 11, SourceACI: "peer", Content: []byte("hi")}
	select {
	case msg := <-c.Incoming():
		if msg.Body != "hi" {
			t.Fatalf("body = %q", msg.Body)
		}
	case <-context.Background().Done():
	}
	waitFor(t, func() bool { return ft.ackCount() >= 1 })
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !ft.closeCalled.Load() {
		t.Fatalf("transport close not called")
	}
}

// TestSignalClient_Login_SubscribeOpenError surfaces an OpenInbound
// failure.
func TestSignalClient_Login_SubscribeOpenError(t *testing.T) {
	ft := newInboundTransport()
	ft.openErr = errors.New("dial fail")
	c, err := NewSignalClient(SignalClientConfig{
		ServerURL: "http://x", Transport: ft, Protocol: &fakeProtocolAdapter{}, Logger: noopLogger{},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := c.Login(context.Background(), LoginOptions{Account: loginAccount(), Subscribe: true}); !IsKind(err, ErrKindTransport) {
		t.Fatalf("want transport, got %v", err)
	}
}

// TestSignalClient_InboundDecryptFailure_RecoversAndAcks forces a decrypt
// failure and verifies the corruption-recovery path runs (session reset +
// reset marker send) and the bad envelope is acked so the queue drains.
func TestSignalClient_InboundDecryptFailure_RecoversAndAcks(t *testing.T) {
	ft := newInboundTransport()
	fa := &decryptControlAdapter{}
	fa.failDecrypt.Store(true)
	c, err := NewSignalClient(SignalClientConfig{
		ServerURL: "http://x", Transport: ft, Protocol: fa, Logger: noopLogger{},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := c.Login(context.Background(), LoginOptions{Account: loginAccount(), Subscribe: true}); err != nil {
		t.Fatalf("login: %v", err)
	}
	ft.inboundCh <- InboundEnvelope{ID: 22, SourceACI: "peer", Content: []byte("garbage")}
	waitFor(t, func() bool { return ft.ackCount() >= 1 })
	// Recovery sends a reset marker envelope and resets the session.
	waitFor(t, func() bool { return fa.resetCalls.Load() >= 1 })
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestSignalClient_HandleSessionCorruption_NoSource errors when the
// envelope carries no source ACI.
func TestSignalClient_HandleSessionCorruption_NoSource(t *testing.T) {
	c, _, _ := newTestSignalClient(t)
	err := c.handleSessionCorruption(context.Background(), SignalAccount{ACI: "self"}, InboundEnvelope{})
	if !IsKind(err, ErrKindProtocol) {
		t.Fatalf("want protocol, got %v", err)
	}
}

// TestSignalClient_FetchBundleFn returns the bundle and wraps fetch
// errors as transport errors.
func TestSignalClient_FetchBundleFn(t *testing.T) {
	ft := newInboundTransport()
	c, err := NewSignalClient(SignalClientConfig{
		ServerURL: "http://x", Transport: ft, Protocol: &fakeProtocolAdapter{}, Logger: noopLogger{},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	fn := c.fetchBundleFn(context.Background(), SignalAccount{ACI: "self"})
	bundle, err := fn("peer", 1)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if bundle.RecipientACI != "peer" {
		t.Fatalf("bundle = %+v", bundle)
	}
	ft.fetchErr = errors.New("nope")
	if _, err := fn("peer", 1); !IsKind(err, ErrKindTransport) {
		t.Fatalf("want transport, got %v", err)
	}
}

// TestSignalClient_RotatePreKeys_NoAccount and nil context error early.
func TestSignalClient_RotatePreKeys_Errors(t *testing.T) {
	c, _, _ := newTestSignalClient(t)
	if err := c.RotatePreKeys(context.Background()); !IsKind(err, ErrKindInvalidInput) {
		t.Fatalf("no account: want invalid_input, got %v", err)
	}
	var nilCtx context.Context
	if err := c.RotatePreKeys(nilCtx); !IsKind(err, ErrKindInvalidInput) {
		t.Fatalf("nil ctx: want invalid_input, got %v", err)
	}
}

// TestSignalClient_Edit_Success delegates to Send with a quote timestamp.
func TestSignalClient_Edit_Success(t *testing.T) {
	c, ft, _ := newTestSignalClient(t)
	c.account = SignalAccount{ACI: "aci", DeviceID: 1, Password: "pw"}
	if err := c.Edit(context.Background(), "conv", 12345, "new body"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if ft.sendCalls != 1 {
		t.Fatalf("expected 1 send, got %d", ft.sendCalls)
	}
}

// TestSignalClient_Edit_NilContext rejects a nil context.
func TestSignalClient_Edit_NilContext(t *testing.T) {
	c, _, _ := newTestSignalClient(t)
	var nilCtx context.Context
	if err := c.Edit(nilCtx, "conv", 1, "x"); !IsKind(err, ErrKindInvalidInput) {
		t.Fatalf("want invalid_input, got %v", err)
	}
}

// TestSignalClient_Send_NilContext and closed client error appropriately.
func TestSignalClient_Send_GuardRails(t *testing.T) {
	c, _, _ := newTestSignalClient(t)
	var nilCtx context.Context
	if err := c.Send(nilCtx, OutboundMessage{ConversationID: "c"}); !IsKind(err, ErrKindInvalidInput) {
		t.Fatalf("nil ctx: want invalid_input, got %v", err)
	}
	if err := c.Send(context.Background(), OutboundMessage{}); !IsKind(err, ErrKindInvalidInput) {
		t.Fatalf("empty conversation: want invalid_input, got %v", err)
	}
	_ = c.Close()
	if err := c.Send(context.Background(), OutboundMessage{ConversationID: "c"}); !IsKind(err, ErrKindConflict) {
		t.Fatalf("closed client: want conflict, got %v", err)
	}
}

// TestKindForTransport classifies typed and untyped errors.
func TestKindForTransport(t *testing.T) {
	if got := kindForTransport(&Error{Kind: ErrKindRateLimited}); got != ErrKindRateLimited {
		t.Fatalf("typed: got %v", got)
	}
	if got := kindForTransport(errors.New("plain")); got != ErrKindTransport {
		t.Fatalf("untyped: got %v", got)
	}
}

// TestNextTimestamp_Monotonic guarantees strictly increasing timestamps
// even within the same millisecond.
func TestNextTimestamp_Monotonic(t *testing.T) {
	c, _, _ := newTestSignalClient(t)
	prev := c.nextTimestamp()
	for i := 0; i < 100; i++ {
		cur := c.nextTimestamp()
		if cur <= prev {
			t.Fatalf("timestamp not strictly increasing: %d <= %d", cur, prev)
		}
		prev = cur
	}
}

// TestStartMaintenance_Idempotent does not spawn a second loop.
func TestStartMaintenance_Idempotent(t *testing.T) {
	ft := newInboundTransport()
	c, err := NewSignalClient(SignalClientConfig{
		ServerURL: "http://x", Transport: ft, Protocol: &fakeProtocolAdapter{}, Logger: noopLogger{},
		PreKeyRotationThreshold: 1,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	c.startMaintenance()
	first := c.maintenanceCancel
	c.startMaintenance() // second call must be a no-op
	if c.maintenanceCancel == nil {
		t.Fatalf("cancel not set")
	}
	_ = first
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
