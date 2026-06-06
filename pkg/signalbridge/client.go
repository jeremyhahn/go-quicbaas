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
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// SignalClientConfig configures a SignalClient.
type SignalClientConfig struct {
	// ServerURL is the base URL of the user's go-signal server.
	ServerURL string
	// Account carries the persisted account record. Pass zero-value on
	// first registration.
	Account SignalAccount
	// Transport is the wire-protocol implementation. Required. Tests
	// inject a FakeServerTransport.
	Transport ServerTransport
	// Protocol is the Signal Protocol adapter (wraps go-libsignal
	// primitives). Required.
	Protocol ProtocolAdapter
	// Logger is used for structured logging. Required.
	Logger interfaces.Logger
	// IncomingBuffer is the size of the decoded inbound channel. A value
	// of 0 defaults to 128.
	IncomingBuffer int
	// RateLimit is the maximum outbound sends per second per
	// conversation. 0 disables rate limiting.
	RateLimit float64
	// PreKeyRotationThreshold triggers a pre-key refresh when the server
	// reports fewer than this many one-time pre-keys remaining. 0
	// disables automatic rotation.
	PreKeyRotationThreshold int
	// PreKeyCheckInterval is how often the maintenance loop polls the
	// server for pre-key inventory. 0 defaults to 10 minutes.
	PreKeyCheckInterval time.Duration
	// BackoffBase is the initial backoff interval on 429/transport
	// errors. 0 defaults to 500ms. Each retry doubles up to BackoffMax.
	BackoffBase time.Duration
	// BackoffMax caps the exponential backoff. 0 defaults to 30s.
	BackoffMax time.Duration
}

// SignalClient wraps go-libsignal primitives and a ServerTransport to
// present a simple Signal-client API (register, login, send, receive)
// to higher layers. It is safe for concurrent use.
type SignalClient struct {
	cfg       SignalClientConfig
	transport ServerTransport
	protocol  ProtocolAdapter
	logger    interfaces.Logger

	mu       sync.RWMutex
	account  SignalAccount
	incoming chan InboundMessage
	closeFn  func() error

	// rollingTS supplies monotonically increasing millisecond timestamps
	// for outbound messages even when many are sent in the same
	// millisecond (required because Signal uses timestamps as message
	// IDs for Edit operations).
	rollingTS atomic.Int64

	// rateLimiters is a per-conversation token bucket keyed by
	// conversation ID.
	rlMu         sync.Mutex
	rateLimiters map[string]*tokenBucket

	// maintenance goroutine lifecycle.
	maintenanceCancel context.CancelFunc
	maintenanceWG     sync.WaitGroup

	// inbound loop lifecycle.
	inboundCancel context.CancelFunc
	inboundWG     sync.WaitGroup

	// closed guards against double-close and send-after-close.
	closed atomic.Bool
}

// NewSignalClient constructs a SignalClient from a validated config.
func NewSignalClient(cfg SignalClientConfig) (*SignalClient, error) {
	if cfg.Transport == nil {
		return nil, newError("signalbridge.NewSignalClient", ErrKindInvalidInput, "Transport is required")
	}
	if cfg.Protocol == nil {
		return nil, newError("signalbridge.NewSignalClient", ErrKindInvalidInput, "Protocol is required")
	}
	if cfg.Logger == nil {
		return nil, newError("signalbridge.NewSignalClient", ErrKindInvalidInput, "Logger is required")
	}
	buf := cfg.IncomingBuffer
	if buf <= 0 {
		buf = 128
	}
	c := &SignalClient{
		cfg:          cfg,
		transport:    cfg.Transport,
		protocol:     cfg.Protocol,
		logger:       cfg.Logger,
		account:      cfg.Account,
		incoming:     make(chan InboundMessage, buf),
		rateLimiters: make(map[string]*tokenBucket),
	}
	c.rollingTS.Store(time.Now().UnixMilli())
	return c, nil
}

// Account returns a snapshot of the client's current account record.
func (c *SignalClient) Account() SignalAccount {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.account
}

// Register creates a new account on the go-signal server. The call is
// three-phased: (1) create session, (2) request+submit verification
// code, (3) complete registration. When opts.VerificationCode is empty
// on entry the function performs phases 1+2 and returns an *Error with
// Kind==ErrKindVerificationRequired carrying the SessionID in Msg; the
// caller is expected to collect the code and re-invoke Register with it.
func (c *SignalClient) Register(ctx context.Context, opts RegisterOptions) (*SignalAccount, error) {
	const op = "signalbridge.SignalClient.Register"
	if ctx == nil {
		return nil, newError(op, ErrKindInvalidInput, "context is required")
	}
	if opts.Number == "" {
		return nil, newError(op, ErrKindInvalidInput, "Number is required")
	}

	if opts.VerificationCode == "" {
		sessionID := opts.SessionID
		if sessionID == "" {
			sid, err := c.transport.CreateVerificationSession(ctx, opts.Number)
			if err != nil {
				return nil, wrapError(op, ErrKindTransport, err, "create verification session")
			}
			sessionID = sid
		}
		if err := c.transport.RequestVerificationCode(ctx, sessionID, "sms", opts.Captcha); err != nil {
			return nil, wrapError(op, ErrKindTransport, err, "request verification code")
		}
		return nil, &Error{Op: op, Kind: ErrKindVerificationRequired, Msg: sessionID}
	}

	if opts.SessionID == "" {
		return nil, newError(op, ErrKindInvalidInput, "SessionID is required when resuming with code")
	}
	if err := c.transport.SubmitVerificationCode(ctx, opts.SessionID, opts.VerificationCode); err != nil {
		return nil, wrapError(op, ErrKindTransport, err, "submit verification code")
	}

	password := opts.Password
	if password == "" {
		password = generatePassword()
	}
	regID := generateRegistrationID()
	pniID := generateRegistrationID()

	idkp, err := c.protocol.GenerateIdentityKeyPair()
	if err != nil {
		return nil, wrapError(op, ErrKindProtocol, err, "generate identity keypair")
	}

	req := RegistrationRequest{
		SessionID:      opts.SessionID,
		Number:         opts.Number,
		Password:       password,
		RegistrationID: regID,
		PNIRegID:       pniID,
		DeviceName:     opts.DeviceName,
		IdentityPublic: idkp.PublicSerialized,
	}
	resp, err := c.transport.CompleteRegistration(ctx, req)
	if err != nil {
		return nil, wrapError(op, ErrKindTransport, err, "complete registration")
	}

	account := SignalAccount{
		ACI:                       resp.ACI,
		PNI:                       resp.PNI,
		Number:                    opts.Number,
		DeviceID:                  resp.DeviceID,
		DeviceName:                opts.DeviceName,
		RegistrationID:            regID,
		IdentityKeyPairSerialized: idkp.Serialized,
		Password:                  password,
	}
	c.mu.Lock()
	c.account = account
	c.mu.Unlock()

	// Publish initial pre-keys so remote clients can initiate sessions.
	if err := c.RotatePreKeys(ctx); err != nil {
		return nil, err
	}
	return &account, nil
}

// Login re-establishes the client for an already-registered account.
// When opts.Subscribe is true, Login opens the inbound stream.
func (c *SignalClient) Login(ctx context.Context, opts LoginOptions) error {
	const op = "signalbridge.SignalClient.Login"
	if ctx == nil {
		return newError(op, ErrKindInvalidInput, "context is required")
	}
	if opts.Account.ACI == "" {
		return newError(op, ErrKindInvalidInput, "account ACI is required")
	}
	if len(opts.Account.IdentityKeyPairSerialized) == 0 {
		return newError(op, ErrKindInvalidInput, "account identity keypair is required")
	}
	if err := c.protocol.LoadIdentityKeyPair(opts.Account.IdentityKeyPairSerialized, opts.Account.RegistrationID); err != nil {
		return wrapError(op, ErrKindProtocol, err, "load identity keypair")
	}

	c.mu.Lock()
	c.account = opts.Account
	c.mu.Unlock()

	if opts.Subscribe {
		if err := c.openInbound(ctx); err != nil {
			return err
		}
	}
	if c.cfg.PreKeyRotationThreshold > 0 {
		c.startMaintenance()
	}
	return nil
}

// openInbound starts the realtime WebSocket consumer goroutine.
func (c *SignalClient) openInbound(ctx context.Context) error {
	const op = "signalbridge.SignalClient.openInbound"
	c.mu.Lock()
	account := c.account
	c.mu.Unlock()

	env, closer, err := c.transport.OpenInbound(ctx, account)
	if err != nil {
		return wrapError(op, ErrKindTransport, err, "open inbound channel")
	}
	c.mu.Lock()
	c.closeFn = closer
	c.mu.Unlock()

	loopCtx, cancel := context.WithCancel(ctx)
	c.inboundCancel = cancel
	c.inboundWG.Add(1)
	go c.inboundLoop(loopCtx, account, env)
	return nil
}

// inboundLoop drains envelopes from the transport channel, decrypts
// them via the ProtocolAdapter, and forwards InboundMessage values to
// the user-facing channel.
func (c *SignalClient) inboundLoop(ctx context.Context, account SignalAccount, env <-chan InboundEnvelope) {
	defer c.inboundWG.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-env:
			if !ok {
				return
			}
			msg, err := c.protocol.Decrypt(ctx, account, e)
			if err != nil {
				c.logger.WithError(err).Warn("signalbridge: decrypt failed",
					"source_aci", e.SourceACI,
					"source_device", e.SourceDeviceID,
					"envelope_id", e.ID,
				)
				// Session corruption recovery: attempt a session reset.
				// Swallow the error so the pipeline survives; downstream
				// sender will treat the reset marker as a fresh ratchet.
				if resetErr := c.handleSessionCorruption(ctx, account, e); resetErr != nil {
					c.logger.WithError(resetErr).Warn("signalbridge: session reset failed",
						"source_aci", e.SourceACI,
					)
				}
				// Ack so the server drops the undecryptable envelope.
				_ = c.transport.AckInbound(ctx, account, e.ID)
				continue
			}
			if err := c.transport.AckInbound(ctx, account, e.ID); err != nil {
				c.logger.WithError(err).Warn("signalbridge: ack inbound failed",
					"envelope_id", e.ID,
				)
			}
			select {
			case c.incoming <- msg:
			case <-ctx.Done():
				return
			}
		}
	}
}

// handleSessionCorruption responds to a decrypt failure by sending a
// Signal Protocol reset marker to the sender and clearing our side of
// the session so the next inbound PreKeySignalMessage initializes a
// fresh double ratchet.
func (c *SignalClient) handleSessionCorruption(ctx context.Context, account SignalAccount, e InboundEnvelope) error {
	const op = "signalbridge.SignalClient.handleSessionCorruption"
	if e.SourceACI == "" {
		return newError(op, ErrKindProtocol, "envelope has no source ACI")
	}
	if err := c.protocol.ResetSession(account, e.SourceACI, e.SourceDeviceID); err != nil {
		return wrapError(op, ErrKindProtocol, err, "reset session state")
	}
	// Emit a zero-body outbound message with the reset flag so the peer
	// knows to reinitialize. This uses the protocol adapter's plaintext
	// path because encryption is impossible until a fresh session
	// exists.
	ts := c.nextTimestamp()
	marker := OutboundMessage{
		ConversationID:   e.SourceACI,
		Kind:             ConversationDM,
		Body:             "",
		Timestamp:        ts,
		SessionResetFlag: true,
	}
	return c.sendDirect(ctx, account, marker)
}

// RotatePreKeys replaces the account's pre-key bundle and uploads it.
func (c *SignalClient) RotatePreKeys(ctx context.Context) error {
	const op = "signalbridge.SignalClient.RotatePreKeys"
	if ctx == nil {
		return newError(op, ErrKindInvalidInput, "context is required")
	}
	c.mu.RLock()
	account := c.account
	c.mu.RUnlock()
	if account.ACI == "" {
		return newError(op, ErrKindInvalidInput, "no account; Register or Login first")
	}
	bundle, err := c.protocol.GeneratePreKeyBundle(account, 100, 25)
	if err != nil {
		return wrapError(op, ErrKindProtocol, err, "generate pre-key bundle")
	}
	if err := c.transport.UploadPreKeys(ctx, account, bundle); err != nil {
		return wrapError(op, ErrKindTransport, err, "upload pre-keys")
	}
	return nil
}

// Send encrypts and delivers a message to one conversation.
func (c *SignalClient) Send(ctx context.Context, msg OutboundMessage) error {
	const op = "signalbridge.SignalClient.Send"
	if ctx == nil {
		return newError(op, ErrKindInvalidInput, "context is required")
	}
	if c.closed.Load() {
		return newError(op, ErrKindConflict, "client is closed")
	}
	if msg.ConversationID == "" {
		return newError(op, ErrKindInvalidInput, "ConversationID is required")
	}
	if msg.Timestamp == 0 {
		msg.Timestamp = c.nextTimestamp()
	}
	if err := c.rateCheck(msg.ConversationID); err != nil {
		return err
	}
	c.mu.RLock()
	account := c.account
	c.mu.RUnlock()
	if account.ACI == "" {
		return newError(op, ErrKindInvalidInput, "no account; Register or Login first")
	}
	return c.sendDirect(ctx, account, msg)
}

// sendDirect performs the protocol-level encrypt + transport send with
// exponential backoff on transient 429/transport errors.
func (c *SignalClient) sendDirect(ctx context.Context, account SignalAccount, msg OutboundMessage) error {
	const op = "signalbridge.SignalClient.sendDirect"
	envelopes, err := c.protocol.Encrypt(ctx, account, msg, c.fetchBundleFn(ctx, account))
	if err != nil {
		return wrapError(op, ErrKindProtocol, err, "encrypt outbound")
	}
	base := c.cfg.BackoffBase
	if base <= 0 {
		base = 500 * time.Millisecond
	}
	max := c.cfg.BackoffMax
	if max <= 0 {
		max = 30 * time.Second
	}
	for _, env := range envelopes {
		backoff := base
		attempt := 0
		for {
			_, sendErr := c.transport.SendEnvelope(ctx, account, env)
			if sendErr == nil {
				break
			}
			attempt++
			if !isRetryable(sendErr) || attempt > 5 {
				return wrapError(op, kindForTransport(sendErr), sendErr, "send envelope")
			}
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return wrapError(op, ErrKindTransport, ctx.Err(), "send envelope cancelled")
			case <-timer.C:
			}
			backoff *= 2
			if backoff > max {
				backoff = max
			}
		}
	}
	return nil
}

// Edit updates an existing message identified by its original
// timestamp. Used for coalescing assistant chunks.
func (c *SignalClient) Edit(ctx context.Context, conversationID string, originalTimestamp int64, newBody string) error {
	const op = "signalbridge.SignalClient.Edit"
	if ctx == nil {
		return newError(op, ErrKindInvalidInput, "context is required")
	}
	if conversationID == "" {
		return newError(op, ErrKindInvalidInput, "conversationID is required")
	}
	if originalTimestamp == 0 {
		return newError(op, ErrKindInvalidInput, "originalTimestamp is required")
	}
	msg := OutboundMessage{
		ConversationID: conversationID,
		Body:           newBody,
		Timestamp:      c.nextTimestamp(),
		QuoteTimestamp: originalTimestamp,
	}
	if err := c.Send(ctx, msg); err != nil {
		return err
	}
	return nil
}

// Incoming returns the read-only channel of decrypted inbound messages.
func (c *SignalClient) Incoming() <-chan InboundMessage {
	return c.incoming
}

// Close shuts down the inbound loop and transport, then drains the
// maintenance loop. Subsequent Send calls return ErrKindConflict.
func (c *SignalClient) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	if c.inboundCancel != nil {
		c.inboundCancel()
	}
	if c.maintenanceCancel != nil {
		c.maintenanceCancel()
	}
	c.inboundWG.Wait()
	c.maintenanceWG.Wait()

	c.mu.Lock()
	closeFn := c.closeFn
	c.closeFn = nil
	c.mu.Unlock()
	if closeFn != nil {
		if err := closeFn(); err != nil {
			c.logger.WithError(err).Warn("signalbridge: close transport failed")
		}
	}
	close(c.incoming)
	return nil
}

// startMaintenance launches a background loop that polls pre-key
// inventory and rotates when it dips below the configured threshold.
func (c *SignalClient) startMaintenance() {
	if c.maintenanceCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.maintenanceCancel = cancel

	interval := c.cfg.PreKeyCheckInterval
	if interval <= 0 {
		interval = 10 * time.Minute
	}

	c.maintenanceWG.Add(1)
	go func() {
		defer c.maintenanceWG.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.mu.RLock()
				account := c.account
				c.mu.RUnlock()
				if account.ACI == "" {
					continue
				}
				count, err := c.transport.PreKeyCount(ctx, account)
				if err != nil {
					c.logger.WithError(err).Warn("signalbridge: pre-key count failed")
					continue
				}
				if count < c.cfg.PreKeyRotationThreshold {
					if err := c.RotatePreKeys(ctx); err != nil {
						c.logger.WithError(err).Warn("signalbridge: pre-key rotation failed")
					} else {
						c.logger.Info("signalbridge: rotated pre-keys",
							"previous_count", count,
							"threshold", c.cfg.PreKeyRotationThreshold,
						)
					}
				}
			}
		}
	}()
}

// fetchBundleFn returns a closure the ProtocolAdapter invokes when it
// needs a remote recipient's pre-key bundle to bootstrap a new session.
func (c *SignalClient) fetchBundleFn(ctx context.Context, account SignalAccount) FetchBundleFunc {
	return func(recipientACI string, deviceID uint32) (PreKeyBundleFetch, error) {
		bundle, err := c.transport.FetchPreKeyBundle(ctx, account, recipientACI, deviceID)
		if err != nil {
			return PreKeyBundleFetch{}, wrapError("signalbridge.fetchBundle", ErrKindTransport, err, "fetch pre-key bundle")
		}
		return bundle, nil
	}
}

// nextTimestamp returns a strictly-increasing millisecond timestamp
// used as a Signal message ID. Signal requires per-device-monotonic
// timestamps, which rules out using raw time.Now().UnixMilli() when
// many sends happen in the same millisecond.
func (c *SignalClient) nextTimestamp() int64 {
	for {
		cur := c.rollingTS.Load()
		now := time.Now().UnixMilli()
		next := cur + 1
		if now > next {
			next = now
		}
		if c.rollingTS.CompareAndSwap(cur, next) {
			return next
		}
	}
}

// rateCheck consults the per-conversation rate limiter. Returns an
// *Error with Kind==ErrKindRateLimited when the bucket is empty.
func (c *SignalClient) rateCheck(conversationID string) error {
	if c.cfg.RateLimit <= 0 {
		return nil
	}
	c.rlMu.Lock()
	tb, ok := c.rateLimiters[conversationID]
	if !ok {
		tb = newTokenBucket(c.cfg.RateLimit, c.cfg.RateLimit)
		c.rateLimiters[conversationID] = tb
	}
	c.rlMu.Unlock()
	if !tb.take() {
		return newError("signalbridge.SignalClient.rateCheck", ErrKindRateLimited, "per-conversation rate limit exceeded")
	}
	return nil
}

// isRetryable reports whether the error is safe to retry.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	var be *Error
	if errors.As(err, &be) {
		return be.Kind == ErrKindRateLimited || be.Kind == ErrKindTransport
	}
	// Unknown errors from the transport are treated as transient.
	return true
}

// kindForTransport inspects an error and classifies it. Used when
// wrapping transport errors into SignalBridge errors.
func kindForTransport(err error) ErrKind {
	var be *Error
	if errors.As(err, &be) {
		return be.Kind
	}
	return ErrKindTransport
}

// generatePassword returns a random 18-byte base64-encoded token
// suitable for HTTP Basic auth against go-signal.
func generatePassword() string {
	buf := make([]byte, 18)
	_, _ = rand.Read(buf)
	return base64.StdEncoding.EncodeToString(buf)
}

// generateRegistrationID returns a random value in [1, 16380]
// matching the Signal Protocol registration ID range.
func generateRegistrationID() uint32 {
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	v := binary.BigEndian.Uint32(buf[:])
	return (v % 16380) + 1
}
