//go:build rest

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

package rest

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	gossh "golang.org/x/crypto/ssh"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/rest/secureio"
)

// =============================================================================
// SSH Handlers — xkms-backed
// =============================================================================
//
// These handlers proxy HTTP requests to the xkms SSH SDK client owned by
// service.QuicBaaS. There is no business logic here — every operation
// delegates to xkmsssh.Client directly. Error → HTTP-status mapping uses
// the SDK's shared dispatcher (xkmsssh.HTTPStatusForError) so REST,
// embedded, and gRPC transports all agree.
//
// Routing uses the /api/v1/ssh prefix. See server.go for the mount point.

// =============================================================================
// Request / Response DTOs
// =============================================================================

// sshGenerateKeyRequest mirrors xkmsssh.GenerateRequest for the wire.
type sshGenerateKeyRequest struct {
	Scope     string `json:"scope"`
	Owner     string `json:"owner"`
	Name      string `json:"name"`
	Algorithm string `json:"algorithm"`
	RSABits   int    `json:"rsa_bits,omitempty"`
	Comment   string `json:"comment,omitempty"`
}

// sshImportKeyRequest carries the PEM payload plus an optional
// base64-encoded passphrase. The Passphrase field decodes directly
// into a mlocked buffer via secureio.PasswordField — plaintext never
// touches a Go string.
type sshImportKeyRequest struct {
	Scope      string                 `json:"scope"`
	Owner      string                 `json:"owner"`
	Name       string                 `json:"name"`
	Comment    string                 `json:"comment,omitempty"`
	PEM        []byte                 `json:"pem"`
	Passphrase secureio.PasswordField `json:"passphrase_b64,omitempty"`
}

// sshKeyListResponse wraps the key list for JSON serialization.
type sshKeyListResponse struct {
	Items []*xkmsssh.Key `json:"items"`
}

// sshKnownHostsListResponse wraps the known-hosts list.
type sshKnownHostsListResponse struct {
	Items []*xkmsssh.Pin `json:"items"`
}

// sshAuthorizedListResponse wraps the authorized-keys list.
type sshAuthorizedListResponse struct {
	Items []*xkmsssh.AuthorizedEntry `json:"items"`
}

// sshVerifyKnownHostRequest is the body accepted by
// POST /known-hosts/{host}/verify. The handler compares Fingerprint
// against the stored pin without requiring the caller to ship the full
// OpenSSH public-key bytes over JSON.
type sshVerifyKnownHostRequest struct {
	Scope       string `json:"scope"`
	Owner       string `json:"owner"`
	Fingerprint string `json:"fingerprint"`
	Algorithm   string `json:"algorithm,omitempty"`
}

// sshVerifyAuthorizedRequest is the body accepted by
// POST /authorized/verify.
type sshVerifyAuthorizedRequest struct {
	Scope       string `json:"scope"`
	Owner       string `json:"owner"`
	Fingerprint string `json:"fingerprint"`
	Algorithm   string `json:"algorithm,omitempty"`
}

// sshEmitResponse is the body returned by POST /authorized.
type sshEmitResponse struct {
	Line string `json:"line"`
}

// sshSignRequest is the body accepted by POST /keys/{...}/sign. The
// digest is raw bytes — clients base64-encode it per JSON conventions.
type sshSignRequest struct {
	Digest []byte `json:"digest"`
}

// sshSignResponse mirrors *ssh.Signature.
type sshSignResponse struct {
	Format string `json:"format"`
	Blob   []byte `json:"blob"`
	Rest   []byte `json:"rest,omitempty"`
}

// sshMismatchResponse is the 409 body emitted for host-key mismatches.
// Having the extra fields first-class on the response lets downstream
// SDK clients reconstruct *xkmsssh.MismatchError-like observables.
type sshMismatchResponse struct {
	Error    string `json:"error"`
	Resource string `json:"resource"`
	Host     string `json:"host"`
	Expected string `json:"expected"`
	Received string `json:"received"`
}

// =============================================================================
// Typed Errors
// =============================================================================

// SSHServiceUnavailableError is returned when the QuicBaaS service has
// no SSH client configured (SSHClient() returned nil).
type SSHServiceUnavailableError struct{}

// Error implements the error interface.
func (e *SSHServiceUnavailableError) Error() string {
	return "rest/ssh: ssh client is not configured on this server"
}

// SSHBadBodyError signals a request body that failed to decode.
type SSHBadBodyError struct {
	Err error
}

// Error implements the error interface.
func (e *SSHBadBodyError) Error() string {
	if e.Err == nil {
		return "rest/ssh: bad request body"
	}
	return "rest/ssh: bad request body: " + e.Err.Error()
}

// Unwrap exposes the underlying decode error for errors.Is / errors.As.
func (e *SSHBadBodyError) Unwrap() error { return e.Err }

// SSHMissingDigestError is returned when the sign endpoint receives a
// request with no digest bytes.
type SSHMissingDigestError struct{}

// Error implements the error interface.
func (e *SSHMissingDigestError) Error() string {
	return "rest/ssh: digest is required"
}

// SSHHostNotPinnedError is returned by the known-hosts verify endpoint
// when no pin exists for the given (scope, owner, host) triple.
type SSHHostNotPinnedError struct {
	Scope string
	Owner string
	Host  string
}

// Error implements the error interface.
func (e *SSHHostNotPinnedError) Error() string {
	return "rest/ssh: no host pin for " + e.Scope + "/" + e.Owner + "/" + e.Host
}

// SSHAuthorizedNotFoundError is returned by the authorized verify
// endpoint when the supplied fingerprint does not match any stored
// authorized entry for (scope, owner).
type SSHAuthorizedNotFoundError struct {
	Scope string
	Owner string
}

// Error implements the error interface.
func (e *SSHAuthorizedNotFoundError) Error() string {
	return "rest/ssh: no authorized key for " + e.Scope + "/" + e.Owner
}

// =============================================================================
// Routes
// =============================================================================

// registerSSHRoutes installs the SSH route table on r. Both the canonical
// /api/v1/ssh mount and the /api/v1/xkms/v1/ssh alias (the prefix the
// go-xkms REST SSH client expects) share this single table, so the two
// mounts can never drift apart.
func (s *Server) registerSSHRoutes(r chi.Router) {
	r.Post("/keys", s.HandleGenerateSSHKey)
	r.Post("/keys/import", s.HandleImportSSHKey)
	r.Get("/keys", s.HandleListSSHKeys)
	r.Get("/keys/{scope}/{owner}/{name}", s.HandleGetSSHKey)
	r.Delete("/keys/{scope}/{owner}/{name}", s.HandleDeleteSSHKey)
	r.Get("/keys/{scope}/{owner}/{name}/public", s.HandleExportSSHKeyPublic)
	r.Post("/keys/{scope}/{owner}/{name}/sign", s.HandleSignSSHDigest)

	r.Post("/known-hosts", s.HandleAddKnownHost)
	r.Get("/known-hosts", s.HandleListKnownHosts)
	r.Delete("/known-hosts/{host}", s.HandleRemoveKnownHost)
	r.Post("/known-hosts/{host}/verify", s.HandleVerifyKnownHost)

	r.Post("/authorized", s.HandleEmitAuthorized)
	r.Post("/authorized/verify", s.HandleVerifyAuthorized)
	r.Post("/authorized/keys", s.HandleAddAuthorizedKey)
	r.Get("/authorized/keys", s.HandleListAuthorizedKeys)
	r.Get("/authorized/keys/{scope}/{owner}/{name}", s.HandleGetAuthorizedKey)
	r.Delete("/authorized/keys/{scope}/{owner}/{name}", s.HandleRemoveAuthorizedKey)
}

// =============================================================================
// Handlers — Keys
// =============================================================================

// HandleGenerateSSHKey creates a new SSH key via xkmsssh.Keys().Generate.
func (s *Server) HandleGenerateSSHKey(w http.ResponseWriter, r *http.Request) {
	client, ok := s.requireSSHClient(w, r)
	if !ok {
		return
	}
	var req sshGenerateKeyRequest
	if err := decodeSSHBody(r, &req); err != nil {
		writeSSHError(w, err)
		return
	}
	out, err := client.Keys().Generate(r.Context(), xkmsssh.GenerateRequest{
		Scope:     req.Scope,
		Owner:     req.Owner,
		Name:      req.Name,
		Algorithm: req.Algorithm,
		RSABits:   req.RSABits,
		Comment:   req.Comment,
	})
	if err != nil {
		writeSSHError(w, err)
		return
	}
	writeSSHJSON(w, out, http.StatusCreated)
}

// HandleImportSSHKey imports a PEM-encoded private key. The passphrase
// (when present) decodes directly into a mlocked buffer — no Go string
// holds the plaintext.
func (s *Server) HandleImportSSHKey(w http.ResponseWriter, r *http.Request) {
	client, ok := s.requireSSHClient(w, r)
	if !ok {
		return
	}
	var req sshImportKeyRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		req.Passphrase.Destroy()
		writeSSHError(w, &SSHBadBodyError{Err: err})
		return
	}
	defer req.Passphrase.Destroy()

	var provider secure.PassphraseProvider
	if buf := req.Passphrase.Buffer(); buf != nil && buf.Size() > 0 {
		provider = &sshBufferProvider{buf: buf}
	}

	out, err := client.Keys().Import(r.Context(), xkmsssh.ImportRequest{
		Scope:   req.Scope,
		Owner:   req.Owner,
		Name:    req.Name,
		Comment: req.Comment,
		PEM:     req.PEM,
	}, provider)
	if err != nil {
		writeSSHError(w, err)
		return
	}
	writeSSHJSON(w, out, http.StatusCreated)
}

// HandleListSSHKeys enumerates keys for the (scope, owner) query pair.
func (s *Server) HandleListSSHKeys(w http.ResponseWriter, r *http.Request) {
	client, ok := s.requireSSHClient(w, r)
	if !ok {
		return
	}
	list, err := client.Keys().List(r.Context(), xkmsssh.ListKeysRequest{
		Scope: r.URL.Query().Get("scope"),
		Owner: r.URL.Query().Get("owner"),
	})
	if err != nil {
		writeSSHError(w, err)
		return
	}
	writeSSHJSON(w, sshKeyListResponse{Items: list}, http.StatusOK)
}

// HandleGetSSHKey returns the metadata for a single SSH key.
func (s *Server) HandleGetSSHKey(w http.ResponseWriter, r *http.Request) {
	client, ok := s.requireSSHClient(w, r)
	if !ok {
		return
	}
	out, err := client.Keys().Get(r.Context(), xkmsssh.GetKeyRequest{
		Scope: chi.URLParam(r, "scope"),
		Owner: chi.URLParam(r, "owner"),
		Name:  chi.URLParam(r, "name"),
	})
	if err != nil {
		writeSSHError(w, err)
		return
	}
	writeSSHJSON(w, out, http.StatusOK)
}

// HandleDeleteSSHKey removes a key by (scope, owner, name).
func (s *Server) HandleDeleteSSHKey(w http.ResponseWriter, r *http.Request) {
	client, ok := s.requireSSHClient(w, r)
	if !ok {
		return
	}
	err := client.Keys().Delete(r.Context(), xkmsssh.DeleteKeyRequest{
		Scope: chi.URLParam(r, "scope"),
		Owner: chi.URLParam(r, "owner"),
		Name:  chi.URLParam(r, "name"),
	})
	if err != nil {
		writeSSHError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleExportSSHKeyPublic returns the authorized_keys-form public key.
func (s *Server) HandleExportSSHKeyPublic(w http.ResponseWriter, r *http.Request) {
	client, ok := s.requireSSHClient(w, r)
	if !ok {
		return
	}
	out, err := client.Keys().ExportPublic(r.Context(), xkmsssh.ExportRequest{
		Scope: chi.URLParam(r, "scope"),
		Owner: chi.URLParam(r, "owner"),
		Name:  chi.URLParam(r, "name"),
	})
	if err != nil {
		writeSSHError(w, err)
		return
	}
	writeSSHJSON(w, out, http.StatusOK)
}

// HandleSignSSHDigest signs a raw digest with the named key. The
// handler opens a short-lived enclave-backed signer, signs, and lets
// the signer fall out of scope — the embedded SDK destroys the
// enclave on each sign, so no explicit close is required.
func (s *Server) HandleSignSSHDigest(w http.ResponseWriter, r *http.Request) {
	client, ok := s.requireSSHClient(w, r)
	if !ok {
		return
	}
	var req sshSignRequest
	if err := decodeSSHBody(r, &req); err != nil {
		writeSSHError(w, err)
		return
	}
	if len(req.Digest) == 0 {
		writeSSHError(w, &SSHMissingDigestError{})
		return
	}
	signer, err := client.Keys().Signer(r.Context(), xkmsssh.GetKeyRequest{
		Scope: chi.URLParam(r, "scope"),
		Owner: chi.URLParam(r, "owner"),
		Name:  chi.URLParam(r, "name"),
	})
	if err != nil {
		writeSSHError(w, err)
		return
	}
	sig, err := signer.Sign(sshRandReader(r.Context()), req.Digest)
	if err != nil {
		writeSSHError(w, err)
		return
	}
	writeSSHJSON(w, sshSignResponse{
		Format: sig.Format,
		Blob:   sig.Blob,
		Rest:   sig.Rest,
	}, http.StatusOK)
}

// =============================================================================
// Handlers — Known Hosts
// =============================================================================

// HandleAddKnownHost pins a host-key fingerprint.
func (s *Server) HandleAddKnownHost(w http.ResponseWriter, r *http.Request) {
	client, ok := s.requireSSHClient(w, r)
	if !ok {
		return
	}
	var req xkmsssh.AddKnownHostRequest
	if err := decodeSSHBody(r, &req); err != nil {
		writeSSHError(w, err)
		return
	}
	if err := client.KnownHosts().Add(r.Context(), req); err != nil {
		writeSSHError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// HandleListKnownHosts enumerates pinned host keys.
func (s *Server) HandleListKnownHosts(w http.ResponseWriter, r *http.Request) {
	client, ok := s.requireSSHClient(w, r)
	if !ok {
		return
	}
	list, err := client.KnownHosts().List(r.Context(), xkmsssh.ListKnownHostsRequest{
		Scope: r.URL.Query().Get("scope"),
		Owner: r.URL.Query().Get("owner"),
	})
	if err != nil {
		writeSSHError(w, err)
		return
	}
	writeSSHJSON(w, sshKnownHostsListResponse{Items: list}, http.StatusOK)
}

// HandleRemoveKnownHost removes a pinned host key.
func (s *Server) HandleRemoveKnownHost(w http.ResponseWriter, r *http.Request) {
	client, ok := s.requireSSHClient(w, r)
	if !ok {
		return
	}
	err := client.KnownHosts().Remove(r.Context(), xkmsssh.RemoveKnownHostRequest{
		Scope: r.URL.Query().Get("scope"),
		Owner: r.URL.Query().Get("owner"),
		Host:  chi.URLParam(r, "host"),
	})
	if err != nil {
		writeSSHError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleVerifyKnownHost compares the supplied fingerprint against the
// stored pin. On mismatch the handler emits 409 with the extra
// Expected/Received fields so callers can reconstruct a
// *xkmsssh.MismatchError observable.
func (s *Server) HandleVerifyKnownHost(w http.ResponseWriter, r *http.Request) {
	client, ok := s.requireSSHClient(w, r)
	if !ok {
		return
	}
	var req sshVerifyKnownHostRequest
	if err := decodeSSHBody(r, &req); err != nil {
		writeSSHError(w, err)
		return
	}
	host := chi.URLParam(r, "host")
	pins, err := client.KnownHosts().List(r.Context(), xkmsssh.ListKnownHostsRequest{
		Scope: req.Scope, Owner: req.Owner,
	})
	if err != nil {
		writeSSHError(w, err)
		return
	}
	var pin *xkmsssh.Pin
	for _, p := range pins {
		if p.Host == host {
			pin = p
			break
		}
	}
	if pin == nil {
		writeSSHError(w, &SSHHostNotPinnedError{Scope: req.Scope, Owner: req.Owner, Host: host})
		return
	}
	if pin.Fingerprint != req.Fingerprint {
		writeSSHMismatch(w, host, pin.Fingerprint, req.Fingerprint)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// =============================================================================
// Handlers — Authorized Keys
// =============================================================================

// HandleEmitAuthorized formats a line for the caller's storage.
func (s *Server) HandleEmitAuthorized(w http.ResponseWriter, r *http.Request) {
	client, ok := s.requireSSHClient(w, r)
	if !ok {
		return
	}
	var req xkmsssh.EmitRequest
	if err := decodeSSHBody(r, &req); err != nil {
		writeSSHError(w, err)
		return
	}
	line, err := client.AuthorizedKeys().Emit(r.Context(), req)
	if err != nil {
		writeSSHError(w, err)
		return
	}
	writeSSHJSON(w, sshEmitResponse{Line: line}, http.StatusOK)
}

// HandleAddAuthorizedKey persists an authorized_keys entry. Added so the
// authorized-keys surface is complete on REST: the SDK client, the CLI, and
// the desktop binding all need add/list/remove, not just emit/verify.
func (s *Server) HandleAddAuthorizedKey(w http.ResponseWriter, r *http.Request) {
	client, ok := s.requireSSHClient(w, r)
	if !ok {
		return
	}
	var req xkmsssh.AddAuthorizedKeyRequest
	if err := decodeSSHBody(r, &req); err != nil {
		writeSSHError(w, err)
		return
	}
	if err := client.AuthorizedKeys().Add(r.Context(), req); err != nil {
		writeSSHError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// HandleListAuthorizedKeys enumerates authorized_keys entries for the
// (scope, owner) query pair.
func (s *Server) HandleListAuthorizedKeys(w http.ResponseWriter, r *http.Request) {
	client, ok := s.requireSSHClient(w, r)
	if !ok {
		return
	}
	list, err := client.AuthorizedKeys().List(r.Context(), xkmsssh.ListAuthorizedKeysRequest{
		Scope: r.URL.Query().Get("scope"),
		Owner: r.URL.Query().Get("owner"),
	})
	if err != nil {
		writeSSHError(w, err)
		return
	}
	writeSSHJSON(w, sshAuthorizedListResponse{Items: list}, http.StatusOK)
}

// HandleGetAuthorizedKey returns one authorized_keys entry by name.
func (s *Server) HandleGetAuthorizedKey(w http.ResponseWriter, r *http.Request) {
	client, ok := s.requireSSHClient(w, r)
	if !ok {
		return
	}
	entry, err := client.AuthorizedKeys().Get(r.Context(),
		chi.URLParam(r, "scope"), chi.URLParam(r, "owner"), chi.URLParam(r, "name"))
	if err != nil {
		writeSSHError(w, err)
		return
	}
	writeSSHJSON(w, entry, http.StatusOK)
}

// HandleRemoveAuthorizedKey deletes one authorized_keys entry by name.
func (s *Server) HandleRemoveAuthorizedKey(w http.ResponseWriter, r *http.Request) {
	client, ok := s.requireSSHClient(w, r)
	if !ok {
		return
	}
	err := client.AuthorizedKeys().Remove(r.Context(),
		chi.URLParam(r, "scope"), chi.URLParam(r, "owner"), chi.URLParam(r, "name"))
	if err != nil {
		writeSSHError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleVerifyAuthorized checks a fingerprint against the stored
// authorized entries for (scope, owner).
func (s *Server) HandleVerifyAuthorized(w http.ResponseWriter, r *http.Request) {
	client, ok := s.requireSSHClient(w, r)
	if !ok {
		return
	}
	var req sshVerifyAuthorizedRequest
	if err := decodeSSHBody(r, &req); err != nil {
		writeSSHError(w, err)
		return
	}
	list, err := client.AuthorizedKeys().List(r.Context(), xkmsssh.ListAuthorizedKeysRequest{
		Scope: req.Scope, Owner: req.Owner,
	})
	if err != nil {
		writeSSHError(w, err)
		return
	}
	for _, entry := range list {
		pub, _, _, _, perr := gossh.ParseAuthorizedKey([]byte(entry.Line))
		if perr != nil {
			continue
		}
		if gossh.FingerprintSHA256(pub) == req.Fingerprint {
			writeSSHJSON(w, entry, http.StatusOK)
			return
		}
	}
	writeSSHError(w, &SSHAuthorizedNotFoundError{Scope: req.Scope, Owner: req.Owner})
}

// =============================================================================
// Helpers
// =============================================================================

// requireSSHClient returns the server's xkmsssh.Client or writes a
// 503 and reports ok=false. Handlers use this as the first line to
// fail-fast when SSH is not configured.
func (s *Server) requireSSHClient(w http.ResponseWriter, _ *http.Request) (xkmsssh.Client, bool) {
	client := s.service.SSHClient()
	if client == nil {
		writeSSHError(w, &SSHServiceUnavailableError{})
		return nil, false
	}
	return client, true
}

// decodeSSHBody reads and JSON-decodes a request body. Returns a
// *SSHBadBodyError on any decode failure so writeSSHError can set a
// 400 status code consistently.
func decodeSSHBody(r *http.Request, dst any) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return &SSHBadBodyError{Err: err}
	}
	defer r.Body.Close()
	if len(body) == 0 {
		return &SSHBadBodyError{Err: stderrors.New("empty body")}
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return &SSHBadBodyError{Err: err}
	}
	return nil
}

// writeSSHJSON serializes v to JSON at the given status code.
func writeSSHJSON(w http.ResponseWriter, v any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeSSHError maps err to an HTTP status and writes a JSON envelope
// compatible with the xkms REST client decoder.
func writeSSHError(w http.ResponseWriter, err error) {
	status := sshErrorStatus(err)
	env := struct {
		Error    string `json:"error"`
		Resource string `json:"resource,omitempty"`
	}{
		Error:    err.Error(),
		Resource: sshErrorResource(err),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(env)
}

// writeSSHMismatch emits the extra host/expected/received fields on
// a 409 so clients can reconstruct the mismatch observable.
func writeSSHMismatch(w http.ResponseWriter, host, expected, received string) {
	env := sshMismatchResponse{
		Error:    "ssh/known_hosts: host key mismatch",
		Resource: "known_hosts",
		Host:     host,
		Expected: expected,
		Received: received,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(env)
}

// sshErrorStatus classifies an error into an HTTP status code. Uses
// the SDK-shared dispatcher for xkmsssh errors, and layers on the
// transport-specific errors defined in this file.
func sshErrorStatus(err error) int {
	var notCfg *SSHServiceUnavailableError
	if stderrors.As(err, &notCfg) {
		return http.StatusServiceUnavailable
	}
	var badBody *SSHBadBodyError
	if stderrors.As(err, &badBody) {
		return http.StatusBadRequest
	}
	var missingDigest *SSHMissingDigestError
	if stderrors.As(err, &missingDigest) {
		return http.StatusBadRequest
	}
	var hostNotPinned *SSHHostNotPinnedError
	if stderrors.As(err, &hostNotPinned) {
		return http.StatusNotFound
	}
	var authNotFound *SSHAuthorizedNotFoundError
	if stderrors.As(err, &authNotFound) {
		return http.StatusNotFound
	}
	return xkmsssh.HTTPStatusForError(err)
}

// sshErrorResource returns the "resource" label for err — "key",
// "known_hosts", or "authorized_key". Used to disambiguate 404 / 409
// responses across the three resource families.
func sshErrorResource(err error) string {
	var hostNotPinned *SSHHostNotPinnedError
	if stderrors.As(err, &hostNotPinned) {
		return "known_hosts"
	}
	var authNotFound *SSHAuthorizedNotFoundError
	if stderrors.As(err, &authNotFound) {
		return "authorized_key"
	}
	return ""
}

// sshBufferProvider wraps an mlocked *secure.Buffer into the SDK
// PassphraseProvider contract. Each call returns a clone so the
// caller's Destroy on the original buffer does not mutate the
// provider's view.
type sshBufferProvider struct {
	buf *secure.Buffer
}

// Passphrase implements secure.PassphraseProvider.
func (p *sshBufferProvider) Passphrase(_ context.Context, _ string) (*secure.Buffer, error) {
	if p == nil || p.buf == nil {
		return nil, &SSHBadBodyError{Err: stderrors.New("nil passphrase provider")}
	}
	cloned, err := secure.NewBuffer(p.buf.Size())
	if err != nil {
		return nil, err
	}
	copy(cloned.Bytes(), p.buf.Bytes())
	return cloned, nil
}

// sshRandReader is kept as a small indirection so callers can substitute
// a deterministic reader during tests. It returns crypto/rand.Reader by
// default (the ctx is accepted for future cancellation plumbing).
var sshRandReader = func(_ context.Context) io.Reader { return sshCryptoRand() }
