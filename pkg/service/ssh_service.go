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

package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"time"

	"github.com/google/uuid"
	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// sshSignAuditAction is the audit action of every SignDigest call.
const sshSignAuditAction = "ssh.key.sign"

// =============================================================================
// SSH Service — the single server-side entry point for the SSH surface
// =============================================================================
//
// Every QuicBaaS transport that serves SSH (embedded, gRPC, QUIC, MCP, and
// the REST alias) routes through this service rather than reaching for
// QuicBaaS.SSHClient() and re-implementing the passphrase, signing, and
// list-envelope handling per transport. The service itself owns no SSH
// business logic: it composes the canonical go-xkms SSH SDK client, which is
// the only implementation of key storage, host pinning, and authorized-key
// emission in the stack.
//
// Request and response types are the go-xkms SSH DTOs wherever go-xkms
// already defines one. The three extra types below exist because the xkms
// contract expresses those operations as Go-level constructs (a
// PassphraseProvider callback, an ssh.Signer) that cannot cross a transport.

// SSHService is the transport-neutral SSH surface.
type SSHService interface {
	// GenerateKey creates a new SSH key.
	GenerateKey(ctx context.Context, req xkmsssh.GenerateRequest) (*xkmsssh.Key, error)

	// ImportKey ingests a PEM-encoded private key, decrypting it with the
	// request's base64 passphrase when one is supplied.
	ImportKey(ctx context.Context, req SSHImportKeyRequest) (*xkmsssh.Key, error)

	// GetKey loads metadata for a single key.
	GetKey(ctx context.Context, req xkmsssh.GetKeyRequest) (*xkmsssh.Key, error)

	// ListKeys enumerates keys for the (scope, owner) pair.
	ListKeys(ctx context.Context, req xkmsssh.ListKeysRequest) ([]*xkmsssh.Key, error)

	// DeleteKey removes a key and destroys its enclave.
	DeleteKey(ctx context.Context, req xkmsssh.DeleteKeyRequest) error

	// ExportPublicKey returns the authorized_keys-format public half.
	ExportPublicKey(ctx context.Context, req xkmsssh.ExportRequest) (*xkmsssh.PublicKey, error)

	// SignDigest signs a raw digest with the named key. The private half
	// never leaves the key store.
	SignDigest(ctx context.Context, req SSHSignRequest) (*SSHSignature, error)

	// AddKnownHost pins a host-key fingerprint.
	AddKnownHost(ctx context.Context, req xkmsssh.AddKnownHostRequest) error

	// ListKnownHosts enumerates pinned host keys.
	ListKnownHosts(ctx context.Context, req xkmsssh.ListKnownHostsRequest) ([]*xkmsssh.Pin, error)

	// RemoveKnownHost removes a pinned host key.
	RemoveKnownHost(ctx context.Context, req xkmsssh.RemoveKnownHostRequest) error

	// AddAuthorizedKey persists an authorized_keys entry.
	AddAuthorizedKey(ctx context.Context, req xkmsssh.AddAuthorizedKeyRequest) error

	// ListAuthorizedKeys enumerates authorized_keys entries.
	ListAuthorizedKeys(ctx context.Context, req xkmsssh.ListAuthorizedKeysRequest) ([]*xkmsssh.AuthorizedEntry, error)

	// RemoveAuthorizedKey deletes one authorized_keys entry by name.
	RemoveAuthorizedKey(ctx context.Context, req SSHAuthorizedKeyRef) error

	// EmitAuthorizedKey formats a public key as an authorized_keys line.
	EmitAuthorizedKey(ctx context.Context, req xkmsssh.EmitRequest) (string, error)
}

// SSHImportKeyRequest is xkmsssh.ImportRequest plus the base64-encoded
// passphrase a transport carries in place of the xkms
// secure.PassphraseProvider callback.
type SSHImportKeyRequest struct {
	Scope   string
	Owner   string
	Name    string
	Comment string
	PEM     []byte

	// PassphraseB64 is the base64 encoding of the PEM passphrase. Empty
	// means the PEM is not encrypted.
	PassphraseB64 string
}

// SSHSignRequest identifies the signing key and the digest to sign.
type SSHSignRequest struct {
	Scope  string
	Owner  string
	Name   string
	Digest []byte
}

// SSHSignature mirrors golang.org/x/crypto/ssh.Signature.
type SSHSignature struct {
	Format string
	Blob   []byte
	Rest   []byte
}

// SSHAuthorizedKeyRef identifies one authorized_keys entry.
type SSHAuthorizedKeyRef struct {
	Scope string
	Owner string
	Name  string
}

// =============================================================================
// Typed errors
// =============================================================================

// SSHNotConfiguredError is returned by every verb when the deployment has no
// SSH client wired (neither XKMS.URL nor an SSH store root was configured).
type SSHNotConfiguredError struct{}

// Error implements the error interface.
func (e *SSHNotConfiguredError) Error() string {
	return "service/ssh: ssh is not configured on this server"
}

// SSHMissingDigestError is returned by SignDigest when no digest is supplied.
type SSHMissingDigestError struct{}

// Error implements the error interface.
func (e *SSHMissingDigestError) Error() string {
	return "service/ssh: digest is required"
}

// SSHPassphraseDecodeError wraps a malformed base64 passphrase.
type SSHPassphraseDecodeError struct {
	Err error
}

// Error implements the error interface.
func (e *SSHPassphraseDecodeError) Error() string {
	return "service/ssh: decode passphrase: " + e.Err.Error()
}

// Unwrap exposes the underlying decode failure.
func (e *SSHPassphraseDecodeError) Unwrap() error { return e.Err }

// =============================================================================
// Implementation
// =============================================================================

// sshService composes the go-xkms SSH SDK client.
type sshService struct {
	client xkmsssh.Client
	audit  AuditLoggerSource
}

// NewSSHService returns the SSH service backed by client. A nil client is
// accepted and makes every verb fail with *SSHNotConfiguredError, which is
// how a deployment without an SSH store reports itself to callers.
func NewSSHService(client xkmsssh.Client, opts ...SSHServiceOption) SSHService {
	s := &sshService{client: client}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// AuditLoggerSource yields the audit logger an audited service writes to.
type AuditLoggerSource interface {
	GetAuditLogger(ctx context.Context) (iam.AuditLogger, error)
}

// SSHServiceOption configures NewSSHService.
type SSHServiceOption func(*sshService)

// WithSSHAudit records every SignDigest call, naming the calling principal, in
// the audit log source yields. Without it signatures are not audited.
func WithSSHAudit(source AuditLoggerSource) SSHServiceOption {
	return func(s *sshService) { s.audit = source }
}

// SSHServiceFor is the SSH service over svc's SSH client, audited through svc
// when svc carries an audit log. A nil svc yields an unconfigured service.
func SSHServiceFor(svc QuicBaaS) SSHService {
	if svc == nil {
		return NewSSHService(nil)
	}
	var opts []SSHServiceOption
	if source, ok := svc.(AuditLoggerSource); ok {
		opts = append(opts, WithSSHAudit(source))
	}
	return NewSSHService(svc.SSHClient(), opts...)
}

// compile-time contract.
var _ SSHService = (*sshService)(nil)

// GenerateKey creates a new SSH key.
func (s *sshService) GenerateKey(ctx context.Context, req xkmsssh.GenerateRequest) (*xkmsssh.Key, error) {
	if s.client == nil {
		return nil, &SSHNotConfiguredError{}
	}
	return s.client.Keys().Generate(ctx, req)
}

// ImportKey ingests a PEM-encoded private key.
func (s *sshService) ImportKey(ctx context.Context, req SSHImportKeyRequest) (*xkmsssh.Key, error) {
	if s.client == nil {
		return nil, &SSHNotConfiguredError{}
	}
	provider, err := sshPassphraseProviderFromB64(req.PassphraseB64)
	if err != nil {
		return nil, err
	}
	return s.client.Keys().Import(ctx, xkmsssh.ImportRequest{
		Scope:   req.Scope,
		Owner:   req.Owner,
		Name:    req.Name,
		Comment: req.Comment,
		PEM:     req.PEM,
	}, provider)
}

// GetKey loads metadata for a single key.
func (s *sshService) GetKey(ctx context.Context, req xkmsssh.GetKeyRequest) (*xkmsssh.Key, error) {
	if s.client == nil {
		return nil, &SSHNotConfiguredError{}
	}
	return s.client.Keys().Get(ctx, req)
}

// ListKeys enumerates keys.
func (s *sshService) ListKeys(ctx context.Context, req xkmsssh.ListKeysRequest) ([]*xkmsssh.Key, error) {
	if s.client == nil {
		return nil, &SSHNotConfiguredError{}
	}
	return s.client.Keys().List(ctx, req)
}

// DeleteKey removes a key.
func (s *sshService) DeleteKey(ctx context.Context, req xkmsssh.DeleteKeyRequest) error {
	if s.client == nil {
		return &SSHNotConfiguredError{}
	}
	return s.client.Keys().Delete(ctx, req)
}

// ExportPublicKey returns the public half.
func (s *sshService) ExportPublicKey(ctx context.Context, req xkmsssh.ExportRequest) (*xkmsssh.PublicKey, error) {
	if s.client == nil {
		return nil, &SSHNotConfiguredError{}
	}
	return s.client.Keys().ExportPublic(ctx, req)
}

// SignDigest signs a digest with the named key.
func (s *sshService) SignDigest(ctx context.Context, req SSHSignRequest) (*SSHSignature, error) {
	if s.client == nil {
		return nil, &SSHNotConfiguredError{}
	}
	if len(req.Digest) == 0 {
		return nil, &SSHMissingDigestError{}
	}
	sig, err := s.signDigest(ctx, req)
	s.auditSign(ctx, req, err)
	return sig, err
}

func (s *sshService) signDigest(ctx context.Context, req SSHSignRequest) (*SSHSignature, error) {
	signer, err := s.client.Keys().Signer(ctx, xkmsssh.GetKeyRequest{
		Scope: req.Scope, Owner: req.Owner, Name: req.Name,
	})
	if err != nil {
		return nil, err
	}
	sig, err := signer.Sign(rand.Reader, req.Digest)
	if err != nil {
		return nil, err
	}
	return &SSHSignature{Format: sig.Format, Blob: sig.Blob, Rest: sig.Rest}, nil
}

// auditSign records a signature, or a refused one, against the principal that
// asked for it. It is best effort: signing never fails because the audit log
// is unavailable.
func (s *sshService) auditSign(ctx context.Context, req SSHSignRequest, cause error) {
	if s.audit == nil {
		return
	}
	logger, err := s.audit.GetAuditLogger(ctx)
	if err != nil || logger == nil {
		return
	}
	event := &iam.AuditEvent{
		ID:        uuid.New().String(),
		Timestamp: time.Now().UTC(),
		EventType: iam.AuditEventAppEvent,
		Category:  iam.AuditCategorySecurity,
		Action:    sshSignAuditAction,
		Outcome:   iam.AuditOutcomeSuccess,
		Resource:  &iam.AuditResource{Type: "ssh_key", ID: req.Scope + "/" + req.Owner + "/" + req.Name, Name: req.Name},
	}
	if subject, ok := auth.SubjectFromContext(ctx); ok {
		event.TenantID = subject.TenantID
		event.Actor = &iam.AuditActor{UserID: subject.UserID, Username: subject.Username, Roles: subject.Roles}
		if event.Actor.Username == "" {
			event.Actor.Username = subject.ID
		}
	}
	if cause != nil {
		event.Outcome = iam.AuditOutcomeFailure
		event.Details = map[string]interface{}{"error": cause.Error()}
	}
	logger.LogAsync(event)
}

// AddKnownHost pins a host-key fingerprint.
func (s *sshService) AddKnownHost(ctx context.Context, req xkmsssh.AddKnownHostRequest) error {
	if s.client == nil {
		return &SSHNotConfiguredError{}
	}
	return s.client.KnownHosts().Add(ctx, req)
}

// ListKnownHosts enumerates pinned host keys.
func (s *sshService) ListKnownHosts(ctx context.Context, req xkmsssh.ListKnownHostsRequest) ([]*xkmsssh.Pin, error) {
	if s.client == nil {
		return nil, &SSHNotConfiguredError{}
	}
	return s.client.KnownHosts().List(ctx, req)
}

// RemoveKnownHost removes a pinned host key.
func (s *sshService) RemoveKnownHost(ctx context.Context, req xkmsssh.RemoveKnownHostRequest) error {
	if s.client == nil {
		return &SSHNotConfiguredError{}
	}
	return s.client.KnownHosts().Remove(ctx, req)
}

// AddAuthorizedKey persists an authorized_keys entry.
func (s *sshService) AddAuthorizedKey(ctx context.Context, req xkmsssh.AddAuthorizedKeyRequest) error {
	if s.client == nil {
		return &SSHNotConfiguredError{}
	}
	return s.client.AuthorizedKeys().Add(ctx, req)
}

// ListAuthorizedKeys enumerates authorized_keys entries.
func (s *sshService) ListAuthorizedKeys(ctx context.Context, req xkmsssh.ListAuthorizedKeysRequest) ([]*xkmsssh.AuthorizedEntry, error) {
	if s.client == nil {
		return nil, &SSHNotConfiguredError{}
	}
	return s.client.AuthorizedKeys().List(ctx, req)
}

// RemoveAuthorizedKey deletes an authorized_keys entry.
func (s *sshService) RemoveAuthorizedKey(ctx context.Context, req SSHAuthorizedKeyRef) error {
	if s.client == nil {
		return &SSHNotConfiguredError{}
	}
	return s.client.AuthorizedKeys().Remove(ctx, req.Scope, req.Owner, req.Name)
}

// EmitAuthorizedKey formats a public key as an authorized_keys line.
func (s *sshService) EmitAuthorizedKey(ctx context.Context, req xkmsssh.EmitRequest) (string, error) {
	if s.client == nil {
		return "", &SSHNotConfiguredError{}
	}
	return s.client.AuthorizedKeys().Emit(ctx, req)
}

// =============================================================================
// Helpers
// =============================================================================

// sshPassphraseProviderFromB64 decodes a base64 passphrase into an mlocked
// buffer and wraps it in the xkms PassphraseProvider contract. An empty
// string yields a nil provider, which the xkms client reads as "the PEM is
// not encrypted".
func sshPassphraseProviderFromB64(encoded string) (secure.PassphraseProvider, error) {
	if encoded == "" {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, &SSHPassphraseDecodeError{Err: err}
	}
	defer secure.Wipe(raw)
	buf, err := secure.NewBuffer(len(raw))
	if err != nil {
		return nil, err
	}
	//secure:unborrowed allocates-protected copy moves caller bytes into a new protected passphrase buffer.
	copy(buf.Bytes(), raw)
	return &sshStaticPassphraseProvider{buf: buf}, nil
}

// sshStaticPassphraseProvider serves a fixed, mlocked passphrase. Each call
// returns a fresh clone so the xkms client can destroy what it receives
// without invalidating the provider.
type sshStaticPassphraseProvider struct {
	buf *secure.Buffer
}

// Passphrase implements secure.PassphraseProvider.
func (p *sshStaticPassphraseProvider) Passphrase(_ context.Context, _ string) (*secure.Buffer, error) {
	clone, err := secure.NewBuffer(p.buf.Size())
	if err != nil {
		return nil, err
	}
	//secure:unborrowed allocates-protected copy moves bytes between protected buffers for caller-owned cloning.
	copy(clone.Bytes(), p.buf.Bytes())
	return clone, nil
}
