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
	"time"
)

// fakeServerTransport is a minimal ServerTransport for exercising
// SignalClient without talking to a real Signal server. Every method
// records inputs and returns deterministic outputs.
type fakeServerTransport struct {
	mu             sync.Mutex
	createSessions int
	requestCalls   int
	submitCalls    int
	completeCalls  int
	uploadCalls    int
	sendCalls      int
	preKeyCount    int
	failSend       atomic.Bool
	// failSendAttempts fails the first N send attempts and then succeeds,
	// without the caller having to intervene mid-flight.
	failSendAttempts atomic.Int32
	sendAttempts     atomic.Int32
}

func (f *fakeServerTransport) CreateVerificationSession(ctx context.Context, number string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createSessions++
	return "session-id", nil
}

func (f *fakeServerTransport) RequestVerificationCode(ctx context.Context, sessionID, transport, captcha string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requestCalls++
	return nil
}

func (f *fakeServerTransport) SubmitVerificationCode(ctx context.Context, sessionID, code string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.submitCalls++
	if code == "bad" {
		return errors.New("invalid code")
	}
	return nil
}

func (f *fakeServerTransport) CompleteRegistration(ctx context.Context, req RegistrationRequest) (RegistrationResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completeCalls++
	return RegistrationResponse{ACI: "aci-bot", PNI: "pni-bot", DeviceID: 1}, nil
}

func (f *fakeServerTransport) UploadPreKeys(ctx context.Context, account SignalAccount, bundle PreKeyBundleUpload) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploadCalls++
	f.preKeyCount = len(bundle.OneTimePreKeys)
	return nil
}

func (f *fakeServerTransport) PreKeyCount(ctx context.Context, account SignalAccount) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.preKeyCount, nil
}

func (f *fakeServerTransport) FetchPreKeyBundle(ctx context.Context, account SignalAccount, recipientACI string, deviceID uint32) (PreKeyBundleFetch, error) {
	return PreKeyBundleFetch{RecipientACI: recipientACI, DeviceID: deviceID}, nil
}

func (f *fakeServerTransport) SendEnvelope(ctx context.Context, account SignalAccount, env OutboundEnvelope) (SendResult, error) {
	attempt := f.sendAttempts.Add(1)
	// failSendAttempts fails a FIXED number of leading attempts and then
	// succeeds on its own. A test that instead flips failSend from the outside
	// after observing an attempt is racing the sender: on a loaded machine the
	// sender can exhaust its retry budget between the observation and the flip,
	// and the send fails for a reason the test never intended to exercise.
	if n := f.failSendAttempts.Load(); n > 0 && attempt <= n {
		return SendResult{}, &Error{Op: "fake", Kind: ErrKindRateLimited, Msg: "forced"}
	}
	if f.failSend.Load() {
		return SendResult{}, &Error{Op: "fake", Kind: ErrKindRateLimited, Msg: "forced"}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendCalls++
	return SendResult{}, nil
}

func (f *fakeServerTransport) OpenInbound(ctx context.Context, account SignalAccount) (<-chan InboundEnvelope, func() error, error) {
	ch := make(chan InboundEnvelope)
	return ch, func() error { close(ch); return nil }, nil
}

func (f *fakeServerTransport) AckInbound(ctx context.Context, account SignalAccount, envelopeID uint64) error {
	return nil
}

// fakeProtocolAdapter implements ProtocolAdapter without real crypto.
// Produces deterministic envelopes so SignalClient plumbing can be
// tested end-to-end.
type fakeProtocolAdapter struct {
	genIDCalls   atomic.Int32
	genPreCalls  atomic.Int32
	encryptCalls atomic.Int32
	decryptCalls atomic.Int32
	resetCalls   atomic.Int32
}

func (f *fakeProtocolAdapter) GenerateIdentityKeyPair() (IdentityKeyPairMaterial, error) {
	f.genIDCalls.Add(1)
	return IdentityKeyPairMaterial{
		Serialized:       make([]byte, 64),
		PublicSerialized: make([]byte, 32),
	}, nil
}
func (f *fakeProtocolAdapter) LoadIdentityKeyPair(serialized []byte, regID uint32) error {
	return nil
}
func (f *fakeProtocolAdapter) GeneratePreKeyBundle(_ SignalAccount, numOneTime int, numKyber int) (PreKeyBundleUpload, error) {
	f.genPreCalls.Add(1)
	keys := make([]PreKeyMaterial, numOneTime)
	return PreKeyBundleUpload{OneTimePreKeys: keys}, nil
}
func (f *fakeProtocolAdapter) Encrypt(ctx context.Context, _ SignalAccount, msg OutboundMessage, _ FetchBundleFunc) ([]OutboundEnvelope, error) {
	f.encryptCalls.Add(1)
	return []OutboundEnvelope{{
		DestinationACI:    msg.ConversationID,
		DestinationDevice: 1,
		Content:           []byte(msg.Body),
		Timestamp:         msg.Timestamp,
	}}, nil
}
func (f *fakeProtocolAdapter) Decrypt(ctx context.Context, _ SignalAccount, env InboundEnvelope) (InboundMessage, error) {
	f.decryptCalls.Add(1)
	return InboundMessage{
		ConversationID: env.SourceACI,
		Body:           string(env.Content),
		SenderACI:      env.SourceACI,
	}, nil
}
func (f *fakeProtocolAdapter) ResetSession(_ SignalAccount, _ string, _ uint32) error {
	f.resetCalls.Add(1)
	return nil
}

// newTestSignalClient constructs a SignalClient wired to fakes.
func newTestSignalClient(t *testing.T) (*SignalClient, *fakeServerTransport, *fakeProtocolAdapter) {
	t.Helper()
	ft := &fakeServerTransport{}
	fa := &fakeProtocolAdapter{}
	c, err := NewSignalClient(SignalClientConfig{
		ServerURL: "http://x",
		Transport: ft,
		Protocol:  fa,
		Logger:    noopLogger{},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return c, ft, fa
}

// TestNewSignalClient_Errors rejects missing required fields.
func TestNewSignalClient_Errors(t *testing.T) {
	if _, err := NewSignalClient(SignalClientConfig{}); err == nil {
		t.Fatalf("expected error")
	}
}

// TestSignalClient_RegisterVerificationRequired returns the special
// typed error on first Register call.
func TestSignalClient_RegisterVerificationRequired(t *testing.T) {
	c, ft, _ := newTestSignalClient(t)
	_, err := c.Register(context.Background(), RegisterOptions{Number: "+15551234567"})
	if err == nil {
		t.Fatalf("expected verification-required error")
	}
	if !IsKind(err, ErrKindVerificationRequired) {
		t.Fatalf("want ErrKindVerificationRequired, got %v", err)
	}
	if ft.createSessions != 1 || ft.requestCalls != 1 {
		t.Fatalf("transport calls wrong: %+v", ft)
	}
}

// TestSignalClient_RegisterComplete completes the flow and uploads
// pre-keys.
func TestSignalClient_RegisterComplete(t *testing.T) {
	c, ft, fa := newTestSignalClient(t)
	account, err := c.Register(context.Background(), RegisterOptions{
		Number:           "+15551234567",
		SessionID:        "sid",
		VerificationCode: "123456",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if account.ACI != "aci-bot" {
		t.Fatalf("unexpected ACI: %q", account.ACI)
	}
	if ft.uploadCalls != 1 {
		t.Fatalf("expected 1 upload, got %d", ft.uploadCalls)
	}
	if fa.genIDCalls.Load() == 0 || fa.genPreCalls.Load() == 0 {
		t.Fatalf("protocol not exercised: %+v", fa)
	}
}

// TestSignalClient_Send_Success sends a single message.
func TestSignalClient_Send_Success(t *testing.T) {
	c, ft, fa := newTestSignalClient(t)
	c.account = SignalAccount{ACI: "aci", DeviceID: 1, Password: "pw"}
	err := c.Send(context.Background(), OutboundMessage{ConversationID: "peer", Body: "hello"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if ft.sendCalls != 1 {
		t.Fatalf("want 1 send got %d", ft.sendCalls)
	}
	if fa.encryptCalls.Load() != 1 {
		t.Fatalf("want 1 encrypt got %d", fa.encryptCalls.Load())
	}
}

// TestSignalClient_Send_NoAccount returns ErrKindInvalidInput.
func TestSignalClient_Send_NoAccount(t *testing.T) {
	c, _, _ := newTestSignalClient(t)
	err := c.Send(context.Background(), OutboundMessage{ConversationID: "peer", Body: "x"})
	if err == nil || !IsKind(err, ErrKindInvalidInput) {
		t.Fatalf("want invalid input got %v", err)
	}
}

// TestSignalClient_Send_RateLimitedRetry verifies that transient
// rate-limited errors are retried with backoff, then succeed.
func TestSignalClient_Send_RateLimitedRetry(t *testing.T) {
	c, ft, _ := newTestSignalClient(t)
	c.cfg.BackoffBase = 1 * time.Millisecond
	c.cfg.BackoffMax = 2 * time.Millisecond
	c.account = SignalAccount{ACI: "aci", DeviceID: 1, Password: "pw"}
	// The transport fails its first attempt and then succeeds BY ITSELF. The
	// test used to start the send, wait for one attempt, and flip the flag off
	// from outside — a race the sender wins whenever it exhausts its retry
	// budget (BackoffBase is 1ms here) before the flip lands, which is what a
	// loaded CI machine reliably produces.
	ft.failSendAttempts.Store(1)
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- c.Send(context.Background(), OutboundMessage{ConversationID: "peer", Body: "hi"})
	}()
	select {
	case err := <-doneCh:
		if err != nil {
			t.Fatalf("send: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("send did not complete")
	}
	if ft.sendAttempts.Load() < 2 {
		t.Fatalf("expected retries, got %d attempts", ft.sendAttempts.Load())
	}
}

// TestSignalClient_RateLimit per-conversation bucket.
func TestSignalClient_RateLimit(t *testing.T) {
	c, _, _ := newTestSignalClient(t)
	c.cfg.RateLimit = 1 // 1 per second
	c.account = SignalAccount{ACI: "aci", DeviceID: 1, Password: "pw"}
	if err := c.Send(context.Background(), OutboundMessage{ConversationID: "a", Body: "1"}); err != nil {
		t.Fatalf("first: %v", err)
	}
	err := c.Send(context.Background(), OutboundMessage{ConversationID: "a", Body: "2"})
	if err == nil || !IsKind(err, ErrKindRateLimited) {
		t.Fatalf("want rate limit got %v", err)
	}
	// Different conversation has its own bucket.
	if err := c.Send(context.Background(), OutboundMessage{ConversationID: "b", Body: "3"}); err != nil {
		t.Fatalf("b: %v", err)
	}
}

// TestSignalClient_Edit_Errors covers input validation.
func TestSignalClient_Edit_Errors(t *testing.T) {
	c, _, _ := newTestSignalClient(t)
	if err := c.Edit(context.Background(), "", 1, "x"); err == nil {
		t.Fatalf("expected error on empty id")
	}
	if err := c.Edit(context.Background(), "c", 0, "x"); err == nil {
		t.Fatalf("expected error on zero timestamp")
	}
}

// TestSignalClient_Close is idempotent.
func TestSignalClient_Close(t *testing.T) {
	c, _, _ := newTestSignalClient(t)
	if err := c.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

// TestSignalClient_Login_Errors rejects missing ACI.
func TestSignalClient_Login_Errors(t *testing.T) {
	c, _, _ := newTestSignalClient(t)
	if err := c.Login(context.Background(), LoginOptions{}); err == nil {
		t.Fatalf("expected error on empty account")
	}
}

// TestTokenBucket_Take verifies the rate-limit helper.
func TestTokenBucket_Take(t *testing.T) {
	b := newTokenBucket(10, 1) // 10/sec, capacity 1
	if !b.take() {
		t.Fatalf("first take should succeed")
	}
	if b.take() {
		t.Fatalf("second immediate take should fail")
	}
}

// TestTokenBucket_Refill verifies refill semantics using nowFn seam.
func TestTokenBucket_Refill(t *testing.T) {
	base := time.Now()
	b := newTokenBucket(10, 1) // 10/sec
	b.nowFn = func() time.Time { return base }
	b.last = base // reset to match nowFn
	if !b.take() {
		t.Fatalf("first take")
	}
	if b.take() {
		t.Fatalf("should deny")
	}
	b.nowFn = func() time.Time { return base.Add(200 * time.Millisecond) } // 2 tokens refilled
	if !b.take() {
		t.Fatalf("refilled take should succeed")
	}
}

// TestIsRetryable categorizes errors.
func TestIsRetryable(t *testing.T) {
	if isRetryable(nil) {
		t.Fatalf("nil should not be retryable")
	}
	if !isRetryable(&Error{Kind: ErrKindRateLimited}) {
		t.Fatalf("rate-limited should retry")
	}
	if !isRetryable(&Error{Kind: ErrKindTransport}) {
		t.Fatalf("transport should retry")
	}
	if isRetryable(&Error{Kind: ErrKindInvalidInput}) {
		t.Fatalf("invalid input should not retry")
	}
}

// TestGeneratePassword produces a base64 string of reasonable length.
func TestGeneratePassword(t *testing.T) {
	p := generatePassword()
	if len(p) < 20 {
		t.Fatalf("too short: %q", p)
	}
	if p == generatePassword() {
		t.Fatalf("should be random")
	}
}

// TestGenerateRegistrationID is in range.
func TestGenerateRegistrationID(t *testing.T) {
	for i := 0; i < 32; i++ {
		id := generateRegistrationID()
		if id < 1 || id > 16380 {
			t.Fatalf("out of range: %d", id)
		}
	}
}
