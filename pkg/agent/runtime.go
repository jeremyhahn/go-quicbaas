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

package agent

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/security"
)

// persistedSessionMagic tags encrypted on-disk session records. The first
// four bytes distinguish an encrypted blob from a plaintext JSON object,
// so LoadPersistedSessions can refuse to decode plaintext as ciphertext
// and vice versa. Value chosen to be non-JSON: first byte is not `{` or
// ASCII whitespace.
var persistedSessionMagic = []byte{0xA5, 'Q', 'B', 'S'}

// Runtime is the daemon-side agent runtime. It wraps pkg/orchestration
// backends and enforces policy + sandbox resolution before each spawn.
// It never re-implements spawn paths — every Spawn call is delegated to
// orchestration.ACPBackend or orchestration.OllamaBackend.
type Runtime struct {
	mu               sync.RWMutex
	backends         map[string]orchestration.AgentBackend
	resolver         *SandboxResolver
	imageMap         map[string]string
	imageRegistry    string
	containerRuntime orchestration.ContainerRuntime
	logger           interfaces.Logger

	// Session persistence + tenant DEK. dekManager may be nil on air-gapped
	// deployments that lack a control plane DEK service; in that case sessions are
	// persisted in plaintext. The workDir is the parent for <workDir>/sessions.
	dekManager security.TenantDEKManager
	workDir    string

	// sessionTenants tracks {sessionID -> tenantID} so Close can decrement the
	// ActiveSessions gauge against the same label series Spawn incremented.
	sessionTenantsMu sync.Mutex
	sessionTenants   map[string]string

	// eventForwarder ships structured session events to the control plane's
	// session event log over gRPC PushEvents. May be nil when the daemon
	// runs in embedded-only mode without a control plane to forward to.
	eventForwarder *EventForwarder

	// sessionBackends tracks {sessionID -> backend name} so Prompt can
	// tag outbound events with the backend that produced them without
	// requiring the caller to re-thread the backend name through every
	// PromptRequest.
	sessionBackendsMu sync.Mutex
	sessionBackends   map[string]string
}

// RuntimeConfig wires external dependencies into the runtime. Backends may
// be nil if a particular kind is disabled. ContainerRuntime is optional
// and only required for sandboxed sessions.
type RuntimeConfig struct {
	Backends         map[string]orchestration.AgentBackend
	ContainerRuntime orchestration.ContainerRuntime
	SandboxPolicy    orchestration.SandboxPolicy
	ImageMap         map[string]string
	ImageRegistry    string
	Logger           interfaces.Logger

	// TenantDEKManager encrypts persisted session state at rest. When nil,
	// PersistSession writes plaintext JSON with a one-time warning. Operators
	// that care about at-rest confidentiality must always wire this in.
	TenantDEKManager security.TenantDEKManager

	// WorkDir is the daemon's working directory; session records are stored
	// under <WorkDir>/sessions/. When empty, PersistSession and
	// LoadPersistedSessions no-op.
	WorkDir string

	// EventForwarder is the optional component that ships session events to
	// the control plane. Runtime.Prompt installs an AgentEventCallback pointing at
	// this forwarder before delegating to the backend so every emitted event
	// reaches attachers on the control plane side.
	EventForwarder *EventForwarder
}

// NewRuntime constructs a Runtime. Returns ErrRuntimeNoBackends if the
// backends map is empty.
func NewRuntime(cfg RuntimeConfig) (*Runtime, error) {
	if len(cfg.Backends) == 0 {
		return nil, ErrRuntimeNoBackends
	}
	if cfg.Logger == nil {
		return nil, ErrRuntimeNoLogger
	}
	hasRT := cfg.ContainerRuntime != nil && cfg.ContainerRuntime.IsAvailable()
	return &Runtime{
		backends:         cloneBackends(cfg.Backends),
		resolver:         NewSandboxResolver(cfg.SandboxPolicy, hasRT),
		imageMap:         cloneStringMap(cfg.ImageMap),
		imageRegistry:    cfg.ImageRegistry,
		containerRuntime: cfg.ContainerRuntime,
		logger:           cfg.Logger,
		dekManager:       cfg.TenantDEKManager,
		workDir:          cfg.WorkDir,
		sessionTenants:   map[string]string{},
		eventForwarder:   cfg.EventForwarder,
		sessionBackends:  map[string]string{},
	}, nil
}

// SetEventForwarder installs (or replaces) the event forwarder. Used by
// the daemon entrypoint to wire the forwarder after the runtime is built,
// since the forwarder's dependencies (token source, TLS config) are easier
// to construct alongside the enroller than inside NewRuntime.
func (r *Runtime) SetEventForwarder(f *EventForwarder) {
	r.mu.Lock()
	r.eventForwarder = f
	r.mu.Unlock()
}

// rememberBackendForSession records which backend owns a session so that
// Prompt calls routed without an explicit backend (e.g. lookup by
// session_id) can still tag outbound events with the correct source.
func (r *Runtime) rememberBackendForSession(sessionID, backendName string) {
	r.sessionBackendsMu.Lock()
	r.sessionBackends[sessionID] = backendName
	r.sessionBackendsMu.Unlock()
}

// backendForSession returns the recorded backend for a session, or empty
// string when unknown. Used during Prompt to tag events.
func (r *Runtime) backendForSession(sessionID string) string {
	r.sessionBackendsMu.Lock()
	defer r.sessionBackendsMu.Unlock()
	return r.sessionBackends[sessionID]
}

// forgetSessionBackend removes the mapping when a session closes.
func (r *Runtime) forgetSessionBackend(sessionID string) {
	r.sessionBackendsMu.Lock()
	delete(r.sessionBackends, sessionID)
	r.sessionBackendsMu.Unlock()
}

// Spawn dispatches to the named backend after resolving the SandboxMode
// against the active policy and the tenant override.
func (r *Runtime) Spawn(ctx context.Context, tenantID string, tenantOverride *orchestration.SandboxPolicy, backendName string, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
	if req == nil {
		return nil, &orchestration.InputValidationError{Field: "request", Message: "cannot be nil"}
	}

	backend, ok := r.lookupBackend(backendName)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrRuntimeUnknownBackend, backendName)
	}

	resolver := r.resolver.WithTenantOverride(tenantOverride)
	effective, resolveErr := resolver.Resolve(req.EffectiveSandboxMode())
	if resolveErr != nil {
		return nil, resolveErr
	}

	req.SandboxMode = effective
	req.Sandboxed = effective == orchestration.SandboxModeRequired

	// Resolve container image from the daemon's image map when the caller
	// didn't set one explicitly. This keeps the wire protocol simple:
	// control plane supplies the backend name; the daemon decides which image
	// to pull.
	if effective == orchestration.SandboxModeRequired && req.ContainerImage == "" {
		if img, ok := r.imageFor(backendName); ok {
			req.ContainerImage = img
		} else {
			return nil, fmt.Errorf("%w: backend %q has no image mapping", ErrRuntimeNoImage, backendName)
		}
	}

	r.logger.Debugf("agent runtime spawn: tenant=%s backend=%s mode=%s image=%s",
		tenantID, backendName, effective.String(), req.ContainerImage)

	sess, spawnErr := backend.Spawn(ctx, req)
	if spawnErr != nil {
		return nil, spawnErr
	}

	// Remember the backend for this session so Prompt can tag outbound
	// events with the correct producer when callers don't re-supply it.
	r.rememberBackendForSession(sess.ID, backendName)

	// Persist session state + increment metrics on success. Persistence
	// errors are logged but not propagated: the backend has already claimed
	// the session and returning an error here would orphan it.
	if persistErr := r.persistSession(ctx, tenantID, backendName, sess); persistErr != nil {
		r.logger.Warnf("agent runtime: persist session %s: %v", sess.ID, persistErr)
	}
	SpawnsTotal.WithLabelValues(tenantID, backendName, sandboxLabel(req.Sandboxed)).Inc()
	ActiveSessions.WithLabelValues(tenantID, backendName).Inc()
	r.sessionTenantsMu.Lock()
	r.sessionTenants[sess.ID] = tenantID
	r.sessionTenantsMu.Unlock()

	return sess, nil
}

// Prompt sends a prompt to the given backend. The session must already
// have been established by a prior Spawn (or by the backend accepting
// stateless prompts — Ollama).
//
// When an EventForwarder is configured, Prompt installs an
// AgentEventCallback that translates each backend event into a structured
// SessionEvent and enqueues it for forwarding to the control plane's session
// log. The callback wraps any caller-supplied EventCallback (rather than
// replacing it) so interactive SDK callers keep seeing events locally.
func (r *Runtime) Prompt(ctx context.Context, backendName string, req *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
	backend, ok := r.lookupBackend(backendName)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrRuntimeUnknownBackend, backendName)
	}

	// Record a PromptSubmitted event up front so attachers see the prompt
	// even when the backend call fails. Tenant is the session's recorded
	// tenant from Spawn; we fall back to "" when the session is unknown.
	if r.eventForwarder != nil && req != nil && req.Prompt != "" {
		r.sessionTenantsMu.Lock()
		tenantID := r.sessionTenants[req.SessionID]
		r.sessionTenantsMu.Unlock()
		backendForEvent := backendName
		if backendForEvent == "" {
			backendForEvent = r.backendForSession(req.SessionID)
		}
		r.eventForwarder.ObserveEvent(&orchestration.SessionEvent{
			SessionID: req.SessionID,
			TenantID:  tenantID,
			Source:    "operator:" + tenantID,
			Kind:      orchestration.KindPromptSubmitted,
			Prompt: &orchestration.PromptSubmittedPayload{
				Prompt:       req.Prompt,
				Model:        req.Model,
				SystemPrompt: req.SystemPrompt,
			},
		})
	}

	// Install a callback that forwards every backend-emitted event to the
	// control plane. Wrap any caller-supplied callback so both paths see events.
	if r.eventForwarder != nil {
		r.sessionTenantsMu.Lock()
		tenantID := r.sessionTenants[req.SessionID]
		r.sessionTenantsMu.Unlock()
		forwardCB := r.eventForwarder.CallbackFor(tenantID, backendName)
		existing := req.EventCallback
		req.EventCallback = func(sessionID, eventType, details string) {
			forwardCB(sessionID, eventType, details)
			if existing != nil {
				existing(sessionID, eventType, details)
			}
		}
	}

	return backend.Prompt(ctx, req)
}

// Infer runs a stateless prompt against the named backend without creating a
// session. Mirrors Prompt but skips all session-lifecycle plumbing. The
// backend must accept sessionless calls (e.g., OllamaBackend). Session-based
// backends (ACP-style) are rejected with InferUnsupportedError so callers
// know to use the Spawn+Prompt path instead.
//
// When an EventForwarder is configured, Infer emits KindInferenceSubmitted
// before the backend call and KindInferenceCompleted (on the final chunk)
// into the event log, enabling operators to observe sessionless calls.
//
// The returned channel streams InferChunk values and is closed when the
// backend finishes. Callers that want an aggregated result should use
// orchestration.AccumulateChunks.
func (r *Runtime) Infer(ctx context.Context, backendName string, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
	backend, ok := r.lookupBackend(backendName)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrRuntimeUnknownBackend, backendName)
	}

	// Session-based backends require Spawn first; surface a typed error so
	// the gRPC handler can map it to codes.Unimplemented.
	if backend.Capabilities().PersistentSessions {
		return nil, &orchestration.InferUnsupportedError{Backend: backendName, Operation: "infer"}
	}

	// Synthesize a per-call session_id so the control plane PushEvents handler
	// (which validates session_id and tenant_id are non-empty) accepts the
	// pair of lifecycle events. Both Submitted and Completed share infID so
	// they group under one queryable key in the session event log.
	infID := "inference-" + uuid.NewString()

	// Empty TenantID would still be rejected by the control plane handler, so
	// suppress emission entirely and log at debug level instead of producing
	// WARN noise on every call. Production callers should always populate
	// TenantID; this branch defends against misconfigured operator paths.
	emitEvents := r.eventForwarder != nil && req != nil && req.Prompt != "" && req.TenantID != ""
	if r.eventForwarder != nil && req != nil && req.TenantID == "" {
		r.logger.Debugf("skipping inference event: tenant unknown (backend=%s)", backendName)
	}

	if emitEvents {
		r.eventForwarder.ObserveEvent(&orchestration.SessionEvent{
			SessionID: infID,
			TenantID:  req.TenantID,
			Source:    "operator:" + req.TenantID,
			Kind:      orchestration.KindInferenceSubmitted,
		})
	}

	// Delegate to the backend's Prompt method (Ollama is sessionless so the
	// empty SessionID is fine). Wrap the single response as a one-element
	// channel so the caller always sees a uniform streaming interface.
	promptReq := &orchestration.PromptRequest{
		Prompt:       req.Prompt,
		Model:        req.Model,
		SystemPrompt: req.SystemPrompt,
	}
	resp, err := backend.Prompt(ctx, promptReq)
	if err != nil {
		return nil, &orchestration.InferBackendError{Op: "Prompt", Err: err}
	}

	if emitEvents {
		r.eventForwarder.ObserveEvent(&orchestration.SessionEvent{
			SessionID: infID,
			TenantID:  req.TenantID,
			Source:    "agent:" + backendName,
			Kind:      orchestration.KindInferenceCompleted,
		})
	}

	r.logger.Debugf("agent runtime infer: tenant=%s backend=%s model=%s stop=%s",
		req.TenantID, backendName, resp.Model, resp.StopReason)

	ch := make(chan *orchestration.InferChunk, 1)
	ch <- &orchestration.InferChunk{
		Delta:      resp.Content,
		Model:      resp.Model,
		StopReason: resp.StopReason,
		Final:      true,
	}
	close(ch)
	return ch, nil
}

// Status returns session health for a persistent backend.
func (r *Runtime) Status(ctx context.Context, backendName, sessionID string) (*orchestration.SessionStatus, error) {
	backend, ok := r.lookupBackend(backendName)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrRuntimeUnknownBackend, backendName)
	}
	return backend.Status(ctx, sessionID)
}

// Close terminates a session owned by the given backend.
func (r *Runtime) Close(ctx context.Context, backendName, sessionID string) error {
	backend, ok := r.lookupBackend(backendName)
	if !ok {
		return fmt.Errorf("%w: %q", ErrRuntimeUnknownBackend, backendName)
	}
	r.forgetSessionBackend(sessionID)
	if err := backend.Close(ctx, sessionID); err != nil {
		return err
	}
	r.removePersistedSession(sessionID)

	// Use the recorded tenant so the gauge decrement hits the same label
	// series Spawn incremented. Missing entries are quietly ignored — an
	// operator restart may have lost the in-memory map.
	r.sessionTenantsMu.Lock()
	tenantID, ok := r.sessionTenants[sessionID]
	if ok {
		delete(r.sessionTenants, sessionID)
	}
	r.sessionTenantsMu.Unlock()
	if ok {
		ActiveSessions.WithLabelValues(tenantID, backendName).Dec()
	}
	return nil
}

// SandboxResolver returns the runtime's resolver for inspection. Daemons
// surface this via the status RPC.
func (r *Runtime) SandboxResolver() *SandboxResolver {
	return r.resolver
}

// ImageFor returns the fully qualified image reference for a backend name.
// Applies the registry prefix when the map entry is unqualified.
func (r *Runtime) ImageFor(backendName string) (string, bool) {
	return r.imageFor(backendName)
}

// Backends returns the set of registered backend names.
func (r *Runtime) Backends() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.backends))
	for k := range r.backends {
		names = append(names, k)
	}
	return names
}

// ListModels returns the model names available on the named backend. Backends
// that implement orchestration.ModelLister are queried directly; others return
// an empty slice. Returns ErrRuntimeUnknownBackend when the backend is unknown.
func (r *Runtime) ListModels(ctx context.Context, backendName string) ([]string, error) {
	r.mu.RLock()
	backend, ok := r.backends[backendName]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrRuntimeUnknownBackend, backendName)
	}
	lister, ok := backend.(orchestration.ModelLister)
	if !ok {
		return []string{}, nil
	}
	return lister.ListModels(ctx)
}

// PrepareImages optionally pre-pulls every mapped container image so the
// first Spawn doesn't pay registry-fetch latency. When autoPull is false,
// this is a no-op.
//
// Individual Pull failures are logged and counted but do NOT cause
// PrepareImages to return an error: an air-gapped host may already have
// every image baked in, and the daemon should remain serviceable.
func (r *Runtime) PrepareImages(ctx context.Context, autoPull bool) error {
	if !autoPull {
		return nil
	}
	if r.containerRuntime == nil {
		r.logger.Infof("agent runtime: auto-pull requested but no container runtime is configured; skipping")
		return nil
	}
	r.mu.RLock()
	images := make([]string, 0, len(r.imageMap))
	for backendName := range r.imageMap {
		img, ok := r.imageMap[backendName]
		if !ok {
			continue
		}
		images = append(images, qualifyImage(img, r.imageRegistry))
	}
	r.mu.RUnlock()
	if len(images) == 0 {
		return nil
	}
	r.logger.Infof("agent runtime: pre-pulling %d container images", len(images))
	for _, img := range images {
		if err := r.containerRuntime.Pull(ctx, img); err != nil {
			r.logger.Warnf("agent runtime: image pull %q failed: %v", img, err)
			ImagePullsTotal.WithLabelValues("error").Inc()
			continue
		}
		r.logger.Infof("agent runtime: image pulled: %s", img)
		ImagePullsTotal.WithLabelValues("ok").Inc()
	}
	return nil
}

// LoadPersistedSessions reads every session record under <workDir>/sessions
// and returns the decrypted entries. Records that fail to decrypt (bad DEK,
// corrupted ciphertext) are moved to "<name>.corrupt" and surfaced via
// ErrPersistedSessionCorrupt so an operator can investigate; the scan
// continues with the remaining records.
func (r *Runtime) LoadPersistedSessions(ctx context.Context) ([]PersistedSession, error) {
	if r.workDir == "" {
		return nil, nil
	}
	dir := filepath.Join(r.workDir, "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, &PersistSessionIOError{Op: "read-dir", Path: dir, Err: err}
	}
	var out []PersistedSession
	var corruptCount int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json.enc") && !strings.HasSuffix(name, ".json") {
			continue
		}
		path := filepath.Join(dir, name)
		sess, loadErr := r.loadSessionFile(ctx, path)
		if loadErr != nil {
			corruptCount++
			r.logger.Warnf("agent runtime: load session %q: %v; moving to .corrupt", path, loadErr)
			if mvErr := os.Rename(path, path+".corrupt"); mvErr != nil {
				r.logger.Errorf("agent runtime: rename corrupt session %q: %v", path, mvErr)
			}
			continue
		}
		out = append(out, sess)
	}
	if corruptCount > 0 {
		return out, &PersistedSessionCorruptError{Count: corruptCount}
	}
	return out, nil
}

// PersistedSession is a minimal, on-disk-safe record of a live session.
// It stores only the fields needed to reattach to an existing backend
// process after daemon restart; runtime state lives elsewhere.
type PersistedSession struct {
	SessionID     string    `json:"session_id"`
	TenantID      string    `json:"tenant_id"`
	Backend       string    `json:"backend"`
	WorkspacePath string    `json:"workspace_path"`
	CreatedAt     time.Time `json:"created_at"`
}

func (r *Runtime) lookupBackend(name string) (orchestration.AgentBackend, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.backends[name]
	return b, ok
}

func (r *Runtime) imageFor(backendName string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	img, ok := r.imageMap[backendName]
	if !ok {
		return "", false
	}
	return qualifyImage(img, r.imageRegistry), true
}

// persistSession serializes a PersistedSession and writes it to disk under
// <workDir>/sessions/<session_id>.json[.enc]. Encryption via the tenant DEK
// is applied when the DEK manager is non-nil and WorkDir is set.
func (r *Runtime) persistSession(ctx context.Context, tenantID, backendName string, sess *orchestration.Session) error {
	if r.workDir == "" || sess == nil || sess.ID == "" {
		return nil
	}
	record := PersistedSession{
		SessionID:     sess.ID,
		TenantID:      tenantID,
		Backend:       backendName,
		WorkspacePath: sess.WorkDir,
		CreatedAt:     time.Unix(0, sess.CreatedAt),
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return &PersistSessionEncodeError{SessionID: sess.ID, Err: err}
	}
	dir := filepath.Join(r.workDir, "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return &PersistSessionIOError{Op: "mkdir", Path: dir, Err: err}
	}
	if r.dekManager == nil {
		// Plaintext path — operators see the warning in logs once per session.
		r.logger.Warnf("agent runtime: no tenant DEK configured; writing session %s in plaintext", sess.ID)
		path := filepath.Join(dir, sess.ID+".json")
		return writeAtomic(path, payload)
	}
	enc, encErr := r.encryptWithTenantDEK(ctx, tenantID, payload)
	if encErr != nil {
		return encErr
	}
	path := filepath.Join(dir, sess.ID+".json.enc")
	return writeAtomic(path, enc)
}

// removePersistedSession deletes both plaintext and encrypted variants of
// a persisted record, if present. Missing files are ignored.
func (r *Runtime) removePersistedSession(sessionID string) {
	if r.workDir == "" || sessionID == "" {
		return
	}
	dir := filepath.Join(r.workDir, "sessions")
	for _, suffix := range []string{".json.enc", ".json"} {
		path := filepath.Join(dir, sessionID+suffix)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			r.logger.Warnf("agent runtime: remove persisted session %q: %v", path, err)
		}
	}
}

// loadSessionFile decodes either an encrypted or plaintext session record.
// The file's magic prefix selects the path — not the file extension — so
// rename attacks cannot coerce plaintext parsing.
func (r *Runtime) loadSessionFile(ctx context.Context, path string) (PersistedSession, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return PersistedSession{}, &PersistSessionIOError{Op: "read", Path: path, Err: err}
	}
	if len(raw) == 0 {
		return PersistedSession{}, &PersistSessionDecodeError{Path: path, Err: errors.New("empty file")}
	}
	if len(raw) >= len(persistedSessionMagic) && string(raw[:len(persistedSessionMagic)]) == string(persistedSessionMagic) {
		if r.dekManager == nil {
			return PersistedSession{}, &PersistSessionDecodeError{Path: path, Err: ErrPersistNoDEK}
		}
		// Peek at the tenant inside the header so we fetch the right DEK.
		plaintext, decErr := r.decryptWithTenantDEK(ctx, raw)
		if decErr != nil {
			return PersistedSession{}, decErr
		}
		var rec PersistedSession
		if uErr := json.Unmarshal(plaintext, &rec); uErr != nil {
			return PersistedSession{}, &PersistSessionDecodeError{Path: path, Err: uErr}
		}
		return rec, nil
	}
	// Plaintext path.
	var rec PersistedSession
	if uErr := json.Unmarshal(raw, &rec); uErr != nil {
		return PersistedSession{}, &PersistSessionDecodeError{Path: path, Err: uErr}
	}
	return rec, nil
}

// encryptWithTenantDEK produces an at-rest blob for the supplied plaintext.
// Format: [magic:4][tenant_len:2 LE][tenant][nonce:12][ciphertext+tag]
// AES-256-GCM is used with a key derived directly from the tenant DEK. The
// tenant ID is stored in the header so decryption can re-fetch the DEK on
// daemon restart without consulting external state.
func (r *Runtime) encryptWithTenantDEK(ctx context.Context, tenantID string, plaintext []byte) ([]byte, error) {
	dek, err := r.dekManager.GetOrCreateTenantDEK(ctx, tenantID)
	if err != nil {
		return nil, &PersistSessionCryptoError{Op: "get-dek", Err: err}
	}
	block, blockErr := aes.NewCipher(dek)
	if blockErr != nil {
		return nil, &PersistSessionCryptoError{Op: "new-cipher", Err: blockErr}
	}
	gcm, gcmErr := cipher.NewGCM(block)
	if gcmErr != nil {
		return nil, &PersistSessionCryptoError{Op: "new-gcm", Err: gcmErr}
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, rErr := io.ReadFull(rand.Reader, nonce); rErr != nil {
		return nil, &PersistSessionCryptoError{Op: "nonce", Err: rErr}
	}
	// Bind the tenant ID to the ciphertext via GCM additional data so swapping
	// headers between tenants fails authentication.
	tenantBytes := []byte(tenantID)
	ciphertext := gcm.Seal(nil, nonce, plaintext, tenantBytes)

	out := make([]byte, 0, len(persistedSessionMagic)+2+len(tenantBytes)+len(nonce)+len(ciphertext))
	out = append(out, persistedSessionMagic...)
	out = append(out, byte(len(tenantBytes)&0xFF), byte((len(tenantBytes)>>8)&0xFF))
	out = append(out, tenantBytes...)
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return out, nil
}

// decryptWithTenantDEK is the inverse of encryptWithTenantDEK.
func (r *Runtime) decryptWithTenantDEK(ctx context.Context, blob []byte) ([]byte, error) {
	const headerLen = 4 + 2
	if len(blob) < headerLen {
		return nil, &PersistSessionCryptoError{Op: "header", Err: ErrPersistInvalidFormat}
	}
	tenantLen := int(blob[4]) | int(blob[5])<<8
	if len(blob) < headerLen+tenantLen+12 {
		return nil, &PersistSessionCryptoError{Op: "header-len", Err: ErrPersistInvalidFormat}
	}
	tenantID := string(blob[headerLen : headerLen+tenantLen])
	nonce := blob[headerLen+tenantLen : headerLen+tenantLen+12]
	ciphertext := blob[headerLen+tenantLen+12:]

	dek, err := r.dekManager.GetTenantDEK(ctx, tenantID)
	if err != nil {
		return nil, &PersistSessionCryptoError{Op: "get-dek", Err: err}
	}
	block, blockErr := aes.NewCipher(dek)
	if blockErr != nil {
		return nil, &PersistSessionCryptoError{Op: "new-cipher", Err: blockErr}
	}
	gcm, gcmErr := cipher.NewGCM(block)
	if gcmErr != nil {
		return nil, &PersistSessionCryptoError{Op: "new-gcm", Err: gcmErr}
	}
	plaintext, openErr := gcm.Open(nil, nonce, ciphertext, []byte(tenantID))
	if openErr != nil {
		return nil, &PersistSessionCryptoError{Op: "gcm-open", Err: openErr}
	}
	return plaintext, nil
}

// writeAtomic writes data to path via a rename from a sibling temp file so
// readers never observe a half-written record.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	if err != nil {
		return &PersistSessionIOError{Op: "temp-create", Path: path, Err: err}
	}
	tmpPath := tmp.Name()
	// Best-effort cleanup on any pre-rename error path.
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return &PersistSessionIOError{Op: "temp-write", Path: path, Err: err}
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return &PersistSessionIOError{Op: "chmod", Path: path, Err: err}
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return &PersistSessionIOError{Op: "temp-close", Path: path, Err: err}
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return &PersistSessionIOError{Op: "rename", Path: path, Err: err}
	}
	return nil
}

// qualifyImage prefixes an image reference with registry when the image
// is not already fully qualified. "ghcr.io/x/y:tag" stays untouched;
// "claude-code:v1" becomes "<registry>/claude-code:v1".
func qualifyImage(image, registry string) string {
	if registry == "" {
		return image
	}
	if strings.Contains(image, "/") {
		// Already has a slash — assume it's registry-qualified or
		// repo-qualified. Don't double-prefix.
		return image
	}
	return strings.TrimRight(registry, "/") + "/" + image
}

func cloneBackends(in map[string]orchestration.AgentBackend) map[string]orchestration.AgentBackend {
	out := make(map[string]orchestration.AgentBackend, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// Runtime error sentinels.
var (
	ErrRuntimeNoBackends     = errors.New("agent: runtime requires at least one backend")
	ErrRuntimeNoLogger       = errors.New("agent: runtime requires a logger")
	ErrRuntimeUnknownBackend = errors.New("agent: unknown backend")
	ErrRuntimeNoImage        = errors.New("agent: no container image mapped for backend")

	// ErrPersistNoDEK indicates an encrypted session record was found but no
	// TenantDEKManager is configured to decrypt it.
	ErrPersistNoDEK = errors.New("agent: encrypted session record requires a tenant DEK manager")

	// ErrPersistInvalidFormat indicates the encrypted record header is
	// malformed or truncated.
	ErrPersistInvalidFormat = errors.New("agent: persisted session format invalid")
)

// PersistSessionIOError wraps a filesystem failure during persistence.
type PersistSessionIOError struct {
	Op   string
	Path string
	Err  error
}

// Error implements the error interface.
func (e *PersistSessionIOError) Error() string {
	return "agent: persist session " + e.Op + " " + e.Path + ": " + e.Err.Error()
}

// Unwrap supports errors.Is/As traversal.
func (e *PersistSessionIOError) Unwrap() error { return e.Err }

// PersistSessionEncodeError wraps a JSON marshal failure.
type PersistSessionEncodeError struct {
	SessionID string
	Err       error
}

// Error implements the error interface.
func (e *PersistSessionEncodeError) Error() string {
	return "agent: persist session encode " + e.SessionID + ": " + e.Err.Error()
}

// Unwrap supports errors.Is/As traversal.
func (e *PersistSessionEncodeError) Unwrap() error { return e.Err }

// PersistSessionDecodeError wraps a JSON unmarshal or plaintext-parse failure.
type PersistSessionDecodeError struct {
	Path string
	Err  error
}

// Error implements the error interface.
func (e *PersistSessionDecodeError) Error() string {
	return "agent: persist session decode " + e.Path + ": " + e.Err.Error()
}

// Unwrap supports errors.Is/As traversal.
func (e *PersistSessionDecodeError) Unwrap() error { return e.Err }

// PersistSessionCryptoError wraps an AES-GCM or DEK-fetch failure.
type PersistSessionCryptoError struct {
	Op  string
	Err error
}

// Error implements the error interface.
func (e *PersistSessionCryptoError) Error() string {
	return "agent: persist session crypto " + e.Op + ": " + e.Err.Error()
}

// Unwrap supports errors.Is/As traversal.
func (e *PersistSessionCryptoError) Unwrap() error { return e.Err }

// PersistedSessionCorruptError signals that one or more records on disk
// could not be decoded and have been renamed to ".corrupt". Returned
// alongside the successfully-decoded records so startup can continue.
type PersistedSessionCorruptError struct {
	Count int
}

// Error implements the error interface.
func (e *PersistedSessionCorruptError) Error() string {
	return fmt.Sprintf("agent: %d persisted session(s) unreadable; moved to .corrupt", e.Count)
}
