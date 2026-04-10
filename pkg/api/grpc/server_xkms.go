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

package grpc

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// XKMSServer implements the xkms.v1.KeystoreService gRPC interface,
// delegating to go-xkms's backend layer. QuicBaaS uses its own gRPC
// interceptors for authentication and authorization, so no additional
// auth adapter is required here.
type XKMSServer struct {
	xkmssdk.ProtoUnimplementedKeystoreServiceServer
	logger *logger.Logger
}

// NewXKMSServer creates a new xkms-compatible gRPC server.
func NewXKMSServer(log *logger.Logger) *XKMSServer {
	return &XKMSServer{
		logger: log.WithField("component", "xkms-grpc-server"),
	}
}

// RegisterXKMSServer registers the xkms.v1.KeystoreService with a gRPC server.
func RegisterXKMSServer(server *grpc.Server, log *logger.Logger) {
	xkmssdk.ProtoRegisterKeystoreServiceServer(server, NewXKMSServer(log))
}

// Health returns the health status of the xkms service.
func (s *XKMSServer) Health(ctx context.Context, req *xkmssdk.ProtoHealthRequest) (*xkmssdk.ProtoHealthResponse, error) {
	return &xkmssdk.ProtoHealthResponse{
		Status:  "healthy",
		Version: xkmssdk.ServiceVersion(),
	}, nil
}

// ListBackends returns all available backend providers.
func (s *XKMSServer) ListBackends(ctx context.Context, req *xkmssdk.ProtoListBackendsRequest) (*xkmssdk.ProtoListBackendsResponse, error) {
	backendNames := xkmssdk.ServiceBackends()
	backends := make([]*xkmssdk.ProtoBackendInfo, 0, len(backendNames))
	for _, name := range backendNames {
		ks, err := xkmssdk.ServiceGetBackend(name)
		if err != nil {
			s.logger.WithError(err).WithField("backend", name).Warn("Failed to retrieve backend")
			continue
		}
		provider := ks.KeyProvider()
		caps := provider.Capabilities()
		backends = append(backends, &xkmssdk.ProtoBackendInfo{
			Name:                        name,
			Type:                        string(provider.Type()),
			Description:                 xkmsBackendDescription(provider.Type()),
			HardwareBacked:              caps.HardwareBacked,
			SupportsSigning:             caps.Signing,
			SupportsDecryption:          caps.Decryption,
			SupportsRotation:            caps.KeyRotation,
			SupportsSymmetricEncryption: caps.SymmetricEncryption,
		})
	}
	return &xkmssdk.ProtoListBackendsResponse{
		Backends: backends,
		Count:    int32(len(backends)), // #nosec G115
	}, nil
}

// GetBackendInfo returns detailed information about a specific backend.
func (s *XKMSServer) GetBackendInfo(ctx context.Context, req *xkmssdk.ProtoGetBackendInfoRequest) (*xkmssdk.ProtoGetBackendInfoResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "backend name is required")
	}
	ks, err := xkmssdk.ServiceGetBackend(req.Name)
	if err != nil {
		return nil, status.Error(codes.NotFound, "backend not found")
	}
	provider := ks.KeyProvider()
	caps := provider.Capabilities()
	return &xkmssdk.ProtoGetBackendInfoResponse{
		Backend: &xkmssdk.ProtoBackendInfo{
			Name:                        req.Name,
			Type:                        string(provider.Type()),
			Description:                 xkmsBackendDescription(provider.Type()),
			HardwareBacked:              caps.HardwareBacked,
			SupportsSigning:             caps.Signing,
			SupportsDecryption:          caps.Decryption,
			SupportsRotation:            caps.KeyRotation,
			SupportsSymmetricEncryption: caps.SymmetricEncryption,
		},
	}, nil
}

// GenerateKey generates a new cryptographic key.
func (s *XKMSServer) GenerateKey(ctx context.Context, req *xkmssdk.ProtoGenerateKeyRequest) (*xkmssdk.ProtoGenerateKeyResponse, error) {
	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}
	if req.Backend == "" {
		return nil, status.Error(codes.InvalidArgument, "backend is required")
	}
	if req.KeyType == "" {
		return nil, status.Error(codes.InvalidArgument, "key_type is required")
	}

	ks, err := xkmssdk.ServiceGetBackend(req.Backend)
	if err != nil {
		return nil, status.Error(codes.NotFound, "backend not found")
	}

	keyType := xkmssdk.ParseKeyType(req.KeyType)
	if keyType == 0 {
		keyType = xkmssdk.KeyTypeSigning
	}

	attrs := &xkmssdk.KeyAttributes{
		CN:         req.KeyId,
		KeyType:    keyType,
		StoreType:  xkmssdk.StoreType(req.Backend),
		Hash:       xkmsParseHash(req.Hash),
		Partition:  xkmssdk.Partition(req.Partition),
		Exportable: req.Exportable,
	}

	algorithm := req.Algorithm
	if algorithm == "" {
		algorithm = req.KeyType
	}

	var privKey crypto.PrivateKey
	switch {
	case xkmssdk.AlgorithmRSA.Equals(algorithm):
		keySize := int(req.KeySize)
		if keySize == 0 {
			keySize = xkmssdk.RSAKeySize2048
		}
		attrs.KeyAlgorithm = x509.RSA
		attrs.RSAAttributes = &xkmssdk.RSAAttributes{KeySize: keySize}
		privKey, err = ks.GenerateRSA(attrs)

	case xkmssdk.AlgorithmECDSA.Equals(algorithm):
		curve := req.Curve
		if curve == "" {
			curve = string(xkmssdk.CurveP256)
		}
		attrs.KeyAlgorithm = x509.ECDSA
		parsedCurve, curveErr := xkmssdk.ParseCurve(curve)
		if curveErr != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid curve")
		}
		attrs.ECCAttributes = &xkmssdk.ECCAttributes{Curve: parsedCurve}
		privKey, err = ks.GenerateECDSA(attrs)

	case xkmssdk.AlgorithmEd25519.Equals(algorithm):
		attrs.KeyAlgorithm = x509.Ed25519
		privKey, err = ks.GenerateEd25519(attrs)

	case xkmssdk.AlgorithmSymmetric.Equals(algorithm):
		symBackend, ok := ks.KeyProvider().(xkmssdk.SymmetricKeyProvider)
		if !ok {
			return nil, status.Error(codes.Unimplemented, "backend does not support symmetric key generation")
		}
		keySize := int(req.KeySize)
		symAlgorithm := req.Algorithm
		if symAlgorithm == "" || symAlgorithm == "symmetric" {
			switch keySize {
			case 128:
				symAlgorithm = "aes128-gcm"
			case 192:
				symAlgorithm = "aes192-gcm"
			case 256, 0:
				symAlgorithm = "aes256-gcm"
			default:
				return nil, status.Error(codes.InvalidArgument, "invalid key size for symmetric key")
			}
		}
		attrs.SymmetricAlgorithm = xkmssdk.SymmetricAlgorithm(symAlgorithm)
		attrs.KeyType = xkmssdk.KeyTypeSecret
		_, err = symBackend.GenerateSymmetricKey(attrs)
		if err != nil {
			return nil, status.Error(codes.Internal, "failed to generate symmetric key")
		}
		return &xkmssdk.ProtoGenerateKeyResponse{
			KeyId:     req.KeyId,
			Backend:   req.Backend,
			KeyType:   req.KeyType,
			CreatedAt: timestamppb.Now(),
		}, nil

	default:
		return nil, status.Error(codes.InvalidArgument, "unsupported algorithm")
	}

	if err != nil {
		return nil, status.Error(codes.Internal, "failed to generate key")
	}

	publicKeyPem := xkmsExtractPublicKeyPEM(privKey)

	return &xkmssdk.ProtoGenerateKeyResponse{
		KeyId:        req.KeyId,
		Backend:      req.Backend,
		KeyType:      req.KeyType,
		PublicKeyPem: publicKeyPem,
		CreatedAt:    timestamppb.Now(),
	}, nil
}

// ListKeys lists all keys in the specified backend.
func (s *XKMSServer) ListKeys(ctx context.Context, req *xkmssdk.ProtoListKeysRequest) (*xkmssdk.ProtoListKeysResponse, error) {
	if req.Backend == "" {
		return nil, status.Error(codes.InvalidArgument, "backend is required")
	}

	ks, err := xkmssdk.ServiceGetBackend(req.Backend)
	if err != nil {
		return nil, status.Error(codes.NotFound, "backend not found")
	}

	allAttrs, err := ks.ListKeys()
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list keys")
	}

	offset := int(req.Offset)
	limit := int(req.Limit)
	if limit == 0 {
		limit = 100
	}
	end := offset + limit
	if end > len(allAttrs) {
		end = len(allAttrs)
	}
	if offset > len(allAttrs) {
		offset = len(allAttrs)
	}
	paginatedAttrs := allAttrs[offset:end]

	keys := make([]*xkmssdk.ProtoKeyInfo, 0, len(paginatedAttrs))
	for _, attr := range paginatedAttrs {
		ki := &xkmssdk.ProtoKeyInfo{
			KeyId:     attr.CN,
			Backend:   req.Backend,
			KeyType:   string(attr.KeyType),
			Algorithm: xkmsAlgorithmString(attr),
			Partition: string(attr.Partition),
			CreatedAt: timestamppb.Now(),
		}
		if attr.RSAAttributes != nil {
			ki.KeySize = int32(attr.RSAAttributes.KeySize) // #nosec G115
		}
		if attr.ECCAttributes != nil {
			ki.Curve = xkmssdk.CurveName(attr.ECCAttributes.Curve)
		}
		keys = append(keys, ki)
	}

	return &xkmssdk.ProtoListKeysResponse{
		Keys:  keys,
		Total: int32(len(allAttrs)), // #nosec G115
	}, nil
}

// GetKey retrieves information about a specific key.
func (s *XKMSServer) GetKey(ctx context.Context, req *xkmssdk.ProtoGetKeyRequest) (*xkmssdk.ProtoGetKeyResponse, error) {
	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}
	if req.Backend == "" {
		return nil, status.Error(codes.InvalidArgument, "backend is required")
	}

	ks, err := xkmssdk.ServiceGetBackend(req.Backend)
	if err != nil {
		return nil, status.Error(codes.NotFound, "backend not found")
	}

	attrs, findErr := xkmsFindKeyAttributes(ks, req.KeyId)
	if findErr != nil {
		return nil, status.Error(codes.NotFound, "key not found")
	}

	ki := &xkmssdk.ProtoKeyInfo{
		KeyId:     attrs.CN,
		Backend:   req.Backend,
		KeyType:   string(attrs.KeyType),
		Algorithm: xkmsAlgorithmString(attrs),
		Partition: string(attrs.Partition),
		CreatedAt: timestamppb.Now(),
	}
	if attrs.RSAAttributes != nil {
		ki.KeySize = int32(attrs.RSAAttributes.KeySize) // #nosec G115
	}
	if attrs.ECCAttributes != nil {
		ki.Curve = xkmssdk.CurveName(attrs.ECCAttributes.Curve)
	}

	var publicKeyPem string
	if attrs.KeyType == xkmssdk.KeyTypeSigning {
		signer, sigErr := ks.Signer(attrs)
		if sigErr == nil {
			publicKeyPem = xkmsExtractPublicKeyPEM(signer)
		}
	}

	return &xkmssdk.ProtoGetKeyResponse{
		Key:          ki,
		PublicKeyPem: publicKeyPem,
	}, nil
}

// DeleteKey deletes a key from the backend.
func (s *XKMSServer) DeleteKey(ctx context.Context, req *xkmssdk.ProtoDeleteKeyRequest) (*xkmssdk.ProtoDeleteKeyResponse, error) {
	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}
	if req.Backend == "" {
		return nil, status.Error(codes.InvalidArgument, "backend is required")
	}

	ks, err := xkmssdk.ServiceGetBackend(req.Backend)
	if err != nil {
		return nil, status.Error(codes.NotFound, "backend not found")
	}

	attrs, findErr := xkmsFindKeyAttributes(ks, req.KeyId)
	if findErr != nil {
		return nil, status.Error(codes.NotFound, "key not found")
	}

	if err := ks.DeleteKey(attrs); err != nil {
		return nil, status.Error(codes.Internal, "failed to delete key")
	}

	return &xkmssdk.ProtoDeleteKeyResponse{
		Success: true,
		Message: "key deleted successfully",
	}, nil
}

// RotateKey rotates an existing key.
func (s *XKMSServer) RotateKey(ctx context.Context, req *xkmssdk.ProtoRotateKeyRequest) (*xkmssdk.ProtoRotateKeyResponse, error) {
	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}
	if req.Backend == "" {
		return nil, status.Error(codes.InvalidArgument, "backend is required")
	}

	ks, err := xkmssdk.ServiceGetBackend(req.Backend)
	if err != nil {
		return nil, status.Error(codes.NotFound, "backend not found")
	}

	attrs, findErr := xkmsFindKeyAttributes(ks, req.KeyId)
	if findErr != nil {
		return nil, status.Error(codes.NotFound, "key not found")
	}

	newKey, rotErr := ks.RotateKey(attrs)
	if rotErr != nil {
		return nil, status.Error(codes.Internal, "failed to rotate key")
	}

	return &xkmssdk.ProtoRotateKeyResponse{
		KeyId:        req.KeyId,
		Backend:      req.Backend,
		KeyType:      string(attrs.KeyType),
		PublicKeyPem: xkmsExtractPublicKeyPEM(newKey),
		RotatedAt:    timestamppb.Now(),
	}, nil
}

// Sign signs data with the specified key.
func (s *XKMSServer) Sign(ctx context.Context, req *xkmssdk.ProtoSignRequest) (*xkmssdk.ProtoSignResponse, error) {
	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}
	if req.Backend == "" {
		return nil, status.Error(codes.InvalidArgument, "backend is required")
	}

	ks, err := xkmssdk.ServiceGetBackend(req.Backend)
	if err != nil {
		return nil, status.Error(codes.NotFound, "backend not found")
	}

	attrs, findErr := xkmsFindKeyAttributes(ks, req.KeyId)
	if findErr != nil {
		return nil, status.Error(codes.NotFound, "key not found")
	}

	signer, sigErr := ks.Signer(attrs)
	if sigErr != nil {
		return nil, status.Error(codes.Internal, "failed to get signer")
	}

	if attrs.KeyAlgorithm == x509.Ed25519 {
		signature, signErr := signer.Sign(nil, req.Data, crypto.Hash(0))
		if signErr != nil {
			return nil, status.Error(codes.Internal, "failed to sign")
		}
		return &xkmssdk.ProtoSignResponse{Signature: signature}, nil
	}

	if len(req.Data) == 0 {
		return nil, status.Error(codes.InvalidArgument, "data is required")
	}

	hashAlg := xkmsParseHash(req.Hash)
	hasher := hashAlg.New()
	hasher.Write(req.Data)
	digest := hasher.Sum(nil)

	signature, signErr := signer.Sign(nil, digest, hashAlg)
	if signErr != nil {
		return nil, status.Error(codes.Internal, "failed to sign")
	}

	return &xkmssdk.ProtoSignResponse{Signature: signature}, nil
}

// Verify verifies a signature with the specified key.
func (s *XKMSServer) Verify(ctx context.Context, req *xkmssdk.ProtoVerifyRequest) (*xkmssdk.ProtoVerifyResponse, error) {
	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}
	if req.Backend == "" {
		return nil, status.Error(codes.InvalidArgument, "backend is required")
	}
	if len(req.Signature) == 0 {
		return nil, status.Error(codes.InvalidArgument, "signature is required")
	}

	ks, err := xkmssdk.ServiceGetBackend(req.Backend)
	if err != nil {
		return nil, status.Error(codes.NotFound, "backend not found")
	}

	attrs, findErr := xkmsFindKeyAttributes(ks, req.KeyId)
	if findErr != nil {
		return nil, status.Error(codes.NotFound, "key not found")
	}

	privKey, getErr := ks.GetKey(attrs)
	if getErr != nil {
		return nil, status.Error(codes.Internal, "failed to get key")
	}

	var pubKey crypto.PublicKey
	switch k := privKey.(type) {
	case *rsa.PrivateKey:
		pubKey = &k.PublicKey
	case *ecdsa.PrivateKey:
		pubKey = &k.PublicKey
	case ed25519.PrivateKey:
		pubKey = k.Public()
	case crypto.Signer:
		pubKey = k.Public()
	default:
		return nil, status.Error(codes.Internal, "unsupported key type")
	}

	if edPub, ok := pubKey.(ed25519.PublicKey); ok {
		valid := ed25519.Verify(edPub, req.Data, req.Signature)
		msg := "signature is valid"
		if !valid {
			msg = "signature is invalid"
		}
		return &xkmssdk.ProtoVerifyResponse{Valid: valid, Message: msg}, nil
	}

	if len(req.Data) == 0 {
		return nil, status.Error(codes.InvalidArgument, "data is required")
	}

	hashAlg := xkmsParseHash(req.Hash)
	hasher := hashAlg.New()
	hasher.Write(req.Data)
	digest := hasher.Sum(nil)

	valid := false
	switch pub := pubKey.(type) {
	case *rsa.PublicKey:
		verifyErr := rsa.VerifyPKCS1v15(pub, hashAlg, digest, req.Signature)
		valid = (verifyErr == nil)
	case *ecdsa.PublicKey:
		valid = ecdsa.VerifyASN1(pub, digest, req.Signature)
	}

	msg := "signature is valid"
	if !valid {
		msg = "signature is invalid"
	}
	return &xkmssdk.ProtoVerifyResponse{Valid: valid, Message: msg}, nil
}

// Encrypt encrypts data with the specified key (symmetric encryption).
func (s *XKMSServer) Encrypt(ctx context.Context, req *xkmssdk.ProtoEncryptRequest) (*xkmssdk.ProtoEncryptResponse, error) {
	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}
	if req.Backend == "" {
		return nil, status.Error(codes.InvalidArgument, "backend is required")
	}
	if len(req.Plaintext) == 0 {
		return nil, status.Error(codes.InvalidArgument, "plaintext is required")
	}

	ks, err := xkmssdk.ServiceGetBackend(req.Backend)
	if err != nil {
		return nil, status.Error(codes.NotFound, "backend not found")
	}

	symBackend, ok := ks.KeyProvider().(xkmssdk.SymmetricKeyProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "backend does not support symmetric encryption")
	}

	attrs, findErr := xkmsFindKeyAttributes(ks, req.KeyId)
	if findErr != nil {
		return nil, status.Error(codes.NotFound, "key not found")
	}

	encrypter, encErr := symBackend.SymmetricEncrypter(attrs)
	if encErr != nil {
		return nil, status.Error(codes.Internal, "failed to get symmetric encrypter")
	}

	opts := &xkmssdk.EncryptOptions{
		AdditionalData: req.AdditionalData,
	}

	result, encryptErr := encrypter.Encrypt(req.Plaintext, opts)
	if encryptErr != nil {
		return nil, status.Error(codes.Internal, "failed to encrypt")
	}

	return &xkmssdk.ProtoEncryptResponse{
		Ciphertext: result.Ciphertext,
		Nonce:      result.Nonce,
		Tag:        result.Tag,
	}, nil
}

// Decrypt decrypts data with the specified key.
func (s *XKMSServer) Decrypt(ctx context.Context, req *xkmssdk.ProtoDecryptRequest) (*xkmssdk.ProtoDecryptResponse, error) {
	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}
	if req.Backend == "" {
		return nil, status.Error(codes.InvalidArgument, "backend is required")
	}
	if len(req.Ciphertext) == 0 {
		return nil, status.Error(codes.InvalidArgument, "ciphertext is required")
	}

	ks, err := xkmssdk.ServiceGetBackend(req.Backend)
	if err != nil {
		return nil, status.Error(codes.NotFound, "backend not found")
	}

	attrs, findErr := xkmsFindKeyAttributes(ks, req.KeyId)
	if findErr != nil {
		return nil, status.Error(codes.NotFound, "key not found")
	}

	var plaintext []byte

	if attrs.IsSymmetric() {
		symBackend, ok := ks.KeyProvider().(xkmssdk.SymmetricKeyProvider)
		if !ok {
			return nil, status.Error(codes.Unimplemented, "backend does not support symmetric decryption")
		}

		encrypter, encErr := symBackend.SymmetricEncrypter(attrs)
		if encErr != nil {
			return nil, status.Error(codes.Internal, "failed to get symmetric encrypter")
		}

		encryptedData := &xkmssdk.EncryptedData{
			Ciphertext: req.Ciphertext,
			Nonce:      req.Nonce,
			Tag:        req.Tag,
		}

		decryptOpts := &xkmssdk.DecryptOptions{
			AdditionalData: req.AdditionalData,
		}

		plaintext, err = encrypter.Decrypt(encryptedData, decryptOpts)
		if err != nil {
			return nil, status.Error(codes.Internal, "failed to decrypt")
		}
	} else {
		decrypter, decErr := ks.Decrypter(attrs)
		if decErr != nil {
			return nil, status.Error(codes.Internal, "failed to get decrypter")
		}

		plaintext, err = decrypter.Decrypt(nil, req.Ciphertext, nil)
		if err != nil {
			return nil, status.Error(codes.Internal, "failed to decrypt")
		}
	}

	return &xkmssdk.ProtoDecryptResponse{Plaintext: plaintext}, nil
}

// SaveCert stores a certificate.
func (s *XKMSServer) SaveCert(ctx context.Context, req *xkmssdk.ProtoSaveCertRequest) (*xkmssdk.ProtoSaveCertResponse, error) {
	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}
	if req.CertPem == "" {
		return nil, status.Error(codes.InvalidArgument, "cert_pem is required")
	}

	backends := xkmssdk.ServiceBackends()
	if len(backends) == 0 {
		return nil, status.Error(codes.Unavailable, "no backends available")
	}

	ks, err := xkmssdk.ServiceGetBackend(backends[0])
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get backend")
	}

	block, _ := pem.Decode([]byte(req.CertPem))
	if block == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid PEM certificate")
	}

	cert, parseErr := x509.ParseCertificate(block.Bytes)
	if parseErr != nil {
		return nil, status.Error(codes.InvalidArgument, "failed to parse certificate")
	}

	if err := ks.SaveCert(req.KeyId, cert); err != nil {
		return nil, status.Error(codes.Internal, "failed to save certificate")
	}

	return &xkmssdk.ProtoSaveCertResponse{
		Success: true,
		Message: "certificate saved",
	}, nil
}

// GetCert retrieves a certificate.
func (s *XKMSServer) GetCert(ctx context.Context, req *xkmssdk.ProtoGetCertRequest) (*xkmssdk.ProtoGetCertResponse, error) {
	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	backends := xkmssdk.ServiceBackends()
	if len(backends) == 0 {
		return nil, status.Error(codes.Unavailable, "no backends available")
	}

	ks, err := xkmssdk.ServiceGetBackend(backends[0])
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get backend")
	}

	cert, getErr := ks.GetCert(req.KeyId)
	if getErr != nil {
		return nil, status.Error(codes.NotFound, "certificate not found")
	}

	certPem := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	})

	return &xkmssdk.ProtoGetCertResponse{CertPem: string(certPem)}, nil
}

// DeleteCert removes a certificate.
func (s *XKMSServer) DeleteCert(ctx context.Context, req *xkmssdk.ProtoDeleteCertRequest) (*xkmssdk.ProtoDeleteCertResponse, error) {
	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	backends := xkmssdk.ServiceBackends()
	if len(backends) == 0 {
		return nil, status.Error(codes.Unavailable, "no backends available")
	}

	ks, err := xkmssdk.ServiceGetBackend(backends[0])
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get backend")
	}

	if err := ks.DeleteCert(req.KeyId); err != nil {
		return nil, status.Error(codes.Internal, "failed to delete certificate")
	}

	return &xkmssdk.ProtoDeleteCertResponse{
		Success: true,
		Message: "certificate deleted",
	}, nil
}

// ListCerts lists all certificates.
func (s *XKMSServer) ListCerts(ctx context.Context, req *xkmssdk.ProtoListCertsRequest) (*xkmssdk.ProtoListCertsResponse, error) {
	backends := xkmssdk.ServiceBackends()
	if len(backends) == 0 {
		return nil, status.Error(codes.Unavailable, "no backends available")
	}

	ks, err := xkmssdk.ServiceGetBackend(backends[0])
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get backend")
	}

	keyIDs, listErr := ks.ListCerts()
	if listErr != nil {
		return nil, status.Error(codes.Internal, "failed to list certificates")
	}

	return &xkmssdk.ProtoListCertsResponse{
		KeyIds: keyIDs,
		Total:  int32(len(keyIDs)), // #nosec G115
	}, nil
}

// Seal encrypts data using the backend's sealing mechanism.
func (s *XKMSServer) Seal(ctx context.Context, req *xkmssdk.ProtoSealRequest) (*xkmssdk.ProtoSealResponse, error) {
	if req.Backend == "" {
		return nil, status.Error(codes.InvalidArgument, "backend is required")
	}
	if len(req.Data) == 0 {
		return nil, status.Error(codes.InvalidArgument, "data is required")
	}

	opts := &xkmssdk.SealOptions{
		AAD: req.Aad,
	}

	if req.KeyId != "" {
		ks, backErr := xkmssdk.ServiceGetBackend(req.Backend)
		if backErr != nil {
			return nil, status.Error(codes.NotFound, "backend not found")
		}
		attrs, findErr := xkmsFindKeyAttributes(ks, req.KeyId)
		if findErr != nil {
			return nil, status.Error(codes.NotFound, "key not found")
		}
		opts.KeyAttributes = attrs
	}

	sealed, sealErr := xkmssdk.ServiceSealWithBackend(ctx, req.Backend, req.Data, opts)
	if sealErr != nil {
		return nil, status.Error(codes.Internal, "failed to seal data")
	}

	metadata := make(map[string]string, len(sealed.Metadata))
	for k, v := range sealed.Metadata {
		metadata[k] = base64.StdEncoding.EncodeToString(v)
	}

	return &xkmssdk.ProtoSealResponse{
		Backend:    string(sealed.Backend),
		Ciphertext: sealed.Ciphertext,
		Nonce:      sealed.Nonce,
		Tag:        sealed.Tag,
		Metadata:   metadata,
	}, nil
}

// Unseal decrypts previously sealed data.
func (s *XKMSServer) Unseal(ctx context.Context, req *xkmssdk.ProtoUnsealRequest) (*xkmssdk.ProtoUnsealResponse, error) {
	if req.Backend == "" {
		return nil, status.Error(codes.InvalidArgument, "backend is required")
	}
	if len(req.Ciphertext) == 0 {
		return nil, status.Error(codes.InvalidArgument, "ciphertext is required")
	}

	ks, backErr := xkmssdk.ServiceGetBackend(req.Backend)
	if backErr != nil {
		return nil, status.Error(codes.NotFound, "backend not found")
	}

	sealed := &xkmssdk.SealedData{
		Backend:    ks.KeyProvider().Type(),
		Ciphertext: req.Ciphertext,
		Nonce:      req.Nonce,
		Tag:        req.Tag,
	}

	opts := &xkmssdk.UnsealOptions{
		AAD: req.Aad,
	}

	if req.KeyId != "" {
		attrs, findErr := xkmsFindKeyAttributes(ks, req.KeyId)
		if findErr != nil {
			return nil, status.Error(codes.NotFound, "key not found")
		}
		opts.KeyAttributes = attrs
		sealed.KeyID = attrs.ID()
	}

	plaintext, unsealErr := xkmssdk.ServiceUnsealWithBackend(ctx, req.Backend, sealed, opts)
	if unsealErr != nil {
		return nil, status.Error(codes.Internal, "failed to unseal data")
	}

	return &xkmssdk.ProtoUnsealResponse{Plaintext: plaintext}, nil
}

// CanSeal checks if a backend supports sealing operations.
func (s *XKMSServer) CanSeal(ctx context.Context, req *xkmssdk.ProtoCanSealRequest) (*xkmssdk.ProtoCanSealResponse, error) {
	if req.Backend != "" {
		return &xkmssdk.ProtoCanSealResponse{CanSeal: xkmssdk.ServiceCanSeal(req.Backend)}, nil
	}
	return &xkmssdk.ProtoCanSealResponse{CanSeal: xkmssdk.ServiceCanSeal()}, nil
}

// BarrierStatus returns the current state of the barrier.
func (s *XKMSServer) BarrierStatus(ctx context.Context, req *emptypb.Empty) (*xkmssdk.ProtoBarrierStatusResponse, error) {
	return nil, status.Error(codes.Unimplemented, "barrier operations not available via QuicBaaS xkms bridge")
}

// BarrierInitialize initializes the barrier.
func (s *XKMSServer) BarrierInitialize(ctx context.Context, req *xkmssdk.ProtoBarrierInitializeRequest) (*emptypb.Empty, error) {
	return nil, status.Error(codes.Unimplemented, "barrier operations not available via QuicBaaS xkms bridge")
}

// BarrierUnseal unseals the barrier.
func (s *XKMSServer) BarrierUnseal(ctx context.Context, req *xkmssdk.ProtoBarrierUnsealRequest) (*emptypb.Empty, error) {
	return nil, status.Error(codes.Unimplemented, "barrier operations not available via QuicBaaS xkms bridge")
}

// BarrierSeal seals the barrier.
func (s *XKMSServer) BarrierSeal(ctx context.Context, req *emptypb.Empty) (*emptypb.Empty, error) {
	return nil, status.Error(codes.Unimplemented, "barrier operations not available via QuicBaaS xkms bridge")
}

// xkmsBackendDescription returns a human-readable description for a backend type.
func xkmsBackendDescription(bt xkmssdk.BackendType) string {
	descriptions := map[xkmssdk.BackendType]string{
		xkmssdk.BackendTypeSoftware:  "Software-based key storage",
		xkmssdk.BackendTypePKCS11:    "Hardware Security Module (PKCS#11)",
		xkmssdk.BackendTypeTPM2:      "Trusted Platform Module 2.0",
		xkmssdk.BackendTypeAWSKMS:    "AWS Key Management Service",
		xkmssdk.BackendTypeGCPKMS:    "Google Cloud Key Management Service",
		xkmssdk.BackendTypeAzureKV:   "Azure Key Vault",
		xkmssdk.BackendTypeVault:     "HashiCorp Vault",
		xkmssdk.BackendTypeSymmetric: "Symmetric key storage",
	}
	if desc, ok := descriptions[bt]; ok {
		return desc
	}
	return string(bt)
}

// xkmsParseHash maps a hash algorithm name to a crypto.Hash.
func xkmsParseHash(name string) crypto.Hash {
	switch name {
	case "sha256", "SHA-256", "SHA256":
		return crypto.SHA256
	case "sha384", "SHA-384", "SHA384":
		return crypto.SHA384
	case "sha512", "SHA-512", "SHA512":
		return crypto.SHA512
	default:
		return crypto.SHA256
	}
}

// xkmsAlgorithmString returns the algorithm string for key attributes.
func xkmsAlgorithmString(attr *xkmssdk.KeyAttributes) string {
	switch attr.KeyAlgorithm {
	case x509.RSA:
		return "RSA"
	case x509.ECDSA:
		return "ECDSA"
	case x509.Ed25519:
		return "Ed25519"
	default:
		if attr.SymmetricAlgorithm != "" {
			return string(attr.SymmetricAlgorithm)
		}
		return string(attr.KeyType)
	}
}

// xkmsExtractPublicKeyPEM extracts the PEM-encoded public key from a private key
// or a crypto.Signer. Returns empty string on failure.
func xkmsExtractPublicKeyPEM(key interface{}) string {
	var pubKey crypto.PublicKey
	switch k := key.(type) {
	case *rsa.PrivateKey:
		pubKey = &k.PublicKey
	case *ecdsa.PrivateKey:
		pubKey = &k.PublicKey
	case ed25519.PrivateKey:
		pubKey = k.Public()
	case crypto.Signer:
		pubKey = k.Public()
	default:
		return ""
	}

	pubKeyBytes, err := x509.MarshalPKIXPublicKey(pubKey)
	if err != nil {
		return ""
	}
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubKeyBytes,
	}))
}

// xkmsFindKeyAttributes searches a backend for a key by its CN (key ID).
func xkmsFindKeyAttributes(ks xkmssdk.ServiceBackend, keyID string) (*xkmssdk.KeyAttributes, error) {
	attrs, err := ks.ListKeys()
	if err != nil {
		return nil, err
	}
	for _, attr := range attrs {
		if attr.CN == keyID {
			return attr, nil
		}
	}
	return nil, &XKMSKeyNotFoundError{KeyID: keyID}
}

// XKMSKeyNotFoundError is returned when a key cannot be found in a backend.
type XKMSKeyNotFoundError struct {
	KeyID string
}

// Error implements the error interface.
func (e *XKMSKeyNotFoundError) Error() string {
	return "xkms: key not found: " + e.KeyID
}
