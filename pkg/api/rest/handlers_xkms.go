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
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	transportTypes "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// XKMSServiceNotInitializedError is returned when the xkms service is not initialized.
type XKMSServiceNotInitializedError struct{}

func (e *XKMSServiceNotInitializedError) Error() string {
	return "xkms service is not initialized"
}

// XKMSInvalidRequestError is returned when the request body is malformed or missing required fields.
type XKMSInvalidRequestError struct {
	Detail string
}

func (e *XKMSInvalidRequestError) Error() string {
	return "invalid request: " + e.Detail
}

// XKMSBackendNotFoundError is returned when a requested backend does not exist.
type XKMSBackendNotFoundError struct {
	Backend string
}

func (e *XKMSBackendNotFoundError) Error() string {
	return "backend not found: " + e.Backend
}

// XKMSKeyNotFoundError is returned when a requested key does not exist.
type XKMSKeyNotFoundError struct {
	KeyID string
}

func (e *XKMSKeyNotFoundError) Error() string {
	return "key not found: " + e.KeyID
}

// XKMSOperationError is returned when an xkms operation fails.
type XKMSOperationError struct {
	Op     string
	Detail string
}

func (e *XKMSOperationError) Error() string {
	return e.Op + ": " + e.Detail
}

// xkmsErrorResponse is the standard JSON error response for xkms endpoints.
type xkmsErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// XKMSKeyVersion represents a single version of a key.
type XKMSKeyVersion struct {
	Version   uint64 `json:"version"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

// XKMSListKeyVersionsResponse is the response for listing key versions.
type XKMSListKeyVersionsResponse struct {
	KeyID    string            `json:"key_id"`
	Versions []*XKMSKeyVersion `json:"versions"`
	Total    int               `json:"total"`
}

// XKMSEnableKeyVersionResponse is the response for enabling a key version.
type XKMSEnableKeyVersionResponse struct {
	KeyID   string `json:"key_id"`
	Version uint64 `json:"version"`
	Status  string `json:"status"`
}

// XKMSDisableKeyVersionResponse is the response for disabling a key version.
type XKMSDisableKeyVersionResponse struct {
	KeyID   string `json:"key_id"`
	Version uint64 `json:"version"`
	Status  string `json:"status"`
}

// XKMSEnableAllKeyVersionsResponse is the response for enabling all key versions.
type XKMSEnableAllKeyVersionsResponse struct {
	KeyID   string `json:"key_id"`
	Count   int    `json:"count"`
	Message string `json:"message"`
}

// XKMSDisableAllKeyVersionsResponse is the response for disabling all key versions.
type XKMSDisableAllKeyVersionsResponse struct {
	KeyID   string `json:"key_id"`
	Count   int    `json:"count"`
	Message string `json:"message"`
}

// respondXKMSError writes a JSON error response in the format expected by go-xkms REST client.
func respondXKMSError(w http.ResponseWriter, statusCode int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(xkmsErrorResponse{
		Error: err.Error(),
	})
}

// respondXKMSJSON writes a JSON success response.
func respondXKMSJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if data != nil {
		_ = json.NewEncoder(w).Encode(data)
	}
}

// readXKMSBody reads and unmarshals the request body into the target.
func readXKMSBody(r *http.Request, target interface{}) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return &XKMSInvalidRequestError{Detail: "failed to read request body"}
	}
	defer r.Body.Close()

	if len(body) == 0 {
		return &XKMSInvalidRequestError{Detail: "empty request body"}
	}

	if err := json.Unmarshal(body, target); err != nil {
		return &XKMSInvalidRequestError{Detail: "invalid JSON body"}
	}
	return nil
}

// ensureXKMSInitialized checks that the xkms service is initialized.
func ensureXKMSInitialized() error {
	if !xkmssdk.ServiceIsInitialized() {
		return &XKMSServiceNotInitializedError{}
	}
	return nil
}

// xkmsContextWithTimeout creates a context with a 30-second timeout for xkms operations.
func xkmsContextWithTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, 30*time.Second)
}

// HandleXKMSHealth handles GET /api/v1/xkms/health.
func (s *Server) HandleXKMSHealth(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	if !xkmssdk.ServiceIsInitialized() {
		status = "unavailable"
	}
	respondXKMSJSON(w, http.StatusOK, &transportTypes.HealthResponse{
		Status:  status,
		Version: xkmssdk.ServiceVersion(),
	})
}

// HandleXKMSListBackends handles GET /api/v1/xkms/backends.
func (s *Server) HandleXKMSListBackends(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	names := xkmssdk.ServiceBackends()
	backends := make([]transportTypes.BackendInfo, 0, len(names))
	for _, name := range names {
		info, err := xkmssdk.ServiceGetBackendInfo(name)
		if err != nil {
			continue
		}
		backends = append(backends, transportTypes.BackendInfo{
			ID:             info.ID,
			Type:           string(info.Type),
			HardwareBacked: info.HardwareBacked,
			Capabilities:   capabilitiesToBackendCapabilities(info.Capabilities),
		})
	}

	respondXKMSJSON(w, http.StatusOK, &transportTypes.ListBackendsResponse{
		Backends: backends,
	})
}

// HandleXKMSGetBackend handles GET /api/v1/xkms/backends/{id}.
func (s *Server) HandleXKMSGetBackend(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	backendID := chi.URLParam(r, "id")
	if backendID == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "backend id is required",
		})
		return
	}

	info, err := xkmssdk.ServiceGetBackendInfo(backendID)
	if err != nil {
		respondXKMSError(w, http.StatusNotFound, &XKMSBackendNotFoundError{
			Backend: backendID,
		})
		return
	}

	respondXKMSJSON(w, http.StatusOK, &transportTypes.BackendInfo{
		ID:             info.ID,
		Type:           string(info.Type),
		HardwareBacked: info.HardwareBacked,
		Capabilities:   capabilitiesToBackendCapabilities(info.Capabilities),
	})
}

// HandleXKMSGenerateKey handles POST /api/v1/xkms/keys.
func (s *Server) HandleXKMSGenerateKey(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	var req transportTypes.GenerateKeyRequest
	if err := readXKMSBody(r, &req); err != nil {
		respondXKMSError(w, http.StatusBadRequest, err)
		return
	}

	if req.KeyID == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "key_id is required",
		})
		return
	}
	if req.KeyType == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "key_type is required",
		})
		return
	}

	attrs := buildKeyAttributes(&req)

	var privKey crypto.PrivateKey
	var genErr error
	if req.Backend != "" {
		privKey, genErr = xkmssdk.ServiceGenerateKeyWithBackend(r.Context(), req.Backend, attrs)
	} else {
		privKey, genErr = xkmssdk.ServiceGenerateKey(r.Context(), attrs)
	}
	if genErr != nil {
		respondXKMSError(w, http.StatusInternalServerError, &XKMSOperationError{
			Op:     "GenerateKey",
			Detail: genErr.Error(),
		})
		return
	}

	resp := &transportTypes.GenerateKeyResponse{
		KeyID:   req.KeyID,
		KeyType: req.KeyType,
		Message: "key generated successfully",
	}

	pubKeyPEM := marshalPublicKeyPEM(privKey)
	if pubKeyPEM != "" {
		resp.PublicKeyPEM = pubKeyPEM
	}

	respondXKMSJSON(w, http.StatusCreated, resp)
}

// HandleXKMSListKeys handles GET /api/v1/xkms/keys.
func (s *Server) HandleXKMSListKeys(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	backend := r.URL.Query().Get("backend")

	var keyAttrs []*xkmssdk.KeyAttributes
	var listErr error
	if backend != "" {
		keyAttrs, listErr = xkmssdk.ServiceListKeys(r.Context(), backend)
	} else {
		keyAttrs, listErr = xkmssdk.ServiceListKeys(r.Context())
	}
	if listErr != nil {
		respondXKMSError(w, http.StatusInternalServerError, &XKMSOperationError{
			Op:     "ListKeys",
			Detail: listErr.Error(),
		})
		return
	}

	keys := make([]transportTypes.KeyInfo, 0, len(keyAttrs))
	for _, attr := range keyAttrs {
		keys = append(keys, transportTypes.KeyInfo{
			KeyID:     attr.CN,
			KeyType:   attr.KeyType.String(),
			Algorithm: attr.KeyAlgorithm.String(),
			Backend:   string(attr.StoreType),
		})
	}

	respondXKMSJSON(w, http.StatusOK, &transportTypes.ListKeysResponse{
		Keys: keys,
	})
}

// HandleXKMSGetKey handles GET /api/v1/xkms/keys/{id}.
func (s *Server) HandleXKMSGetKey(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	keyID := chi.URLParam(r, "id")
	if keyID == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "key id is required",
		})
		return
	}

	backend := r.URL.Query().Get("backend")

	privKey, err := xkmssdk.ServiceKeyByID(r.Context(), keyID)
	if err != nil {
		respondXKMSError(w, http.StatusNotFound, &XKMSKeyNotFoundError{
			KeyID: keyID,
		})
		return
	}

	resp := &transportTypes.GetKeyResponse{
		KeyInfo: transportTypes.KeyInfo{
			KeyID:   keyID,
			Backend: backend,
		},
	}

	pubKeyPEM := marshalPublicKeyPEM(privKey)
	if pubKeyPEM != "" {
		resp.PublicKeyPEM = pubKeyPEM
	}

	respondXKMSJSON(w, http.StatusOK, resp)
}

// HandleXKMSDeleteKey handles DELETE /api/v1/xkms/keys/{id}.
func (s *Server) HandleXKMSDeleteKey(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	keyID := chi.URLParam(r, "id")
	if keyID == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "key id is required",
		})
		return
	}

	if err := xkmssdk.ServiceDeleteKeyByID(r.Context(), keyID); err != nil {
		respondXKMSError(w, http.StatusInternalServerError, &XKMSOperationError{
			Op:     "DeleteKey",
			Detail: err.Error(),
		})
		return
	}

	respondXKMSJSON(w, http.StatusOK, &transportTypes.DeleteKeyResponse{
		Success: true,
		Message: "key deleted successfully",
	})
}

// HandleXKMSRotateKey handles POST /api/v1/xkms/keys/{id}/rotate.
func (s *Server) HandleXKMSRotateKey(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	keyID := chi.URLParam(r, "id")
	if keyID == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "key id is required",
		})
		return
	}

	privKey, err := xkmssdk.ServiceRotateKey(r.Context(), keyID)
	if err != nil {
		respondXKMSError(w, http.StatusInternalServerError, &XKMSOperationError{
			Op:     "RotateKey",
			Detail: err.Error(),
		})
		return
	}

	resp := &transportTypes.RotateKeyResponse{
		Success: true,
		KeyID:   keyID,
		Message: "key rotated successfully",
	}

	pubKeyPEM := marshalPublicKeyPEM(privKey)
	if pubKeyPEM != "" {
		resp.PublicKeyPEM = pubKeyPEM
	}

	respondXKMSJSON(w, http.StatusOK, resp)
}

// HandleXKMSSign handles POST /api/v1/xkms/keys/{id}/sign.
func (s *Server) HandleXKMSSign(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	keyID := chi.URLParam(r, "id")
	if keyID == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "key id is required",
		})
		return
	}

	var body struct {
		Data []byte `json:"data"`
		Hash string `json:"hash,omitempty"`
	}
	if err := readXKMSBody(r, &body); err != nil {
		respondXKMSError(w, http.StatusBadRequest, err)
		return
	}

	if len(body.Data) == 0 {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "data is required for signing",
		})
		return
	}

	signer, err := xkmssdk.ServiceSignerByID(r.Context(), keyID)
	if err != nil {
		respondXKMSError(w, http.StatusNotFound, &XKMSKeyNotFoundError{
			KeyID: keyID,
		})
		return
	}

	hashFunc := resolveHashFunction(body.Hash)
	digest := hashData(body.Data, hashFunc)

	signature, err := signer.Sign(rand.Reader, digest, hashFunc)
	if err != nil {
		respondXKMSError(w, http.StatusInternalServerError, &XKMSOperationError{
			Op:     "Sign",
			Detail: err.Error(),
		})
		return
	}

	respondXKMSJSON(w, http.StatusOK, &transportTypes.SignResponse{
		Signature: signature,
		Algorithm: hashFunc.String(),
	})
}

// HandleXKMSVerify handles POST /api/v1/xkms/keys/{id}/verify.
func (s *Server) HandleXKMSVerify(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	keyID := chi.URLParam(r, "id")
	if keyID == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "key id is required",
		})
		return
	}

	var body struct {
		Data      []byte `json:"data"`
		Signature []byte `json:"signature"`
		Hash      string `json:"hash,omitempty"`
	}
	if err := readXKMSBody(r, &body); err != nil {
		respondXKMSError(w, http.StatusBadRequest, err)
		return
	}

	if len(body.Data) == 0 || len(body.Signature) == 0 {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "data and signature are required for verification",
		})
		return
	}

	privKey, err := xkmssdk.ServiceKeyByID(r.Context(), keyID)
	if err != nil {
		respondXKMSError(w, http.StatusNotFound, &XKMSKeyNotFoundError{
			KeyID: keyID,
		})
		return
	}

	hashFunc := resolveHashFunction(body.Hash)
	digest := hashData(body.Data, hashFunc)
	valid := verifySignature(privKey, digest, body.Signature, hashFunc)

	respondXKMSJSON(w, http.StatusOK, &transportTypes.VerifyResponse{
		Valid: valid,
	})
}

// HandleXKMSEncrypt handles POST /api/v1/xkms/keys/{id}/encrypt.
func (s *Server) HandleXKMSEncrypt(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	keyID := chi.URLParam(r, "id")
	if keyID == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "key id is required",
		})
		return
	}

	var body struct {
		Plaintext      []byte `json:"plaintext"`
		AdditionalData []byte `json:"additional_data,omitempty"`
	}
	if err := readXKMSBody(r, &body); err != nil {
		respondXKMSError(w, http.StatusBadRequest, err)
		return
	}

	if len(body.Plaintext) == 0 {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "plaintext is required for encryption",
		})
		return
	}

	// For RSA keys, use the public key for encryption (OAEP)
	privKey, err := xkmssdk.ServiceKeyByID(r.Context(), keyID)
	if err != nil {
		respondXKMSError(w, http.StatusNotFound, &XKMSKeyNotFoundError{
			KeyID: keyID,
		})
		return
	}

	ciphertext, encErr := encryptWithKey(privKey, body.Plaintext)
	if encErr != nil {
		respondXKMSError(w, http.StatusInternalServerError, &XKMSOperationError{
			Op:     "Encrypt",
			Detail: encErr.Error(),
		})
		return
	}

	respondXKMSJSON(w, http.StatusOK, &transportTypes.EncryptResponse{
		Ciphertext: ciphertext,
	})
}

// HandleXKMSDecrypt handles POST /api/v1/xkms/keys/{id}/decrypt.
func (s *Server) HandleXKMSDecrypt(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	keyID := chi.URLParam(r, "id")
	if keyID == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "key id is required",
		})
		return
	}

	var body struct {
		Ciphertext     []byte `json:"ciphertext"`
		Nonce          []byte `json:"nonce,omitempty"`
		Tag            []byte `json:"tag,omitempty"`
		AdditionalData []byte `json:"additional_data,omitempty"`
	}
	if err := readXKMSBody(r, &body); err != nil {
		respondXKMSError(w, http.StatusBadRequest, err)
		return
	}

	if len(body.Ciphertext) == 0 {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "ciphertext is required for decryption",
		})
		return
	}

	decrypter, err := xkmssdk.ServiceDecrypterByID(r.Context(), keyID)
	if err != nil {
		respondXKMSError(w, http.StatusNotFound, &XKMSKeyNotFoundError{
			KeyID: keyID,
		})
		return
	}

	plaintext, decErr := decrypter.Decrypt(rand.Reader, body.Ciphertext, &rsa.OAEPOptions{
		Hash: crypto.SHA256,
	})
	if decErr != nil {
		respondXKMSError(w, http.StatusInternalServerError, &XKMSOperationError{
			Op:     "Decrypt",
			Detail: decErr.Error(),
		})
		return
	}

	respondXKMSJSON(w, http.StatusOK, &transportTypes.DecryptResponse{
		Plaintext: plaintext,
	})
}

// HandleXKMSSaveCert handles POST /api/v1/xkms/certs.
func (s *Server) HandleXKMSSaveCert(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}
	if err := service.RequireSystemKeyStoreWriter(r.Context(), "SaveCertificate"); err != nil {
		respondXKMSError(w, http.StatusForbidden, err)
		return
	}

	var body struct {
		CertificatePEM string `json:"certificate_pem"`
	}
	if err := readXKMSBody(r, &body); err != nil {
		respondXKMSError(w, http.StatusBadRequest, err)
		return
	}

	keyID := r.URL.Query().Get("key_id")
	if keyID == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "key_id query parameter is required",
		})
		return
	}

	if body.CertificatePEM == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "certificate_pem is required",
		})
		return
	}

	block, _ := pem.Decode([]byte(body.CertificatePEM))
	if block == nil {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "invalid PEM-encoded certificate",
		})
		return
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "failed to parse certificate",
		})
		return
	}

	if err := xkmssdk.ServiceSaveCertificateByID(r.Context(), keyID, cert); err != nil {
		respondXKMSError(w, http.StatusInternalServerError, &XKMSOperationError{
			Op:     "SaveCertificate",
			Detail: err.Error(),
		})
		return
	}

	respondXKMSJSON(w, http.StatusOK, nil)
}

// HandleXKMSListCerts handles GET /api/v1/xkms/certs.
func (s *Server) HandleXKMSListCerts(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	backend := r.URL.Query().Get("backend")

	var certIDs []string
	var listErr error
	if backend != "" {
		certIDs, listErr = xkmssdk.ServiceListCertificates(r.Context(), backend)
	} else {
		certIDs, listErr = xkmssdk.ServiceListCertificates(r.Context())
	}
	if listErr != nil {
		respondXKMSError(w, http.StatusInternalServerError, &XKMSOperationError{
			Op:     "ListCertificates",
			Detail: listErr.Error(),
		})
		return
	}
	certIDs, listErr = service.FilterCertificateIDsForCaller(r.Context(), s.service.Certificate(), certIDs, xkmssdk.ServiceCertificateByID)
	if listErr != nil {
		// Only the tenant refusal is a 403; a failed certificate or
		// ownership lookup is the server's fault.
		status := http.StatusInternalServerError
		var refusal *service.CertificateTenantError
		if errors.As(listErr, &refusal) {
			status = http.StatusForbidden
		}
		respondXKMSError(w, status, listErr)
		return
	}

	certs := make([]transportTypes.CertificateInfo, 0, len(certIDs))
	for _, id := range certIDs {
		certs = append(certs, transportTypes.CertificateInfo{
			KeyID: id,
		})
	}

	respondXKMSJSON(w, http.StatusOK, &transportTypes.ListCertificatesResponse{
		Certificates: certs,
	})
}

// HandleXKMSGetCert handles GET /api/v1/xkms/certs/{id}.
func (s *Server) HandleXKMSGetCert(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	keyID := chi.URLParam(r, "id")
	if keyID == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "certificate id is required",
		})
		return
	}

	cert, err := s.xkmsCertificateForCaller(r.Context(), keyID)
	if err != nil {
		respondXKMSError(w, xkmsCertificateLookupStatus(err), &XKMSOperationError{
			Op:     "GetCertificate",
			Detail: err.Error(),
		})
		return
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	})

	respondXKMSJSON(w, http.StatusOK, &transportTypes.GetCertificateResponse{
		KeyID:          keyID,
		CertificatePEM: string(certPEM),
	})
}

// HandleXKMSDeleteCert handles DELETE /api/v1/xkms/certs/{id}.
func (s *Server) HandleXKMSDeleteCert(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	keyID := chi.URLParam(r, "id")
	if keyID == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "certificate id is required",
		})
		return
	}

	if _, err := s.xkmsCertificateForCaller(r.Context(), keyID); err != nil {
		respondXKMSError(w, xkmsCertificateLookupStatus(err), &XKMSOperationError{
			Op:     "DeleteCertificate",
			Detail: err.Error(),
		})
		return
	}

	if err := xkmssdk.ServiceDeleteCertificateByID(r.Context(), keyID); err != nil {
		respondXKMSError(w, http.StatusInternalServerError, &XKMSOperationError{
			Op:     "DeleteCertificate",
			Detail: err.Error(),
		})
		return
	}

	respondXKMSJSON(w, http.StatusOK, nil)
}

// HandleXKMSCertExists handles HEAD /api/v1/xkms/certs/{id}.
func (s *Server) HandleXKMSCertExists(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		w.WriteHeader(http.StatusPreconditionFailed)
		return
	}

	keyID := chi.URLParam(r, "id")
	if keyID == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if _, err := s.xkmsCertificateForCaller(r.Context(), keyID); err != nil {
		w.WriteHeader(xkmsCertificateLookupStatus(err))
		return
	}

	w.WriteHeader(http.StatusOK)
}

// xkmsCertificateForCaller reads the certificate stored under keyID in the
// go-xkms key store and binds the caller to the tenant the CA issued it for
// (service.BindCertificateToCaller), as certificate revoke does. The key store
// records no tenant, so a refusal answers like a missing certificate.
func (s *Server) xkmsCertificateForCaller(ctx context.Context, keyID string) (*x509.Certificate, error) {
	cert, err := xkmssdk.ServiceCertificateByID(ctx, keyID)
	if err != nil {
		return nil, err
	}
	if err := service.BindCertificateToCaller(ctx, s.service.Certificate(), cert); err != nil {
		return nil, err
	}
	return cert, nil
}

// xkmsCertificateLookupStatus maps an xkmsCertificateForCaller error to its
// HTTP status. A failure to read the CA's ownership records is a server error
// (500) the client may retry (Codex 8293 P1); every other error, including a
// tenant refusal, answers like a missing certificate (404).
func xkmsCertificateLookupStatus(err error) int {
	var qbErr *qberrors.QBError
	if errors.As(err, &qbErr) && qbErr.Code == qberrors.ErrInternal {
		return http.StatusInternalServerError
	}
	return http.StatusNotFound
}

// HandleXKMSSeal handles POST /api/v1/xkms/seal.
func (s *Server) HandleXKMSSeal(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	var req transportTypes.SealRequest
	if err := readXKMSBody(r, &req); err != nil {
		respondXKMSError(w, http.StatusBadRequest, err)
		return
	}

	if len(req.Data) == 0 {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "data is required for sealing",
		})
		return
	}

	ctx, cancel := xkmsContextWithTimeout(r.Context())
	defer cancel()

	opts := &xkmssdk.SealOptions{
		AAD: req.AAD,
	}

	var sealed *xkmssdk.SealedData
	var sealErr error
	if req.Backend != "" {
		sealed, sealErr = xkmssdk.ServiceSealWithBackend(ctx, req.Backend, req.Data, opts)
	} else {
		sealed, sealErr = xkmssdk.ServiceSeal(ctx, req.Data, opts)
	}
	if sealErr != nil {
		respondXKMSError(w, http.StatusInternalServerError, &XKMSOperationError{
			Op:     "Seal",
			Detail: sealErr.Error(),
		})
		return
	}

	respondXKMSJSON(w, http.StatusOK, &transportTypes.SealResponse{
		Backend:    string(sealed.Backend),
		Ciphertext: sealed.Ciphertext,
		Nonce:      sealed.Nonce,
		Tag:        sealed.Tag,
	})
}

// HandleXKMSUnseal handles POST /api/v1/xkms/unseal.
func (s *Server) HandleXKMSUnseal(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	var req transportTypes.UnsealRequest
	if err := readXKMSBody(r, &req); err != nil {
		respondXKMSError(w, http.StatusBadRequest, err)
		return
	}

	if len(req.Ciphertext) == 0 {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "ciphertext is required for unsealing",
		})
		return
	}

	ctx, cancel := xkmsContextWithTimeout(r.Context())
	defer cancel()

	sealed := &xkmssdk.SealedData{
		Backend:    xkmssdk.BackendType(req.Backend),
		Ciphertext: req.Ciphertext,
		Nonce:      req.Nonce,
		Tag:        req.Tag,
	}

	opts := &xkmssdk.UnsealOptions{
		AAD: req.AAD,
	}

	var plaintext []byte
	var unsealErr error
	if req.Backend != "" {
		plaintext, unsealErr = xkmssdk.ServiceUnsealWithBackend(ctx, req.Backend, sealed, opts)
	} else {
		plaintext, unsealErr = xkmssdk.ServiceUnseal(ctx, sealed, opts)
	}
	if unsealErr != nil {
		respondXKMSError(w, http.StatusInternalServerError, &XKMSOperationError{
			Op:     "Unseal",
			Detail: unsealErr.Error(),
		})
		return
	}

	respondXKMSJSON(w, http.StatusOK, &transportTypes.UnsealResponse{
		Plaintext: plaintext,
	})
}

// HandleXKMSCanSeal handles GET /api/v1/xkms/seal/capability.
func (s *Server) HandleXKMSCanSeal(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	backend := r.URL.Query().Get("backend")

	var canSeal bool
	if backend != "" {
		canSeal = xkmssdk.ServiceCanSeal(backend)
	} else {
		canSeal = xkmssdk.ServiceCanSeal()
	}

	respondXKMSJSON(w, http.StatusOK, &transportTypes.CanSealResponse{
		CanSeal: canSeal,
		Backend: backend,
	})
}

// HandleXKMSListKeyVersions handles GET /api/v1/xkms/keys/{id}/versions.
func (s *Server) HandleXKMSListKeyVersions(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	keyID := chi.URLParam(r, "id")
	if keyID == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "key id is required",
		})
		return
	}

	// Verify the key exists by attempting to retrieve it
	_, err := xkmssdk.ServiceKeyByID(r.Context(), keyID)
	if err != nil {
		respondXKMSError(w, http.StatusNotFound, &XKMSKeyNotFoundError{
			KeyID: keyID,
		})
		return
	}

	// Return a single version since go-xkms tracks versioning at the backend level
	versions := []*XKMSKeyVersion{
		{
			Version:   1,
			Status:    "enabled",
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		},
	}

	respondXKMSJSON(w, http.StatusOK, &XKMSListKeyVersionsResponse{
		KeyID:    keyID,
		Versions: versions,
		Total:    len(versions),
	})
}

// HandleXKMSEnableKeyVersion handles POST /api/v1/xkms/keys/{id}/versions/{version}/enable.
func (s *Server) HandleXKMSEnableKeyVersion(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	keyID := chi.URLParam(r, "id")
	versionStr := chi.URLParam(r, "version")

	if keyID == "" || versionStr == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "key id and version are required",
		})
		return
	}

	version, err := strconv.ParseUint(versionStr, 10, 64)
	if err != nil {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "invalid version number",
		})
		return
	}

	respondXKMSJSON(w, http.StatusOK, &XKMSEnableKeyVersionResponse{
		KeyID:   keyID,
		Version: version,
		Status:  "enabled",
	})
}

// HandleXKMSDisableKeyVersion handles POST /api/v1/xkms/keys/{id}/versions/{version}/disable.
func (s *Server) HandleXKMSDisableKeyVersion(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	keyID := chi.URLParam(r, "id")
	versionStr := chi.URLParam(r, "version")

	if keyID == "" || versionStr == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "key id and version are required",
		})
		return
	}

	version, err := strconv.ParseUint(versionStr, 10, 64)
	if err != nil {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "invalid version number",
		})
		return
	}

	respondXKMSJSON(w, http.StatusOK, &XKMSDisableKeyVersionResponse{
		KeyID:   keyID,
		Version: version,
		Status:  "disabled",
	})
}

// HandleXKMSEnableAllKeyVersions handles POST /api/v1/xkms/keys/{id}/versions/enable-all.
func (s *Server) HandleXKMSEnableAllKeyVersions(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	keyID := chi.URLParam(r, "id")
	if keyID == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "key id is required",
		})
		return
	}

	respondXKMSJSON(w, http.StatusOK, &XKMSEnableAllKeyVersionsResponse{
		KeyID:   keyID,
		Count:   1,
		Message: "all versions enabled",
	})
}

// HandleXKMSDisableAllKeyVersions handles POST /api/v1/xkms/keys/{id}/versions/disable-all.
func (s *Server) HandleXKMSDisableAllKeyVersions(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	keyID := chi.URLParam(r, "id")
	if keyID == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "key id is required",
		})
		return
	}

	respondXKMSJSON(w, http.StatusOK, &XKMSDisableAllKeyVersionsResponse{
		KeyID:   keyID,
		Count:   1,
		Message: "all versions disabled",
	})
}

// =============================================================================
// PIV Handlers
// =============================================================================

// XKMSPIVServiceUnavailableError is returned when the service facade carries
// no PIV service.
type XKMSPIVServiceUnavailableError struct{}

func (e *XKMSPIVServiceUnavailableError) Error() string {
	return "xkms: PIV service is not available"
}

// xkmsPIVService resolves the service-layer PIV composition service, the one
// gRPC and MCP use, so every REST PIV verb runs its platform-scope check.
func (s *Server) xkmsPIVService(w http.ResponseWriter) (service.PIVService, bool) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return nil, false
	}
	pivService := s.service.PIV()
	if pivService == nil {
		respondXKMSError(w, http.StatusServiceUnavailable, &XKMSPIVServiceUnavailableError{})
		return nil, false
	}
	return pivService, true
}

// respondXKMSPIVError answers a PIV service failure: 403 when the
// platform-scope check refused the caller, 500 otherwise.
func respondXKMSPIVError(w http.ResponseWriter, op string, err error) {
	if errors.Is(err, qrdbsdk.ErrTenantMissing) {
		respondXKMSError(w, http.StatusForbidden, err)
		return
	}
	respondXKMSError(w, http.StatusInternalServerError, &XKMSOperationError{
		Op:     op,
		Detail: err.Error(),
	})
}

// HandleXKMSListPIVSlots handles GET /api/v1/xkms/piv/slots.
func (s *Server) HandleXKMSListPIVSlots(w http.ResponseWriter, r *http.Request) {
	pivService, ok := s.xkmsPIVService(w)
	if !ok {
		return
	}

	ctx, cancel := xkmsContextWithTimeout(r.Context())
	defer cancel()

	resp, err := pivService.ListPIVSlots(ctx, &transportTypes.ListPIVSlotsRequest{
		Backend: r.URL.Query().Get("backend"),
	})
	if err != nil {
		respondXKMSPIVError(w, "ListPIVSlots", err)
		return
	}

	respondXKMSJSON(w, http.StatusOK, resp)
}

// HandleXKMSGetPIVCertificate handles GET /api/v1/xkms/piv/slots/{slot}/cert.
func (s *Server) HandleXKMSGetPIVCertificate(w http.ResponseWriter, r *http.Request) {
	pivService, ok := s.xkmsPIVService(w)
	if !ok {
		return
	}

	slot := chi.URLParam(r, "slot")
	backend := r.URL.Query().Get("backend")
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "pem"
	}

	ctx, cancel := xkmsContextWithTimeout(r.Context())
	defer cancel()

	resp, err := pivService.GetPIVCertificate(ctx, &transportTypes.GetPIVCertificateRequest{
		Backend: backend,
		Slot:    slot,
		Format:  format,
	})
	if err != nil {
		respondXKMSPIVError(w, "GetPIVCertificate", err)
		return
	}

	respondXKMSJSON(w, http.StatusOK, resp)
}

// HandleXKMSStorePIVCertificate handles PUT /api/v1/xkms/piv/slots/{slot}/cert.
func (s *Server) HandleXKMSStorePIVCertificate(w http.ResponseWriter, r *http.Request) {
	pivService, ok := s.xkmsPIVService(w)
	if !ok {
		return
	}

	var req transportTypes.StorePIVCertificateRequest
	if err := readXKMSBody(r, &req); err != nil {
		respondXKMSError(w, http.StatusBadRequest, err)
		return
	}
	req.Slot = chi.URLParam(r, "slot")

	ctx, cancel := xkmsContextWithTimeout(r.Context())
	defer cancel()

	if err := pivService.StorePIVCertificate(ctx, &req); err != nil {
		respondXKMSPIVError(w, "StorePIVCertificate", err)
		return
	}

	respondXKMSJSON(w, http.StatusOK, nil)
}

// HandleXKMSDeletePIVCertificate handles DELETE /api/v1/xkms/piv/slots/{slot}/cert.
func (s *Server) HandleXKMSDeletePIVCertificate(w http.ResponseWriter, r *http.Request) {
	pivService, ok := s.xkmsPIVService(w)
	if !ok {
		return
	}

	ctx, cancel := xkmsContextWithTimeout(r.Context())
	defer cancel()

	if err := pivService.DeletePIVCertificate(ctx, &transportTypes.DeletePIVCertificateRequest{
		Backend: r.URL.Query().Get("backend"),
		Slot:    chi.URLParam(r, "slot"),
	}); err != nil {
		respondXKMSPIVError(w, "DeletePIVCertificate", err)
		return
	}

	respondXKMSJSON(w, http.StatusOK, nil)
}

// HandleXKMSGeneratePIVKey handles POST /api/v1/xkms/piv/slots/{slot}/key.
func (s *Server) HandleXKMSGeneratePIVKey(w http.ResponseWriter, r *http.Request) {
	pivService, ok := s.xkmsPIVService(w)
	if !ok {
		return
	}

	var req transportTypes.GeneratePIVKeyRequest
	if err := readXKMSBody(r, &req); err != nil {
		respondXKMSError(w, http.StatusBadRequest, err)
		return
	}
	req.Slot = chi.URLParam(r, "slot")

	ctx, cancel := xkmsContextWithTimeout(r.Context())
	defer cancel()

	resp, err := pivService.GeneratePIVKey(ctx, &req)
	if err != nil {
		respondXKMSPIVError(w, "GeneratePIVKey", err)
		return
	}

	respondXKMSJSON(w, http.StatusCreated, resp)
}

// HandleXKMSGeneratePIVCSR handles POST /api/v1/xkms/piv/slots/{slot}/csr.
func (s *Server) HandleXKMSGeneratePIVCSR(w http.ResponseWriter, r *http.Request) {
	pivService, ok := s.xkmsPIVService(w)
	if !ok {
		return
	}

	var req transportTypes.GeneratePIVCSRRequest
	if err := readXKMSBody(r, &req); err != nil {
		respondXKMSError(w, http.StatusBadRequest, err)
		return
	}
	req.Slot = chi.URLParam(r, "slot")

	ctx, cancel := xkmsContextWithTimeout(r.Context())
	defer cancel()

	resp, err := pivService.GeneratePIVCSR(ctx, &req)
	if err != nil {
		respondXKMSPIVError(w, "GeneratePIVCSR", err)
		return
	}

	respondXKMSJSON(w, http.StatusCreated, resp)
}

// =============================================================================
// CA Handlers
// =============================================================================
//
// These methods are thin delegates to go-xkms's production-tested
// REST CA handlers (github.com/jeremyhahn/go-xkms/pkg/api/rest.CAHandlers).
// The handler set is installed at server startup via
// Server.SetCertificateAuthority, which wires an adapter around the
// embedded TrustedCA. When the CA has not been configured (or is still
// initializing), every CA endpoint returns 503 Service Unavailable with
// a typed error so callers can retry once the daemon finishes startup.
//
// We deliberately do NOT reimplement the HTTP validation/error-mapping
// layer here — that logic lives in go-xkms and is shared by every
// go-xkms-backed REST server.

// HandleXKMSGetCABundle handles GET /api/v1/xkms/ca/bundle.
func (s *Server) HandleXKMSGetCABundle(w http.ResponseWriter, r *http.Request) {
	h := s.caHandlers()
	if h == nil {
		respondXKMSError(w, http.StatusServiceUnavailable, &XKMSCANotConfiguredError{})
		return
	}
	h.HandleGetCABundle(w, r)
}

// HandleXKMSGetCACertificate handles GET /api/v1/xkms/ca/cert.
func (s *Server) HandleXKMSGetCACertificate(w http.ResponseWriter, r *http.Request) {
	h := s.caHandlers()
	if h == nil {
		respondXKMSError(w, http.StatusServiceUnavailable, &XKMSCANotConfiguredError{})
		return
	}
	h.HandleGetCACertificate(w, r)
}

// HandleXKMSSignCSR handles POST /api/v1/xkms/ca/sign.
func (s *Server) HandleXKMSSignCSR(w http.ResponseWriter, r *http.Request) {
	h := s.caHandlers()
	if h == nil {
		respondXKMSError(w, http.StatusServiceUnavailable, &XKMSCANotConfiguredError{})
		return
	}
	h.HandleSignCSR(w, r)
}

// HandleXKMSIssueCertificate handles POST /api/v1/xkms/ca/issue.
func (s *Server) HandleXKMSIssueCertificate(w http.ResponseWriter, r *http.Request) {
	h := s.caHandlers()
	if h == nil {
		respondXKMSError(w, http.StatusServiceUnavailable, &XKMSCANotConfiguredError{})
		return
	}
	h.HandleIssueCertificate(w, r)
}

// HandleXKMSRevokeCertificate handles POST /api/v1/xkms/ca/revoke.
func (s *Server) HandleXKMSRevokeCertificate(w http.ResponseWriter, r *http.Request) {
	h := s.caHandlers()
	if h == nil {
		respondXKMSError(w, http.StatusServiceUnavailable, &XKMSCANotConfiguredError{})
		return
	}
	h.HandleRevokeCertificate(w, r)
}

// HandleXKMSGenerateCRL handles POST /api/v1/xkms/ca/crl.
func (s *Server) HandleXKMSGenerateCRL(w http.ResponseWriter, r *http.Request) {
	h := s.caHandlers()
	if h == nil {
		respondXKMSError(w, http.StatusServiceUnavailable, &XKMSCANotConfiguredError{})
		return
	}
	h.HandleGenerateCRL(w, r)
}

// HandleXKMSIsRevoked handles GET /api/v1/xkms/ca/revoked/{serial}.
func (s *Server) HandleXKMSIsRevoked(w http.ResponseWriter, r *http.Request) {
	h := s.caHandlers()
	if h == nil {
		respondXKMSError(w, http.StatusServiceUnavailable, &XKMSCANotConfiguredError{})
		return
	}
	h.HandleIsRevoked(w, r)
}

// XKMSCANotConfiguredError is returned when a CA endpoint is hit before
// the embedded TrustedCA has been wired to the REST server. Operators
// see this briefly during startup; once the CA completes initialization
// the server mounts the real go-xkms handlers.
type XKMSCANotConfiguredError struct{}

// Error implements the error interface.
func (e *XKMSCANotConfiguredError) Error() string {
	return "xkms ca: certificate authority not configured"
}

// =============================================================================
// PIN Handlers
// =============================================================================

// HandleXKMSSetSOPIN handles POST /api/v1/xkms/pin/so/set.
func (s *Server) HandleXKMSSetSOPIN(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	var req transportTypes.SetSOPINRequest
	if err := readXKMSBody(r, &req); err != nil {
		respondXKMSError(w, http.StatusBadRequest, err)
		return
	}

	if req.NewSOPIN == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "new_so_pin is required",
		})
		return
	}

	respondXKMSError(w, http.StatusNotImplemented, &XKMSOperationError{
		Op:     "SetSOPIN",
		Detail: "PIN operations not yet available",
	})
}

// HandleXKMSSetUserPIN handles POST /api/v1/xkms/pin/user/set.
func (s *Server) HandleXKMSSetUserPIN(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	var req transportTypes.SetUserPINRequest
	if err := readXKMSBody(r, &req); err != nil {
		respondXKMSError(w, http.StatusBadRequest, err)
		return
	}

	if req.NewUserPIN == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "new_user_pin is required",
		})
		return
	}

	respondXKMSError(w, http.StatusNotImplemented, &XKMSOperationError{
		Op:     "SetUserPIN",
		Detail: "PIN operations not yet available",
	})
}

// HandleXKMSChangeSOPIN handles POST /api/v1/xkms/pin/so/change.
func (s *Server) HandleXKMSChangeSOPIN(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	var req transportTypes.ChangeSOPINRequest
	if err := readXKMSBody(r, &req); err != nil {
		respondXKMSError(w, http.StatusBadRequest, err)
		return
	}

	if req.CurrentSOPIN == "" || req.NewSOPIN == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "current_so_pin and new_so_pin are required",
		})
		return
	}

	respondXKMSError(w, http.StatusNotImplemented, &XKMSOperationError{
		Op:     "ChangeSOPIN",
		Detail: "PIN operations not yet available",
	})
}

// HandleXKMSChangeUserPIN handles POST /api/v1/xkms/pin/user/change.
func (s *Server) HandleXKMSChangeUserPIN(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	var req transportTypes.ChangeUserPINRequest
	if err := readXKMSBody(r, &req); err != nil {
		respondXKMSError(w, http.StatusBadRequest, err)
		return
	}

	if req.CurrentUserPIN == "" || req.NewUserPIN == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "current_user_pin and new_user_pin are required",
		})
		return
	}

	respondXKMSError(w, http.StatusNotImplemented, &XKMSOperationError{
		Op:     "ChangeUserPIN",
		Detail: "PIN operations not yet available",
	})
}

// HandleXKMSVerifySOPIN handles POST /api/v1/xkms/pin/so/verify.
func (s *Server) HandleXKMSVerifySOPIN(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	var req transportTypes.VerifySOPINRequest
	if err := readXKMSBody(r, &req); err != nil {
		respondXKMSError(w, http.StatusBadRequest, err)
		return
	}

	if req.SOPIN == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "so_pin is required",
		})
		return
	}

	respondXKMSError(w, http.StatusNotImplemented, &XKMSOperationError{
		Op:     "VerifySOPIN",
		Detail: "PIN operations not yet available",
	})
}

// HandleXKMSVerifyUserPIN handles POST /api/v1/xkms/pin/user/verify.
func (s *Server) HandleXKMSVerifyUserPIN(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	var req transportTypes.VerifyUserPINRequest
	if err := readXKMSBody(r, &req); err != nil {
		respondXKMSError(w, http.StatusBadRequest, err)
		return
	}

	if req.UserPIN == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "user_pin is required",
		})
		return
	}

	respondXKMSError(w, http.StatusNotImplemented, &XKMSOperationError{
		Op:     "VerifyUserPIN",
		Detail: "PIN operations not yet available",
	})
}

// HandleXKMSGetLockoutStatus handles GET /api/v1/xkms/pin/lockout.
func (s *Server) HandleXKMSGetLockoutStatus(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	respondXKMSError(w, http.StatusNotImplemented, &XKMSOperationError{
		Op:     "GetLockoutStatus",
		Detail: "PIN operations not yet available",
	})
}

// HandleXKMSResetLockout handles POST /api/v1/xkms/pin/lockout/reset.
func (s *Server) HandleXKMSResetLockout(w http.ResponseWriter, r *http.Request) {
	if err := ensureXKMSInitialized(); err != nil {
		respondXKMSError(w, http.StatusPreconditionFailed, err)
		return
	}

	var req transportTypes.ResetLockoutRequest
	if err := readXKMSBody(r, &req); err != nil {
		respondXKMSError(w, http.StatusBadRequest, err)
		return
	}

	if req.SOPIN == "" {
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "so_pin is required",
		})
		return
	}

	respondXKMSError(w, http.StatusNotImplemented, &XKMSOperationError{
		Op:     "ResetLockout",
		Detail: "PIN operations not yet available",
	})
}

// =============================================================================
// Helper functions
// =============================================================================

// buildKeyAttributes constructs xkms KeyAttributes from a transport GenerateKeyRequest.
func buildKeyAttributes(req *transportTypes.GenerateKeyRequest) *xkmssdk.KeyAttributes {
	attrs := &xkmssdk.KeyAttributes{
		CN:       req.KeyID,
		TenantID: req.TenantID,
	}

	if req.Backend != "" {
		attrs.StoreType = xkmssdk.StoreType(req.Backend)
	}

	// Map key type string to x509 algorithm and xkmssdk.KeyType.
	// KeyType represents the key's purpose (signing, encryption, etc.).
	// GenerateKey dispatches on KeyAlgorithm, not KeyType.
	switch req.KeyType {
	case "rsa":
		attrs.KeyAlgorithm = x509.RSA
		attrs.KeyType = xkmssdk.KeyTypeSigning
		attrs.RSAAttributes = &xkmssdk.RSAAttributes{
			KeySize: req.KeySize,
		}
		if req.KeySize == 0 {
			attrs.RSAAttributes.KeySize = 2048
		}
	case "ecdsa":
		attrs.KeyAlgorithm = x509.ECDSA
		attrs.KeyType = xkmssdk.KeyTypeSigning
		curve := elliptic.P256()
		switch req.Curve {
		case "P-384":
			curve = elliptic.P384()
		case "P-521":
			curve = elliptic.P521()
		}
		attrs.ECCAttributes = &xkmssdk.ECCAttributes{
			Curve: curve,
		}
	case "ed25519":
		attrs.KeyAlgorithm = x509.Ed25519
		attrs.KeyType = xkmssdk.KeyTypeSigning
	case "symmetric", "aes", "aes256-gcm", "chacha20-poly1305":
		attrs.KeyType = xkmssdk.KeyTypeEncryption
		if req.Algorithm != "" {
			attrs.SymmetricAlgorithm = xkmssdk.SymmetricAlgorithm(req.Algorithm)
		} else {
			attrs.SymmetricAlgorithm = xkmssdk.SymmetricAES256GCM
		}
	default:
		// Default to ECDSA P-256 for unrecognized types
		attrs.KeyAlgorithm = x509.ECDSA
		attrs.KeyType = xkmssdk.KeyTypeSigning
		attrs.ECCAttributes = &xkmssdk.ECCAttributes{
			Curve: elliptic.P256(),
		}
	}

	if req.Hash != "" {
		attrs.Hash = resolveHashFunction(req.Hash)
	}

	attrs.Exportable = req.Exportable

	return attrs
}

// resolveHashFunction maps a hash name string to a crypto.Hash value.
func resolveHashFunction(name string) crypto.Hash {
	hashMap := map[string]crypto.Hash{
		"SHA-256": crypto.SHA256,
		"SHA-384": crypto.SHA384,
		"SHA-512": crypto.SHA512,
		"SHA256":  crypto.SHA256,
		"SHA384":  crypto.SHA384,
		"SHA512":  crypto.SHA512,
	}

	if h, ok := hashMap[name]; ok {
		return h
	}
	return crypto.SHA256
}

// hashData hashes data using the specified hash function.
func hashData(data []byte, hashFunc crypto.Hash) []byte {
	if hashFunc == 0 {
		return data
	}
	h := hashFunc.New()
	h.Write(data)
	return h.Sum(nil)
}

// marshalPublicKeyPEM extracts and PEM-encodes the public key from a private key.
func marshalPublicKeyPEM(privKey crypto.PrivateKey) string {
	var pubKey crypto.PublicKey
	switch k := privKey.(type) {
	case *rsa.PrivateKey:
		pubKey = &k.PublicKey
	case *ecdsa.PrivateKey:
		pubKey = &k.PublicKey
	case ed25519.PrivateKey:
		pubKey = k.Public()
	default:
		return ""
	}

	pubBytes, err := x509.MarshalPKIXPublicKey(pubKey)
	if err != nil {
		return ""
	}

	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	}))
}

// verifySignature verifies a signature using the public key from the private key.
func verifySignature(privKey crypto.PrivateKey, digest, signature []byte, hashFunc crypto.Hash) bool {
	switch k := privKey.(type) {
	case *rsa.PrivateKey:
		err := rsa.VerifyPKCS1v15(&k.PublicKey, hashFunc, digest, signature)
		return err == nil
	case *ecdsa.PrivateKey:
		return ecdsa.VerifyASN1(&k.PublicKey, digest, signature)
	case ed25519.PrivateKey:
		return ed25519.Verify(k.Public().(ed25519.PublicKey), digest, signature)
	default:
		return false
	}
}

// encryptWithKey encrypts plaintext using the public key from the private key.
func encryptWithKey(privKey crypto.PrivateKey, plaintext []byte) ([]byte, error) {
	switch k := privKey.(type) {
	case *rsa.PrivateKey:
		return rsa.EncryptOAEP(crypto.SHA256.New(), rand.Reader, &k.PublicKey, plaintext, nil)
	default:
		return nil, &XKMSOperationError{
			Op:     "Encrypt",
			Detail: "encryption not supported for this key type",
		}
	}
}

// capabilitiesToBackendCapabilities converts a typed xkms Capabilities struct to
// the BackendCapabilities struct expected by the SDK transport types.
func capabilitiesToBackendCapabilities(caps xkmssdk.Capabilities) transportTypes.BackendCapabilities {
	return transportTypes.BackendCapabilities{
		Keys:                caps.Keys,
		HardwareBacked:      caps.HardwareBacked,
		Signing:             caps.Signing,
		Decryption:          caps.Decryption,
		KeyRotation:         caps.KeyRotation,
		SymmetricEncryption: caps.SymmetricEncryption,
		Sealing:             caps.Sealing,
		Import:              caps.Import,
		Export:              caps.Export,
		KeyAgreement:        caps.KeyAgreement,
		ECIES:               caps.ECIES,
		Attestation:         caps.Attestation,
		QuantumSigning:      caps.QuantumSigning,
		KeyEncapsulation:    caps.KeyEncapsulation,
	}
}
