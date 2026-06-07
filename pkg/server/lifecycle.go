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

package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-qrdb/pkg/atrest"
	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/backup"
	_ "github.com/jeremyhahn/go-quicbaas/pkg/barrier" // side-effect import: registers barrier package init
	"github.com/jeremyhahn/go-quicbaas/pkg/cache"
	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/cluster"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/crypto"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/routing"
	quicbaasseal "github.com/jeremyhahn/go-quicbaas/pkg/seal"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"

	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
	qbstorage "github.com/jeremyhahn/go-quicbaas/pkg/storage"
	filestorage "github.com/jeremyhahn/go-quicbaas/pkg/storage/file"
	"github.com/jeremyhahn/go-quicbaas/pkg/watchbridge"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// Seam injection points for unit testing.
// NOTE: REST, QUIC, MCP, and Vector seams are defined in their respective
// conditional compilation files (lifecycle_rest.go, lifecycle_quic.go, etc.).
var (
	seamMu sync.Mutex // Protects seam variables during testing

	listenFunc = net.Listen
	newGRPCAPI = grpcapi.NewServer
	// additional seams for unit tests.
	grpcStopTimeout  = 10 * time.Second
	grpcGracefulStop = func(s *ggrpc.Server) { s.GracefulStop() }
	grpcForceStop    = func(s *ggrpc.Server) { s.Stop() }
	// Seams for backup, secrets, catalog, and worker constructors.
	newSecretsMgr          = secrets.NewManager
	newCatalogStore        = catalog.NewPebbleStorage
	newCatalogMgr          = catalog.NewCatalogManager
	newBackendMgr          = catalog.NewBackendManager
	newReplicationWorkerFn = catalog.NewReplicationWorker
	newArchiveWorkerFn     = catalog.NewArchiveWorker
	newVerifyWorkerFn      = catalog.NewVerifyWorker
	newRetentionWorkerFn   = catalog.NewRetentionWorker
	closeBackupMgrFn       = func(m *backup.BackupManager) error { return m.Close() }
	// Seams for KMS constructor functions in startKMS.
	newKMSKeyStorage   = xkmssdk.NewMemoryBackend
	newSoftwareBackend = xkmssdk.NewSoftwareBackend
	newXKMSKeystore    = func(cfg *xkmssdk.ServiceBackendConfig) (xkmssdk.ServiceBackend, error) {
		return xkmssdk.NewServiceBackend(cfg)
	}
	initXKMS             = dbxkms.Initialize
	newKMSBackendFactory = kms.NewBackendFactory
	newKMSServiceFn      = kms.NewKMSService
	// Seam for cache.NewManager in startService.
	newCacheManagerFn = cache.NewManager
	// Seam for grpcapi.InitializeDefaultSealManager in startGRPCServer.
	initSealManagerFn = grpcapi.InitializeDefaultSealManager
	// Seam for watchbridge.New in startWatchBridge.
	newWatchBridgeFn = watchbridge.NewWatchBridge
	// Seams for IAM init, reencrypt path, routing, and worker management.
	initIAMComponentsFn      = func(s *Server) (service.IAMComponents, error) { return s.initializeIAMComponents() }
	newKMSDEKProviderFn      = catalog.NewKMSDEKProvider
	newEncryptionManagerFn   = catalog.NewEncryptionManager
	newReencryptWorkerSeamFn = catalog.NewReencryptWorker
	validateRoutingFn        = validateRouting
	initFromXKMSFn           = func(factory *kms.BackendFactory, ctx context.Context, backends []config.BackendType) error {
		return factory.InitializeFromXKMS(ctx, backends)
	}
	workerMgrRegisterFn = func(mgr *catalog.WorkerManager, w catalog.Worker) error { return mgr.Register(w) }
	workerMgrStartAllFn = func(mgr *catalog.WorkerManager, ctx context.Context) error { return mgr.StartAll(ctx) }
	// Seams for messaging push service start and watch bridge start.
	pushServiceStartFn = func(ps *messaging.PushService, ctx context.Context) error { return ps.Start(ctx) }
	watchBridgeStartFn = func(b *watchbridge.WatchBridge, ctx context.Context) error { return b.Start(ctx) }
	// Seam for CA construction.
	newCAFn = xkmssdk.NewTrustedCA
	// Seam for seal manager construction.
	newSealManagerFn = defaultNewSealManager
	// Seam for CA initialization (allows tests to bypass full CA setup).
	initializeCAFn = func(s *Server) error { return s.initializeCA() }
)

// initializeCA initializes the Certificate Authority for TLS.
// On first startup it creates a root CA and issues a server certificate.
// On subsequent startups it loads the existing CA from disk.
func (s *Server) initializeCA() error {
	// Skip CA initialization when TLS is disabled
	if !s.config.Security.TLSEnabled {
		return nil
	}

	// When the caller provides an external CA, skip internal CA bootstrap.
	// The external CA is responsible for having completed Load() or Init()
	// before being passed here.
	if s.opts.externalCA != nil {
		s.xkmsCA = s.opts.externalCA
		s.logger.Info("Using external certificate authority — skipping internal CA bootstrap")
		return s.issueTLSCertFromExternalCA()
	}

	// Apply CA config defaults
	if s.config.CA.RootCommonName == "" {
		s.config.CA.RootCommonName = "QuicBaaS Root CA"
	}
	if s.config.CA.RootOrganization == "" {
		s.config.CA.RootOrganization = "QuicBaaS"
	}
	if s.config.CA.RootValidYears == 0 {
		s.config.CA.RootValidYears = 10
	}
	if s.config.CA.DefaultValidityDays == 0 {
		s.config.CA.DefaultValidityDays = 365
	}

	// Determine directories
	dataDir := s.config.Storage.DataDir
	keysDir := filepath.Join(dataDir, "keys")
	if s.config.CA.KeysDir != "" {
		keysDir = s.config.CA.KeysDir
	}
	certsDir := filepath.Join(dataDir, "certs")

	// Create file-based storage backends.
	// The resulting keyStorage is also stored on the Server so that startKMS
	// can reuse it instead of creating a parallel key store under a different
	// directory. One storage root = one KEK universe = no wrap/unwrap mismatch.
	keyStorage, err := xkmssdk.NewFileStorageBackend(keysDir)
	if err != nil {
		return api.NewOperationFailedError("create key storage", err)
	}
	s.xkmsKeyStorage = keyStorage
	certFileStorage, err := xkmssdk.NewFileStorageBackend(certsDir)
	if err != nil {
		return api.NewOperationFailedError("create cert storage", err)
	}

	// Create PKCS#8 backend for key operations
	pkcs8Backend, err := xkmssdk.NewPKCS8Backend(&xkmssdk.PKCS8BackendConfig{KeyStorage: keyStorage})
	if err != nil {
		return api.NewOperationFailedError("create PKCS#8 backend", err)
	}

	// Create unified xkms backend
	xkmsBackend, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{
		Backend:     pkcs8Backend,
		CertStorage: certFileStorage,
	})
	if err != nil {
		return api.NewOperationFailedError("create xkms backend", err)
	}

	// Create certificate store
	certAdapter := xkmssdk.NewStorageCertAdapter(certFileStorage)
	cs, err := xkmssdk.NewCertStore(&xkmssdk.CertStoreConfig{
		CertStorage: certAdapter,
	})
	if err != nil {
		return api.NewOperationFailedError("create cert store", err)
	}

	// Build key config from QuicBaaS config
	keyAlgo := xkmssdk.AlgorithmECDSA
	if s.config.CA.RootKeyAlgorithm == "RSA" {
		keyAlgo = xkmssdk.AlgorithmRSA
	}

	keyConfig := &xkmssdk.KeyConfig{
		KeyAlgorithm:       keyAlgo,
		KeyType:            xkmssdk.KeyTypeStringCA,
		Hash:               xkmssdk.HashSHA256,
		SignatureAlgorithm: xkmssdk.SigECDSAWithSHA256,
		StoreType:          xkmssdk.StoreSoftware,
		ECCConfig:          &xkmssdk.KeyECCConfig{Curve: xkmssdk.CurveP256},
	}

	// Handle RSA vs ECDSA config
	if keyAlgo == xkmssdk.AlgorithmRSA {
		keyConfig.RSAConfig = &xkmssdk.KeyRSAConfig{KeySize: 2048}
		keyConfig.SignatureAlgorithm = xkmssdk.SigSHA256WithRSA
		keyConfig.ECCConfig = nil
	} else {
		// Handle curve selection
		switch s.config.CA.RootKeyCurve {
		case "P-384":
			keyConfig.ECCConfig = &xkmssdk.KeyECCConfig{Curve: xkmssdk.CurveP384}
			keyConfig.SignatureAlgorithm = xkmssdk.SigECDSAWithSHA384
		case "P-521":
			keyConfig.ECCConfig = &xkmssdk.KeyECCConfig{Curve: xkmssdk.CurveP521}
			keyConfig.SignatureAlgorithm = xkmssdk.SigECDSAWithSHA512
		}
	}

	// Build CA config with unified Identity-based hierarchy
	caConfig := &xkmssdk.TrustedCAMultiIdentityConfig{
		Identity: []xkmssdk.TrustedCAIdentity{
			{
				Subject: xkmssdk.TrustedCASubject{
					CommonName:   s.config.CA.RootCommonName,
					Organization: s.config.CA.RootOrganization,
				},
				Valid:        s.config.CA.RootValidYears,
				Keys:         []*xkmssdk.KeyConfig{keyConfig},
				KeystoreType: xkmssdk.StoreSoftware,
				IsRoot:       true,
			},
		},
		SelectedCA:          0,
		DefaultValidityDays: s.config.CA.DefaultValidityDays,
		HomeDir:             dataDir,
	}

	// Create CA - derive KeyAttributes and CAConfig from the identity
	issuingIdentity := caConfig.IssuingIdentity()
	keyAttrs, err := issuingIdentity.ToKeyAttributes()
	if err != nil {
		return api.NewOperationFailedError("derive CA key attributes", err)
	}
	caCAConfig := &xkmssdk.TrustedCAConfig{
		Identity:                issuingIdentity.Subject.CommonName,
		Subject:                 &issuingIdentity.Subject,
		ValidityDays:            issuingIdentity.GetValidityDays(),
		IsRootCA:                issuingIdentity.IsRoot,
		MaxPathLength:           1,
		StoreType:               issuingIdentity.KeystoreType,
		DefaultCertValidityDays: caConfig.GetDefaultValidityDays(),
	}
	params := &xkmssdk.TrustedCAParams{
		Config:        caCAConfig,
		KeyAttributes: keyAttrs,
		KeyStore:      xkmsBackend,
		CertStore:     cs,
	}

	xkmsCA, err := newCAFn(params)
	if err != nil {
		return api.NewOperationFailedError("create certificate authority", err)
	}

	// Try Load first; if not initialized, Init instead
	if loadErr := xkmsCA.Load(); loadErr != nil {
		// Load failed - try initializing from scratch
		if initErr := xkmsCA.Init(); initErr != nil {
			return api.NewOperationFailedError("initialize certificate authority", initErr)
		}
		s.logger.Info("Certificate authority initialized with new keys and certificates")
	} else {
		s.logger.Info("Certificate authority loaded from existing files")
	}

	s.xkmsCA = xkmsCA

	// Update config paths for TLS transports unless the operator has already
	// configured them (via security.tls_cert_file / tls_key_file / tls_ca_file
	// in config or QUICBAAS_SECURITY_TLS_*_FILE env vars). This lets
	// deployments that mount their own CA (e.g. integration tests sharing a
	// single CA across all nodes) bypass the auto-generated per-node CA path
	// while keeping the default behaviour intact for plain installs.
	if s.config.Security.TLSCertFile == "" {
		s.config.Security.TLSCertFile = filepath.Join(certsDir, "server.crt")
	}
	if s.config.Security.TLSKeyFile == "" {
		s.config.Security.TLSKeyFile = filepath.Join(certsDir, "server.key")
	}
	if s.config.Security.TLSCAFile == "" {
		s.config.Security.TLSCAFile = filepath.Join(certsDir, "ca-chain.crt")
	}
	s.config.Security.TLSEnabled = true

	// If the operator has provided an explicit server cert and key on disk
	// (e.g. an integration cluster mounting a shared CA across all nodes,
	// or a production deployment with externally-managed certs), load them
	// directly and skip the per-boot CA-issued server cert path. This makes
	// the listener serve the operator's cert chain instead of a freshly-
	// issued, per-node cert that no external client can trust.
	if s.config.Security.TLSCertFile != "" && s.config.Security.TLSKeyFile != "" {
		if _, certErr := os.Stat(s.config.Security.TLSCertFile); certErr == nil {
			if _, keyErr := os.Stat(s.config.Security.TLSKeyFile); keyErr == nil {
				pair, loadErr := tls.LoadX509KeyPair(s.config.Security.TLSCertFile, s.config.Security.TLSKeyFile)
				if loadErr != nil {
					return api.NewOperationFailedError("load operator server cert/key", loadErr)
				}
				s.tlsConfig = &tls.Config{
					Certificates: []tls.Certificate{pair},
					MinVersion:   tls.VersionTLS13,
					NextProtos:   []string{"h2", "http/1.1"},
				}
				// Wire the operator-provided CA for optional mTLS so per-route
				// client-cert validation still works with externally-managed
				// chains. Full-mTLS clusters override this in the REST server.
				if s.config.Security.TLSCAFile != "" {
					if caBytes, caErr := os.ReadFile(s.config.Security.TLSCAFile); caErr == nil {
						pool := x509.NewCertPool()
						if pool.AppendCertsFromPEM(caBytes) {
							s.tlsConfig.ClientCAs = pool
							if !s.config.Security.TLSClientAuth {
								s.tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
							}
						}
					}
				}
				s.logger.Infof("TLS configured from operator-provided cert: %s", s.config.Security.TLSCertFile)
				return nil
			}
		}
	}

	// Issue a server TLS certificate - CA generates the key through its keystore backend
	dnsNames := []string{"localhost"}
	ipStrings := []string{"127.0.0.1", "::1"}
	certCN := s.config.Server.Host
	if certCN == "" || certCN == "0.0.0.0" {
		certCN = "localhost"
	}
	// When Host is a routable hostname or IP, include it in the SANs so
	// remote callers can verify this server. Parse as IP to route into the
	// correct SAN field rather than mixing IPs into the DNSNames list.
	if s.config.Server.Host != "" && s.config.Server.Host != "0.0.0.0" {
		if ip := net.ParseIP(s.config.Server.Host); ip != nil {
			ipStrings = append(ipStrings, s.config.Server.Host)
		} else {
			dnsNames = append(dnsNames, s.config.Server.Host)
		}
	}
	// Include the machine hostname so peers can verify this node's certificate
	if hostname, err := os.Hostname(); err == nil && hostname != "" && hostname != "localhost" {
		dnsNames = append(dnsNames, hostname)
	}
	// Operator-supplied additional SANs — required when remote agents
	// reach the central by an IP or FQDN that differs from Host (e.g.,
	// a GPU host dialing the central across a LAN).
	dnsNames = append(dnsNames, s.config.Server.AdditionalSANDNS...)
	ipStrings = append(ipStrings, s.config.Server.AdditionalSANIPs...)

	serverRequest := &xkmssdk.TrustedCACertificateRequest{
		Subject: xkmssdk.TrustedCASubject{
			CommonName:   certCN,
			Organization: s.config.CA.RootOrganization,
		},
		SANS: &xkmssdk.TrustedCASubjectAlternativeNames{
			DNS: dnsNames,
			IPs: ipStrings,
		},
		Valid: s.config.CA.DefaultValidityDays,
	}

	issuedCert, err := xkmsCA.IssueCertificate(serverRequest)
	if err != nil && !errors.Is(err, xkmssdk.ErrCertificateAlreadyExists) {
		return api.NewOperationFailedError("issue server TLS certificate", err)
	}

	// Build server key attributes matching what the CA generated
	keyConfig.KeyType = xkmssdk.KeyTypeStringTLS
	serverKeyAttrs, err := xkmssdk.KeyAttributesFromConfig(keyConfig)
	if err != nil {
		return api.NewOperationFailedError("parse server key config", err)
	}
	serverKeyAttrs.CN = certCN

	// Get in-memory TLS config backed by OpaqueKey. ServerTLSConfig
	// (not bare TLSConfig) makes server intent explicit and silences
	// the false-positive "ServerName not specified" WARN.
	s.tlsConfig, err = xkmsCA.ServerTLSConfig(serverKeyAttrs, false)
	if err != nil {
		return api.NewOperationFailedError("create server TLS config", err)
	}
	// Enforce TLS 1.3 minimum
	s.tlsConfig.MinVersion = tls.VersionTLS13
	// Enable optional mTLS so per-route handlers (e.g. the agent
	// register endpoint) can validate a presented client cert while
	// browser-origin requests proceed without one. Full-mTLS clusters
	// set TLSClientAuth=true; in that branch the REST server's own
	// buildTLSConfig path takes over and promotes to RequireAndVerify.
	if !s.config.Security.TLSClientAuth {
		if caBundle, caErr := xkmsCA.CABundle(); caErr == nil && len(caBundle) > 0 {
			pool := x509.NewCertPool()
			if pool.AppendCertsFromPEM(caBundle) {
				s.tlsConfig.ClientCAs = pool
				s.tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
				s.logger.Info("TLS configured with optional mTLS (per-route client cert validation)")
			}
		}
	}

	// Advertise gRPC ALPN (h2) on the in-memory TLS config so gRPC
	// clients (e.g. the agent event forwarder's PushEvents stream)
	// negotiate HTTP/2 cleanly. http/1.1 is appended for clients that
	// dial the same TLS config without HTTP/2 support. The REST listener
	// clones this config and overrides NextProtos for its own dispatch.
	s.tlsConfig.NextProtos = []string{"h2", "http/1.1"}

	// Save server cert and CA chain to disk (skip if cert already existed)
	if issuedCert != nil {
		if err := saveServerCert(issuedCert, s.config.Security.TLSCertFile); err != nil {
			return err
		}

		caBundle, err := xkmsCA.CABundle()
		if err != nil {
			return api.NewOperationFailedError("get CA bundle", err)
		}
		if err := os.WriteFile(s.config.Security.TLSCAFile, caBundle, 0600); err != nil {
			return api.NewDataDirOperationError("server.initializeCA", "write CA chain", s.config.Security.TLSCAFile, err)
		}
	}

	// Export key from keystore's file storage for TLS transports.
	if !xkmsBackend.KeyProvider().Capabilities().Export {
		return &qberrors.QBError{
			Code: qberrors.ErrConfiguration,
			Op:   "server.initializeCA",
			Err:  &api.ConfigurationError{Action: "TLS requires an exportable key backend (software/PKCS#8)"},
		}
	}
	// Read key DER from the file storage backend and write as PEM.
	keyDER, err := xkmssdk.StorageGetKey(context.Background(), keyStorage, serverKeyAttrs.ID())
	if err != nil {
		return api.NewOperationFailedError("export server key", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	// #nosec G306 - Key file permissions are restrictive (0600)
	if err := os.WriteFile(s.config.Security.TLSKeyFile, keyPEM, 0600); err != nil {
		return api.NewDataDirOperationError("server.initializeCA", "write server key", s.config.Security.TLSKeyFile, err)
	}

	s.logger.WithFields(map[string]interface{}{
		"ca_cert":     s.config.Security.TLSCAFile,
		"server_cert": s.config.Security.TLSCertFile,
		"certs_dir":   certsDir,
	}).Info("TLS certificates ready")

	return nil
}

// issueTLSCertFromExternalCA configures TLS using the externally injected CA.
// If a pre-issued TLS certificate was provided via WithTLSCertificate, it is
// used directly. Otherwise a new server certificate is issued through the
// external CA following the same SAN/CN logic as internal CA issuance.
func (s *Server) issueTLSCertFromExternalCA() error {
	// If a pre-issued certificate was provided, use it directly
	if s.opts.tlsCertificate != nil {
		s.logger.Info("Using pre-issued TLS certificate — skipping certificate issuance")
		s.tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{*s.opts.tlsCertificate},
			MinVersion:   tls.VersionTLS13,
			NextProtos:   []string{"h2", "http/1.1"},
		}
		return nil
	}

	// Issue a server TLS certificate through the external CA
	s.logger.Info("Issuing server TLS certificate from external CA")

	dnsNames := []string{"localhost"}
	certCN := s.config.Server.Host
	if certCN == "" || certCN == "0.0.0.0" {
		certCN = "localhost"
	}
	if s.config.Server.Host != "" && s.config.Server.Host != "0.0.0.0" {
		dnsNames = append(dnsNames, s.config.Server.Host)
	}
	if hostname, err := os.Hostname(); err == nil && hostname != "" && hostname != "localhost" {
		dnsNames = append(dnsNames, hostname)
	}
	ipStrings := []string{"127.0.0.1", "::1"}

	serverRequest := &xkmssdk.TrustedCACertificateRequest{
		Subject: xkmssdk.TrustedCASubject{
			CommonName:   certCN,
			Organization: s.config.CA.RootOrganization,
		},
		SANS: &xkmssdk.TrustedCASubjectAlternativeNames{
			DNS: dnsNames,
			IPs: ipStrings,
		},
		Valid: s.config.CA.DefaultValidityDays,
	}

	_, err := s.xkmsCA.IssueCertificate(serverRequest)
	if err != nil && !errors.Is(err, xkmssdk.ErrCertificateAlreadyExists) {
		return api.NewOperationFailedError("issue server TLS certificate from external CA", err)
	}

	// Build key attributes for TLSConfig lookup
	keyAlgo := xkmssdk.AlgorithmECDSA
	if s.config.CA.RootKeyAlgorithm == "RSA" {
		keyAlgo = xkmssdk.AlgorithmRSA
	}
	keyConfig := &xkmssdk.KeyConfig{
		KeyAlgorithm:       keyAlgo,
		KeyType:            xkmssdk.KeyTypeStringTLS,
		Hash:               xkmssdk.HashSHA256,
		SignatureAlgorithm: xkmssdk.SigECDSAWithSHA256,
		StoreType:          xkmssdk.StoreSoftware,
		ECCConfig:          &xkmssdk.KeyECCConfig{Curve: xkmssdk.CurveP256},
	}
	if keyAlgo == xkmssdk.AlgorithmRSA {
		keyConfig.RSAConfig = &xkmssdk.KeyRSAConfig{KeySize: 2048}
		keyConfig.SignatureAlgorithm = xkmssdk.SigSHA256WithRSA
		keyConfig.ECCConfig = nil
	} else {
		switch s.config.CA.RootKeyCurve {
		case "P-384":
			keyConfig.ECCConfig = &xkmssdk.KeyECCConfig{Curve: xkmssdk.CurveP384}
			keyConfig.SignatureAlgorithm = xkmssdk.SigECDSAWithSHA384
		case "P-521":
			keyConfig.ECCConfig = &xkmssdk.KeyECCConfig{Curve: xkmssdk.CurveP521}
			keyConfig.SignatureAlgorithm = xkmssdk.SigECDSAWithSHA512
		}
	}

	serverKeyAttrs, err := xkmssdk.KeyAttributesFromConfig(keyConfig)
	if err != nil {
		return api.NewOperationFailedError("parse server key config for external CA", err)
	}
	serverKeyAttrs.CN = certCN

	// Get in-memory TLS config backed by the external CA's OpaqueKey.
	// ServerTLSConfig (not bare TLSConfig) makes server intent explicit
	// and silences the false-positive "ServerName not specified" WARN.
	s.tlsConfig, err = s.xkmsCA.ServerTLSConfig(serverKeyAttrs, false)
	if err != nil {
		return api.NewOperationFailedError("create server TLS config from external CA", err)
	}
	s.tlsConfig.MinVersion = tls.VersionTLS13
	// Advertise gRPC ALPN (h2) on the externally-issued TLS config so
	// the gRPC server's TLS handshake completes for clients that
	// negotiate HTTP/2.
	s.tlsConfig.NextProtos = []string{"h2", "http/1.1"}

	s.logger.Info("TLS certificate issued from external CA")
	return nil
}

// saveServerCert saves an issued certificate chain to a PEM file.
func saveServerCert(issued *xkmssdk.TrustedCAIssuedCertificate, certPath string) error {
	certDir := filepath.Dir(certPath)
	// #nosec G301 - Certs directory needs 0750 for group access
	if err := os.MkdirAll(certDir, 0750); err != nil {
		return api.NewDataDirOperationError("server.saveServerCert", "create certs directory", certDir, err)
	}

	// Build full chain PEM: server cert + CA cert
	var fullChainPEM []byte
	fullChainPEM = append(fullChainPEM, issued.CertificatePEM...)
	if issued.CACertificatePEM != nil {
		fullChainPEM = append(fullChainPEM, issued.CACertificatePEM...)
	}
	if issued.ChainPEM != nil {
		fullChainPEM = append(fullChainPEM, issued.ChainPEM...)
	}

	// Write cert file
	// #nosec G306 - Server cert needs 0640 for group read access
	if err := os.WriteFile(certPath, fullChainPEM, 0640); err != nil {
		return api.NewDataDirOperationError("server.saveServerCert", "write server certificate", certPath, err)
	}

	return nil
}

// defaultNewSealManager constructs a disk-persistent seal manager that
// satisfies interfaces.SealManager. Initialization state is stored under
// <dataDir>/barrier/seal_state.json so that initialized=true survives daemon
// restarts. Production deployments can replace this via newSealManagerFn to
// inject a real BarrierClient-backed manager.
func defaultNewSealManager(cfg *quicbaasseal.Config, log *logger.Logger, dataDir string) (interfaces.SealManager, error) {
	barrierDir := filepath.Join(dataDir, "barrier")
	mgr, err := grpcapi.NewPersistentDefaultSealManager(cfg, barrierDir)
	if err != nil {
		log.WithError(err).Warn("Failed to create persistent seal manager; falling back to in-memory (initialized state will not survive restart)")
		return grpcapi.NewDefaultSealManager(cfg), nil
	}
	return mgr, nil
}

// initBarrier initializes the seal manager and encrypting KV store when seal is enabled.
// When seal is disabled, this method is a no-op and GetKVStore returns raw storage.
func (s *Server) initBarrier() error {
	if !s.config.Seal.Enabled {
		s.logger.Info("Seal is disabled, skipping barrier initialization")
		return nil
	}

	s.logger.Info("Initializing seal barrier...")

	// Determine barrier directory
	barrierDir := s.config.Seal.BarrierDir
	if barrierDir == "" {
		barrierDir = filepath.Join(s.config.Storage.DataDir, "barrier")
	}

	// Create seal manager with barrier
	sealCfg := &quicbaasseal.Config{
		Mode:                quicbaasseal.SealMode(s.config.Seal.Mode),
		Threshold:           s.config.Seal.Threshold,
		TotalShares:         s.config.Seal.TotalShares,
		RecoveryKeysEnabled: s.config.Seal.RecoveryEnabled,
		RecoveryThreshold:   s.config.Seal.RecoveryThreshold,
		RecoveryTotalKeys:   s.config.Seal.RecoveryTotal,
	}

	mgr, err := newSealManagerFn(sealCfg, s.logger, s.config.Storage.DataDir)
	if err != nil {
		return api.NewOperationFailedError("create seal manager", err)
	}

	s.sealManager = mgr

	s.logger.WithFields(map[string]interface{}{
		"barrier_dir":  barrierDir,
		"mode":         s.config.Seal.Mode,
		"threshold":    s.config.Seal.Threshold,
		"total_shares": s.config.Seal.TotalShares,
	}).Info("Seal barrier initialized")

	return nil
}

// initDataPlaneEncryptor builds the canonical data-plane StorageBarrier and
// derives the single atrest.Encryptor from it. This must run after
// initBarrier (so the data directory exists) and BEFORE initVectorStorage and
// initTimeSeriesStorage, which need the encryptor to open their Pebble stores
// with FS-level AES-256-GCM encryption.
//
// The encryptor is stored on s.dataPlaneEncryptor. When the data-plane barrier
// cannot be constructed (no DataDir, construction error) the encryptor is a
// no-op and all stores open in plaintext, preserving backward compatibility.
func (s *Server) initDataPlaneEncryptor() error {
	dataPlaneBarrierSvc, rawBarrier, dataPlaneBarrierCloser, dpErr := buildDataPlaneBarrierService(s.config, slog.Default())
	if dpErr != nil {
		s.logger.Warnf("Data-plane barrier construction failed (%v) — stores will open in plaintext", dpErr)
	}
	if dataPlaneBarrierCloser != nil {
		s.dataPlaneBarrierCloser = dataPlaneBarrierCloser
	}
	if rawBarrier != nil {
		s.dataPlaneBarrier = rawBarrier
		s.logger.Info("Data-plane StorageBarrier constructed for at-rest encryption")
	}
	// Stash the service for later injection into the sharded client
	// (used by startService). Store via a package-level variable so
	// startService can pick it up without re-building the barrier.
	if dataPlaneBarrierSvc != nil {
		s.cachedDataPlaneBarrierSvc = dataPlaneBarrierSvc
	}
	// Build the single atrest.Encryptor. A nil barrier produces a no-op
	// Encryptor so callers do not need to nil-check before passing it on.
	s.dataPlaneEncryptor = atrest.New(rawBarrier)
	return nil
}

// fileExists returns true if the file at path exists and is a regular file.
//
//nolint:unused // used by tagged tests
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// startStorage initializes the storage layer based on the configured
// backend. For persistent backends (pebble, file), creates the data
// directory. For memory backend, no initialization is needed.
func (s *Server) startStorage() error {
	backend := s.config.Storage.Backend
	if backend == "" {
		backend = config.StorageBackendMemory
	}

	switch backend {
	case config.StorageBackendPebble, config.StorageBackendFile:
		dataDir := s.config.Storage.DataDir
		if dataDir == "" {
			return &StorageInitError{Backend: backend, Err: ErrDataDirRequired}
		}
		if err := os.MkdirAll(dataDir, 0750); err != nil {
			return &StorageInitError{Backend: backend, Err: err}
		}
		s.logger.WithFields(map[string]interface{}{
			"backend":  backend,
			"data_dir": dataDir,
		}).Info("Storage initialized")

	case config.StorageBackendMemory:
		s.logger.Info("Using in-memory storage (data will not persist across restarts)")

	default:
		s.logger.Infof("Using in-memory storage (unrecognized backend %q, falling back)", backend)
	}

	return nil
}

// startIndexes is a no-op. QuicBaaS operates as a BaaS client
// and does not host a local index manager.
func (s *Server) startIndexes() error {
	s.logger.Info("Index initialization skipped (BaaS client mode)")
	return nil
}

// startKMS initializes the KMS infrastructure when KMS is enabled in
// configuration. When s.config.KMS.Enabled is false, this is a no-op and
// s.kmsManager remains nil — callers that depend on the manager must check
// for nil before use. Build-tag gating still controls whether the KMS code
// is compiled into the binary at all.
func (s *Server) startKMS() error {
	if !s.config.KMS.Enabled {
		s.logger.Info("KMS service is disabled, skipping initialization")
		return nil
	}

	s.logger.Info("Initializing KMS infrastructure...")

	// Create logger adapter for XKMS
	logAdapter := logger.NewAdapter(s.logger)

	// Determine default backend type for XKMS initialization
	defaultBackendType := s.config.KMS.Backend
	if defaultBackendType == "" {
		defaultBackendType = config.BackendTypeSoftware
	}
	defaultBackendName := string(defaultBackendType)

	// Reuse the CA chain's key storage when available so that KEKs and
	// asymmetric CA keys share a single on-disk directory and a single XKMS
	// instance. This eliminates the two-chain problem: previously startKMS
	// created a parallel store under $DATA_DIR/kms-keys while initializeCA
	// had already registered $DATA_DIR/keys with the XKMS global singleton.
	// Wrap used one chain, unwrap saw the other → "cipher: message
	// authentication failed" on every first boot.
	//
	// When TLS is disabled (no CA chain) and a data directory is configured,
	// fall back to creating a file store under $DATA_DIR/xkms-keys so that
	// KEKs still survive restarts. Without a data directory (unit tests),
	// use the in-memory seam.
	var keyStorage xkmssdk.StorageBackend
	if s.xkmsKeyStorage != nil {
		// CA chain was initialised — reuse its storage root (canonical path).
		keyStorage = s.xkmsKeyStorage
		s.logger.Info("KMS key storage reusing CA chain storage (single XKMS key universe)")
	} else if dataDir := s.config.Storage.DataDir; dataDir != "" {
		xkmsKeysDir := filepath.Join(dataDir, "xkms-keys")
		var ksErr error
		keyStorage, ksErr = xkmssdk.NewFileStorageBackend(xkmsKeysDir)
		if ksErr != nil {
			return api.NewOperationFailedError("create KMS file key storage", ksErr)
		}
		s.logger.WithField("xkms_keys_dir", xkmsKeysDir).Info("KMS key storage backed by filesystem (TLS disabled)")
	} else {
		var ksErr error
		keyStorage, ksErr = newKMSKeyStorage()
		if ksErr != nil {
			return api.NewOperationFailedError("create key storage", ksErr)
		}
		s.logger.Info("KMS key storage backed by memory (no data directory configured)")
	}
	certStorage := xkmssdk.NewStorageMemory()

	// Create software backend that supports symmetric key operations (AES, ChaCha20)
	softwareBackend, err := newSoftwareBackend(&xkmssdk.SoftwareBackendConfig{
		KeyStorage: keyStorage,
	})
	if err != nil {
		return api.NewOperationFailedError("create software backend", err)
	}

	// Create XKMS keystore with software backend
	ks, err := newXKMSKeystore(&xkmssdk.ServiceBackendConfig{
		Backend:     softwareBackend,
		CertStorage: certStorage,
	})
	if err != nil {
		return api.NewOperationFailedError("create XKMS keystore", err)
	}

	// Initialize XKMS with the configured backend
	xkmsConfig := &dbxkms.Config{
		DefaultBackend: defaultBackendName,
		Backends: map[string]xkmssdk.ServiceBackend{
			defaultBackendName: ks,
		},
		Logger: logAdapter,
	}

	if err := initXKMS(xkmsConfig); err != nil {
		// Check if already initialized (not an error)
		if errors.Is(err, dbxkms.ErrAlreadyInitialized) {
			s.logger.Info("XKMS already initialized")
		} else {
			return api.NewOperationFailedError("initialize XKMS", err)
		}
	}

	// Create BackendFactory to manage multiple backends via registry
	factory, err := newKMSBackendFactory(&kms.BackendFactoryConfig{
		Logger:              logAdapter,
		EnabledBackends:     s.config.KMS.EnabledBackends,
		HealthCheckInterval: 30,
	})
	if err != nil {
		return api.NewOperationFailedError("create backend factory", err)
	}

	// Initialize factory from go-xkms's available backends
	ctx := context.Background()
	if err := initFromXKMSFn(factory, ctx, s.config.KMS.EnabledBackends); err != nil {
		return api.NewOperationFailedError("initialize KMS backends from XKMS", err)
	}

	// Get the registry populated with backends
	registry := factory.Registry()

	s.logger.WithFields(map[string]interface{}{
		"backends_count": len(registry.List()),
		"backends":       dbxkms.Backends(),
	}).Info("KMS backend registry initialized from go-xkms")

	// Create KMS Manager with the registry
	kmsMgr, err := newKMSServiceFn(&kms.KMSServiceOptions{
		Registry:         registry,
		Logger:           logAdapter,
		CacheKeyMaterial: s.config.KMS.Cache.Enabled,
	})
	if err != nil {
		return api.NewOperationFailedError("create KMS manager", err)
	}

	s.kmsManager = kmsMgr
	s.logger.WithFields(map[string]interface{}{
		"backends_count": kmsMgr.BackendCount(),
	}).Info("KMS manager initialized with backend registry")

	return nil
}

// startSecrets initializes the secrets manager unconditionally when the
// secrets package is compiled into the binary. Per operator decision
// (2026-04-22): "if the feature has been compiled into the binary then it's
// enabled". The previous Enabled config gate caused platformd to FATAL on
// App.Init for any node whose config carried kms:// URIs (TPM PINs, CA
// passwords, ACME account keys) when QuicBaaS Secrets defaulted to disabled.
// Build-tag gating still controls whether the secrets code is compiled into
// the binary at all; cfg.Secrets.Enabled is no longer consulted here.
func (s *Server) startSecrets() error {
	s.logger.Info("Initializing Secrets manager...")

	// Choose backend: when the qrdb SDK client is available use the
	// persistent KV-backed backend so secrets survive restarts.
	// startSecrets runs before startService (which creates qrdbClient), so
	// the KV backend is only available on a re-invocation path. For the
	// initial start the in-memory backend is used; the caller can replace
	// it by calling startSecrets again after startService completes.
	// The qrdbClient guard prevents a nil-pointer panic in GetKVStore.
	var backend secrets.Backend
	if s.qrdbClient != nil {
		kvStore := s.GetKVStore(qbshards.PlatformShardID)
		backend = secrets.NewKVBackend(kvStore)
		s.logger.Info("Secrets backend: persistent KV store (qrdb client)")
	} else {
		backend = secrets.NewMemoryBackend()
		s.logger.Info("Secrets backend: in-memory (qrdb client not yet initialized)")
	}

	// Create secrets manager with logger adapter.
	//
	// Locker is intentionally nil: the Manager's EnsureSecret falls back to a
	// process-local sync.Mutex keyed by secret path, which is correct for
	// single-node deployments. Distributed locking via qrdb's lock.Service
	// can be wired here in a future phase for multi-node cluster mode.
	logAdapter := logger.NewAdapter(s.logger)
	secretsMgr, err := newSecretsMgr(&secrets.ManagerOptions{
		Backend: backend,
		Logger:  logAdapter,
		Locker:  nil,
	})
	if err != nil {
		return api.NewOperationFailedError("create secrets manager", err)
	}

	s.secretsManager = secretsMgr
	s.logger.Info("Secrets manager initialized")

	return nil
}

// startCatalogManager initializes the catalog manager, backend manager, and background workers
// if object storage is configured or enabled via environment variable.
func (s *Server) startCatalogManager() error {
	// Skip if object storage is neither configured with backends nor enabled
	if !s.config.ObjectStorage.Enabled && len(s.config.ObjectStorage.Backends) == 0 {
		s.logger.Info("Object storage not configured, skipping catalog manager initialization")
		return nil
	}

	s.logger.Info("Initializing catalog manager for object storage...")

	// If enabled but no backends configured, create a default memory backend
	if s.config.ObjectStorage.Enabled && len(s.config.ObjectStorage.Backends) == 0 {
		s.logger.Info("Object storage enabled without backends, creating default memory backend")
		if s.config.ObjectStorage.Backends == nil {
			s.config.ObjectStorage.Backends = make(map[string]*config.StorageBackendConfig)
		}
		s.config.ObjectStorage.Backends["default"] = &config.StorageBackendConfig{
			Type:        "memory",
			Config:      map[string]string{},
			Description: "Auto-created default memory backend",
		}
	}

	// Create logger adapter
	logAdapter := logger.NewAdapter(s.logger)

	// Create catalog storage with a dedicated path for object metadata
	catalogPath := filepath.Join(s.config.Storage.DataDir, "catalog")
	catalogStorage, err := newCatalogStore(&catalog.PebbleStorageConfig{
		Path:       catalogPath,
		SyncWrites: true,
	})
	if err != nil {
		return api.NewOperationFailedError("create catalog storage", err)
	}

	// Create catalog manager
	catalogMgr, err := newCatalogMgr(&catalog.ManagerConfig{
		Storage: catalogStorage,
		Logger:  logAdapter,
	})
	if err != nil {
		return api.NewOperationFailedError("create catalog manager", err)
	}

	s.catalogManager = catalogMgr
	s.logger.Info("Catalog manager initialized")

	// Create backend manager for storage backends
	backendMgrConfig := catalog.DefaultBackendManagerConfig()
	backendMgr, err := newBackendMgr(backendMgrConfig)
	if err != nil {
		return api.NewOperationFailedError("create backend manager", err)
	}

	// Register configured storage backends
	for name, backendConfig := range s.config.ObjectStorage.Backends {
		cfg := &catalog.BackendConfig{
			Type:     backendConfig.Type,
			Enabled:  true,
			Settings: backendConfig.Config,
		}
		if err := backendMgr.RegisterBackend(name, cfg); err != nil {
			s.logger.WithFields(map[string]interface{}{
				"backend": name,
				"type":    backendConfig.Type,
				"error":   err.Error(),
			}).Warn("Failed to register storage backend, continuing without it")
		} else {
			s.logger.WithFields(map[string]interface{}{
				"backend": name,
				"type":    backendConfig.Type,
			}).Info("Registered storage backend")
		}
	}

	s.backendManager = backendMgr
	s.logger.Info("Backend manager initialized")

	// Create worker manager for background tasks
	workerMgr := catalog.NewWorkerManager(logAdapter)
	s.workerManager = workerMgr

	// Create default worker configuration with reasonable defaults
	workerConfig := catalog.DefaultWorkerConfig()

	// Create and register background workers
	replicationWorker, err := newReplicationWorkerFn(catalogMgr, backendMgr, workerConfig, logAdapter)
	if err != nil {
		s.logger.WithError(err).Warn("Failed to create replication worker")
	} else {
		if err := workerMgrRegisterFn(workerMgr, replicationWorker); err != nil {
			s.logger.WithError(err).Warn("Failed to register replication worker")
		} else {
			s.logger.Info("Registered replication worker")
		}
	}

	archiveWorker, err := newArchiveWorkerFn(catalogMgr, backendMgr, workerConfig, logAdapter)
	if err != nil {
		s.logger.WithError(err).Warn("Failed to create archive worker")
	} else {
		if err := workerMgrRegisterFn(workerMgr, archiveWorker); err != nil {
			s.logger.WithError(err).Warn("Failed to register archive worker")
		} else {
			s.logger.Info("Registered archive worker")
		}
	}

	verifyWorker, err := newVerifyWorkerFn(catalogMgr, backendMgr, workerConfig, logAdapter)
	if err != nil {
		s.logger.WithError(err).Warn("Failed to create verify worker")
	} else {
		if err := workerMgrRegisterFn(workerMgr, verifyWorker); err != nil {
			s.logger.WithError(err).Warn("Failed to register verify worker")
		} else {
			s.logger.Info("Registered verify worker")
		}
	}

	retentionConfig := catalog.DefaultRetentionWorkerConfig()
	retentionWorker, err := newRetentionWorkerFn(catalogMgr, backendMgr, retentionConfig, logAdapter)
	if err != nil {
		s.logger.WithError(err).Warn("Failed to create retention worker")
	} else {
		if err := workerMgrRegisterFn(workerMgr, retentionWorker); err != nil {
			s.logger.WithError(err).Warn("Failed to register retention worker")
		} else {
			s.logger.Info("Registered retention worker")
		}
	}

	// ReencryptWorker - re-encrypts objects when keys are rotated
	if s.kmsManager != nil {
		dekProvider, dekErr := newKMSDEKProviderFn(&catalog.KMSDEKProviderConfig{
			KMSManager: s.kmsManager,
			Logger:     logAdapter,
		})
		if dekErr != nil {
			s.logger.WithError(dekErr).Warn("Failed to create DEK provider, reencrypt worker will not be registered")
		} else {
			fullConfig := &config.ObjectStoreFullConfig{
				Backends: s.config.ObjectStorage.Backends,
				Defaults: *config.DefaultObjectStoreDefaults(),
			}
			enabledBackendStrings := make([]string, len(s.config.KMS.EnabledBackends))
			for i, bt := range s.config.KMS.EnabledBackends {
				enabledBackendStrings[i] = string(bt)
			}
			nodeConfig := &config.NodeObjectStorageConfig{
				EnabledBackends: enabledBackendStrings,
			}
			configResolver := config.NewObjectStoreResolver(fullConfig, nodeConfig)

			encryptionMgr, encErr := newEncryptionManagerFn(dekProvider, configResolver)
			if encErr != nil {
				s.logger.WithError(encErr).Warn("Failed to create encryption manager, reencrypt worker will not be registered")
			} else {
				reencryptConfig := catalog.DefaultReencryptWorkerConfig()
				reencryptWorker, reencryptErr := newReencryptWorkerSeamFn(catalogMgr, backendMgr, encryptionMgr, reencryptConfig, logAdapter)
				if reencryptErr != nil {
					s.logger.WithError(reencryptErr).Warn("Failed to create reencrypt worker")
				} else {
					if err := workerMgrRegisterFn(workerMgr, reencryptWorker); err != nil {
						s.logger.WithError(err).Warn("Failed to register reencrypt worker")
					} else {
						s.logger.Info("Registered reencrypt worker with KMS-backed DEK provider")
					}
				}
			}
		}
	}

	// Start all registered workers
	if err := workerMgrStartAllFn(workerMgr, context.Background()); err != nil {
		s.logger.WithError(err).Warn("Failed to start some background workers")
	}

	s.logger.WithFields(map[string]interface{}{
		"backends_count": len(s.config.ObjectStorage.Backends),
		"workers_count":  workerMgr.WorkerCount(),
	}).Info("Object storage catalog, backend manager, and background workers initialized")

	return nil
}

// stopCatalogManager shuts down the catalog manager.
func (s *Server) stopCatalogManager() error {
	if s.catalogManager == nil {
		return nil
	}

	s.logger.Debug("Closing catalog manager...")
	if err := s.catalogManager.Close(); err != nil {
		return api.NewOperationFailedError("close catalog manager", err)
	}

	s.catalogManager = nil
	s.logger.Debug("Catalog manager closed")
	return nil
}

// stopBackendManager shuts down the backend manager and all storage backends.
func (s *Server) stopBackendManager() error {
	if s.backendManager == nil {
		return nil
	}

	s.logger.Debug("Closing backend manager...")
	if err := s.backendManager.Close(); err != nil {
		return api.NewOperationFailedError("close backend manager", err)
	}

	s.backendManager = nil
	s.logger.Debug("Backend manager closed")
	return nil
}

// stopWorkerManager shuts down all background workers.
func (s *Server) stopWorkerManager() error {
	if s.workerManager == nil {
		return nil
	}

	s.logger.Debug("Stopping background workers...")
	if err := s.workerManager.StopAll(); err != nil {
		return api.NewOperationFailedError("stop worker manager", err)
	}

	s.workerManager = nil
	s.logger.Debug("Background workers stopped")
	return nil
}

// startBackupManager initializes the backup manager if enabled in config.
func (s *Server) startBackupManager() error {
	// Skip if backup is not enabled
	if !s.config.Backup.Enabled {
		s.logger.Info("Backup service is disabled, skipping initialization")
		return nil
	}

	s.logger.Info("Initializing backup manager...")

	// Determine the storage backend for backups
	storageBackend := s.config.Backup.StorageBackend
	if storageBackend == "" {
		storageBackend = "local"
	}

	// Build storage config from backup config
	storageConfig := make(map[string]string)
	if s.config.Backup.WorkDir != "" {
		storageConfig["path"] = s.config.Backup.WorkDir
		storageConfig["prefix"] = "backups"
	} else {
		storageConfig["path"] = filepath.Join(s.config.Storage.DataDir, "backups")
		storageConfig["prefix"] = "backups"
	}

	// Determine compression type
	var compression backup.CompressionType
	switch s.config.Backup.DefaultCompression {
	case "none":
		compression = backup.CompressionNone
	case "gzip":
		compression = backup.CompressionGzip
	case "zstd", "":
		compression = backup.CompressionZstd
	default:
		compression = backup.CompressionZstd
	}

	// Determine encryption type
	var encryption backup.EncryptionType
	var encryptionKey []byte
	switch s.config.Backup.DefaultEncryption {
	case "aes-256-gcm":
		if s.config.Backup.EncryptionKey != "" {
			encryption = backup.EncryptionAES256GCM
			var err error
			encryptionKey, err = base64.StdEncoding.DecodeString(s.config.Backup.EncryptionKey)
			if err != nil {
				encryptionKey = []byte(s.config.Backup.EncryptionKey)
			}
			if len(encryptionKey) != 32 {
				return api.NewEncryptionKeySizeError("server.startBackupManager", 32, len(encryptionKey))
			}
		} else {
			s.logger.WithFields(map[string]interface{}{
				"component": "server",
			}).Warn("AES-256-GCM encryption configured but no key provided, disabling backup encryption")
			encryption = backup.EncryptionNone
		}
	case "none", "":
		encryption = backup.EncryptionNone
	default:
		encryption = backup.EncryptionNone
	}

	// Determine timeout
	timeout := s.config.Backup.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}

	// Determine work directory
	workDir := s.config.Backup.WorkDir
	if workDir == "" {
		workDir = filepath.Join(s.config.Storage.DataDir, "backup-work")
	}

	// Create backup config
	backupConfig := &backup.Config{
		StorageBackend:     storageBackend,
		StorageConfig:      storageConfig,
		DefaultCompression: compression,
		DefaultEncryption:  encryption,
		EncryptionKey:      encryptionKey,
		WorkDir:            workDir,
		Timeout:            timeout,
	}

	// Create file-based storage for the backup manager.
	// The backup manager requires interfaces.Storage for snapshot iteration.
	backupDataDir := filepath.Join(workDir, "storage")
	fileConfig := filestorage.DefaultConfig()
	fileConfig.DataDir = backupDataDir
	backupStore, err := filestorage.New(fileConfig, s.logger)
	if err != nil {
		return api.NewOperationFailedError("create backup storage", err)
	}
	s.backupStorage = newBackupStorageAdapter(backupStore)

	// Create the backup manager
	mgr, err := backup.NewBackupManager(backupConfig, s.backupStorage, s.raftNode, s.logger)
	if err != nil {
		// Clean up storage on failure
		if closeErr := s.backupStorage.Close(); closeErr != nil {
			s.logger.WithError(closeErr).Warn("Failed to close backup storage during cleanup")
		}
		s.backupStorage = nil
		return api.NewOperationFailedError("create backup manager", err)
	}
	s.backupManager = mgr

	s.logger.Info("Backup manager initialized successfully")
	return nil
}

// stopBackupManager shuts down the backup manager.
func (s *Server) stopBackupManager() error {
	if s.backupManager == nil {
		return nil
	}

	s.logger.Debug("Closing backup manager...")
	if err := closeBackupMgrFn(s.backupManager); err != nil {
		return api.NewOperationFailedError("close backup manager", err)
	}

	s.backupManager = nil

	// Close the backup storage adapter
	if s.backupStorage != nil {
		if err := s.backupStorage.Close(); err != nil {
			s.logger.WithError(err).Warn("Failed to close backup storage")
		}
		s.backupStorage = nil
	}

	s.logger.Debug("Backup manager closed")
	return nil
}

// startService creates the QuicBaaS facade used by all API servers.
func (s *Server) startService() error {
	s.logger.Info("Initializing QuicBaaS facade...")

	// Create service configuration
	shards := s.config.Server.Shards
	if len(shards) == 0 {
		shards = []uint64{1}
	}

	// Include vector shard in service config if vectors are enabled
	if s.config.Vector.Enabled {
		vectorShardID := s.config.Vector.ShardID
		if vectorShardID == 0 {
			vectorShardID = qbshards.PlatformShardID
		}
		vectorShardExists := false
		for _, sid := range shards {
			if sid == vectorShardID {
				vectorShardExists = true
				break
			}
		}
		if !vectorShardExists {
			shards = append(shards, vectorShardID)
		}
	}

	// Include time series shard in service config if timeseries is enabled
	if s.config.TimeSeries.Enabled {
		tsShardID := s.config.TimeSeries.ShardID
		if tsShardID == 0 {
			tsShardID = qbshards.PlatformShardID
		}
		tsShardExists := false
		for _, sid := range shards {
			if sid == tsShardID {
				tsShardExists = true
				break
			}
		}
		if !tsShardExists {
			shards = append(shards, tsShardID)
		}
	}

	svcConfig := &service.ServiceConfig{
		NodeID:                     s.config.Raft.NodeID,
		Mode:                       s.config.Server.Mode,
		Version:                    "1.0.0",
		Shards:                     shards,
		EnableAuth:                 false,
		EnableMetrics:              true,
		EnableScheduler:            s.config.Scheduler.Enabled,
		SchedulerTickInterval:      s.config.Scheduler.TickInterval,
		SchedulerMaxConcurrentJobs: s.config.Scheduler.MaxConcurrentJobs,
		TimeSeriesShardID:          s.config.TimeSeries.ShardID,
		TenantIsolation:            s.config.Security.TenantIsolation,
		XKMS:                       s.config.XKMS,
		DataDir:                    s.config.Storage.DataDir,
	}

	// Build the QuicBaaS service using the builder pattern
	builder := service.NewServiceBuilder().
		WithLogger(logger.NewAdapter(s.logger)).
		WithConfig(svcConfig)

	// Wire the raft node when present. On the qrdb-node path s.raftNode
	// is nil; the service builder tolerates nil raftNode when qrdbClient
	// is set (which it always is on the node path via newNodeBackedShardedClient).
	if s.raftNode != nil {
		builder = builder.WithRaftNode(s.raftNode)
	}

	// Add KMS manager if initialized
	if s.kmsManager != nil {
		builder = builder.WithKMSManager(s.kmsManager)
		s.logger.Info("KMS manager attached to service")

		cryptoMgr := crypto.NewMemoryCryptoManager(s.kmsManager)
		builder = builder.WithCryptoManager(cryptoMgr)
		s.logger.Info("CryptoManager attached to service")
	}

	// Add Secrets manager if initialized
	if s.secretsManager != nil {
		builder = builder.WithSecretsManager(s.secretsManager)
		s.logger.Info("Secrets manager attached to service")
	}

	// Add Object Storage dependencies if catalog manager is initialized
	if s.catalogManager != nil && s.backendManager != nil {
		builder = builder.WithCatalogManager(s.catalogManager)
		builder = builder.WithBackendManager(s.backendManager)
		s.logger.Info("Object storage catalog and backend managers attached to service")
		if len(s.config.ObjectStorage.Backends) > 0 {
			fullConfig := &config.ObjectStoreFullConfig{
				Backends: s.config.ObjectStorage.Backends,
				Defaults: *config.DefaultObjectStoreDefaults(),
			}
			enabledBackendStrings := make([]string, 0)
			for name := range s.config.ObjectStorage.Backends {
				enabledBackendStrings = append(enabledBackendStrings, name)
			}
			nodeConfig := &config.NodeObjectStorageConfig{
				EnabledBackends: enabledBackendStrings,
			}
			configResolver := config.NewObjectStoreResolver(fullConfig, nodeConfig)
			builder = builder.WithObjectStoreConfigResolver(configResolver)
			s.logger.Info("Object storage config resolver attached to service")
		}

		if s.workerManager != nil {
			builder = builder.WithWorkerManager(s.workerManager)
			s.logger.Info("Object storage worker manager attached to service")
		}
	}

	// Add Vector service if initialized
	if s.vectorService != nil {
		builder = builder.WithVectorService(s.vectorService)
		s.logger.Info("Vector service attached to service builder")
	}

	// Add Time Series storage if initialized
	if s.timeSeriesStorage != nil {
		builder = builder.WithTimeSeriesStorage(s.timeSeriesStorage)
		s.logger.Info("Time series storage attached to service builder")
	}

	// Add Embedding manager if initialized
	if s.embeddingManager != nil {
		adapter := service.NewEmbeddingManagerAdapter(s.embeddingManager)
		builder = builder.WithEmbeddingManager(adapter)
		s.logger.Info("Embedding manager attached to service builder")
	}

	// Add Ollama service if initialized
	if s.ollamaService != nil {
		builder = builder.WithOllamaService(s.ollamaService)
		s.logger.Info("Ollama service attached to service builder")
	}

	// Add Backup manager if initialized
	if s.backupManager != nil {
		backupAdapter := service.NewBackupManagerAdapter(s.backupManager)
		builder = builder.WithBackupManager(backupAdapter)
		s.logger.Info("Backup manager attached to service builder")
	}

	// Initialize and add Cache manager
	cacheConfig := cache.Config{
		L1Size:          1000,
		L1TTL:           5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}
	if s.config.Cache.Enabled {
		if s.config.Cache.MaxSize > 0 {
			cacheConfig.L1Size = s.config.Cache.MaxSize
		}
		if s.config.Cache.DefaultTTL > 0 {
			cacheConfig.L1TTL = s.config.Cache.DefaultTTL
		}
		if s.config.Cache.CleanupInterval > 0 {
			cacheConfig.CleanupInterval = s.config.Cache.CleanupInterval
		}
	}
	cacheMgr, err := newCacheManagerFn(cacheConfig, logger.NewAdapter(s.logger))
	if err != nil {
		s.logger.Warnf("Failed to initialize cache manager: %v", err)
	} else {
		s.cacheManager = cacheMgr
		builder = builder.WithCacheManager(cacheMgr)
		s.logger.Info("Cache manager attached to service builder")
	}

	// Initialize the qrdb ShardedClient before building the service so that
	// the KVStore provider (used by Scheduler during Build) can resolve shards.
	//
	// Standalone mode: s.qrdbNode is the real qrdb engine. All data-plane
	// operations go through node.Client(), which is backed by the in-process
	// embedded transport. The node owns the barrier, at-rest encryptor, and
	// all storage — quicbaas does not build duplicate stores here.
	//
	// Both standalone and clustered modes set s.qrdbNode via
	// initStandaloneQRDBNode / initClusteredQRDBNode respectively, so this
	// branch is the live path for all configured server modes.
	if s.qrdbNode != nil {
		// Standalone/clustered node path: back the ShardedClient with the
		// qrdb node's SDK client. All shard IDs resolve to the same client;
		// the embedded engine handles routing internally. The node's SDK
		// client already has barrier/ceremony/credential wired via its
		// embedded transport — no duplicate construction needed here.
		s.qrdbClient = newNodeBackedShardedClient(s.qrdbNode)
		s.logger.Info("qrdb ShardedClient backed by real qrdb engine node")

		// Populate s.ceremonyServices from the node's services so that the
		// gRPC barrier/ceremony adapters and REST /api/v1/init/* routes can
		// resolve the barrier and ceremony service without re-building the
		// stack. When seal is disabled these fields are nil, matching the
		// existing NotSupported fallback behaviour.
		if svcs := s.qrdbNode.Services(); svcs != nil && svcs.Barrier != nil {
			s.ceremonyServices = &CeremonyServices{
				Barrier:    svcs.Barrier,
				Credential: svcs.Credential,
				Ceremony:   svcs.Ceremony,
			}
			s.logger.Info("Node barrier/ceremony/credential services wired from qrdb node")
		}
	}
	// Phase E: the else (legacy baasTransport) branch is removed.
	// s.qrdbNode is always set (both standalone and clustered modes build a
	// real qrdb node). The node-backed ShardedClient is the only code path.

	// Add KVStore provider for Scheduler and other services
	builder = builder.WithKVStoreProvider(func(shardID uint64) interface{} {
		return s.GetKVStore(shardID)
	})
	s.logger.Info("KVStore provider attached to service builder")

	builder = builder.WithQRDBClient(s.qrdbClient)
	s.logger.Info("qrdb SDK client attached to service builder")

	// Wire the Certificate Authority into the service builder so the
	// CertificateService inside QuicBaaS can issue and revoke certificates
	// on behalf of in-process callers (Security().Certificate().*). The CA
	// is the same instance used for TLS bootstrap above; whether it was
	// supplied externally via WithCA or created internally by initializeCA,
	// it is now reachable through the service facade for callers such as
	// platformd's provisionDaemonIdentity.
	if s.xkmsCA != nil {
		caAdapter, caErr := newCAServiceAdapter(s.xkmsCA)
		if caErr != nil {
			s.logger.Warnf("Failed to wire CA into service builder: %v", caErr)
		} else {
			// Propagate the operator-configured SPIFFE trust domain so issued
			// certificates carry their tenant-scoped SPIFFE URI SAN; login
			// derives the tenant from the verified peer certificate.
			caAdapter.SetSPIFFETrustDomain(s.config.IAM.SPIFFETrustDomain)
			// Keep a reference so UpdateCA() can swap the backing CA after start.
			s.caAdapter = caAdapter
			builder = builder.WithCAService(caAdapter)
			// Thread the application-supplied DANE publisher (if any) so
			// Trust().PublishDANE has a DNS hook. xkms ships none; when unset
			// PublishDANE returns a typed not-configured error.
			if s.opts.danePublisher != nil {
				builder = builder.WithDANEPublisher(s.opts.danePublisher)
			}
			s.logger.Info("CA service attached to service builder via TrustedCA adapter")

			// Compose the go-xkms ACME (RFC 8555) server off the same CA +
			// qrdb datastore and attach it to the facade. ACME is HTTP-native;
			// it is reachable over REST + the embedded transport only. A nil
			// servicer (ACME not configured) is benign; a construction error
			// is logged but does not abort startup.
			acmeSvc, acmeErr := s.buildACMEService(caAdapter)
			if acmeErr != nil {
				s.logger.Warnf("Failed to compose ACME service: %v", acmeErr)
			} else if acmeSvc != nil {
				builder = builder.WithACMEService(acmeSvc)
				s.logger.Info("ACME (RFC 8555) service attached to service builder")
			}
		}
	}

	svc, err := builder.Build()
	if err != nil {
		return api.NewOperationFailedError("create QuicBaaS", err)
	}

	s.quicBaaSService = svc
	s.logger.Info("QuicBaaS facade initialized")

	// Update the ShardedClient's transport factory with the service reference
	// for embedded transport mode (needed when creating embedded SDK clients).
	s.qrdbClient.factory.kvService = svc
	svc.SetQRDBClient(s.qrdbClient)
	s.logger.Info("qrdb ShardedClient attached to service")

	// Wire the unified login-ceremony facade. The login ceremony is hosted
	// in-process by the composed qrdb node; only node.Client().Login() exposes
	// a transport.LoginClient. The facade forwards BeginLogin/FinishLogin to it
	// and enforces the mTLS anti-spoofing model. When no node (or login client)
	// is present the facade fails closed with LoginCeremonyUnavailableError.
	if s.qrdbNode != nil {
		if nodeClient := s.qrdbNode.Client(); nodeClient != nil {
			if lc := nodeClient.Login(); lc != nil {
				svc.SetLoginService(service.NewLoginService(lc))
				s.logger.Info("unified login ceremony facade wired from qrdb node")
			}
		}
	}

	// Inject SDK client provider into vector service for write operations.
	// The vector service is created before the ShardedClient (startVector runs
	// before startService), so we inject the provider after construction.
	if s.vectorService != nil {
		if setter, ok := s.vectorService.(service.VectorSDKProviderSetter); ok {
			setter.SetSDKProvider(s.qrdbClient)
			s.logger.Info("SDK client provider injected into vector service")
		}
	}

	// Initialize IAM components and pass to service
	iamComponents, err := initIAMComponentsFn(s)
	if err != nil {
		s.logger.WithError(err).Warn("Failed to initialize IAM components, IAM features will be unavailable")
	} else if iamComponents != nil {
		s.iamComponents = iamComponents
		svc.SetIAMProvider(iamComponents)
		s.logger.Info("IAM components attached to service")

		// Bind the OIDC provider to the late-bound login session issuer that
		// was injected into the qrdb node before IAM init. This activates
		// OIDC bearer issuance for the node's in-process login ceremony.
		s.bindLoginCeremonyProvider(iamComponents)
	}

	// Tenant barrier lifecycle is owned by the qrdb node's TenantServicer.
	// When the qrdb node is initialized (initStandaloneQRDBNode/initClusteredQRDBNode),
	// barrier isolation is configured via the qrdb server options. No separate
	// BaaS-layer BarrierIsolationManager is needed.

	return nil
}

// startGRPCServer initializes and starts the gRPC server.
// In embedded mode, this creates the service without starting any listeners.
func (s *Server) startGRPCServer() error {
	// Check for embedded mode - skip all listeners but still initialize services
	if s.config.Server.Embedded {
		s.logger.Info("Running in embedded mode - skipping gRPC listener setup")
		s.logger.Info("Access QuicBaaS via Server.Service() directly")
		return nil
	}

	s.logger.Info("Initializing gRPC server...")

	// Initialize seal manager for gRPC endpoints
	if s.sealManager == nil {
		if err := initSealManagerFn(s.logger); err != nil {
			s.logger.Warnf("Failed to initialize seal manager: %v (seal operations will be unavailable)", err)
		}
	}

	// TLS is always enabled
	var grpcOpts []ggrpc.ServerOption
	creds := grpcapi.ServerTLSCredentialsFromConfig(s.tlsConfig)

	grpcOpts = []ggrpc.ServerOption{
		ggrpc.Creds(creds),
		ggrpc.MaxRecvMsgSize(10 * 1024 * 1024), // 10MB
		ggrpc.MaxSendMsgSize(10 * 1024 * 1024), // 10MB
		ggrpc.MaxConcurrentStreams(256),
		ggrpc.NumStreamWorkers(uint32(runtime.NumCPU())),
		ggrpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle: 15 * time.Minute,
			MaxConnectionAge:  30 * time.Minute,
			Time:              5 * time.Minute,
			Timeout:           20 * time.Second,
		}),
		ggrpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             30 * time.Second,
			PermitWithoutStream: false,
		}),
	}

	if s.config.Security.TLSClientAuth {
		s.logger.Info("Mutual TLS (client authentication) enabled")
	}

	// Add interceptors. The tenant interceptor runs after recovery so a panic
	// is still trapped, and before the handlers so tenant-scoped operations
	// observe the caller's tenant. It is best-effort: tenantResolve is nil in
	// non-iam builds (and the interceptor is a no-op pass-through), and even
	// when wired it never rejects a call for lack of a tenant.
	tenantResolve := s.tenantContextFunc()
	grpcOpts = append(grpcOpts,
		ggrpc.ChainUnaryInterceptor(
			grpcapi.UnaryTimeoutInterceptor(30*time.Second),
			grpcapi.UnaryLoggingInterceptor(s.logger),
			grpcapi.UnaryMetricsInterceptor(),
			grpcapi.UnaryRecoveryInterceptor(s.logger),
			grpcapi.UnaryTenantInterceptor(tenantResolve),
		),
		ggrpc.ChainStreamInterceptor(
			grpcapi.StreamTimeoutInterceptor(5*time.Minute),
			grpcapi.StreamLoggingInterceptor(s.logger),
			grpcapi.StreamMetricsInterceptor(),
			grpcapi.StreamRecoveryInterceptor(s.logger),
			grpcapi.StreamTenantInterceptor(tenantResolve),
		),
	)

	// Create gRPC server
	grpcServer := ggrpc.NewServer(grpcOpts...)

	// Create QuicBaaS gRPC service using QuicBaaS facade
	quicbaasService, err := newGRPCAPI(
		s.quicBaaSService,
		s.logger,
		s.config.Raft.NodeID,
		s.config.Server.Host,
	)
	if err != nil {
		return api.NewOperationFailedError("create gRPC service", err)
	}

	// Configure routing based on config
	vectorShardID := s.config.Vector.ShardID
	if vectorShardID == 0 && s.config.Vector.Enabled {
		vectorShardID = qbshards.PlatformShardID
	}
	kmsShardID := s.config.KMS.Raft.ShardID
	if kmsShardID == 0 {
		kmsShardID = qbshards.PlatformShardID
	}
	kvShardList := kvShards(s.config.Server.Shards, vectorShardID, s.config.TimeSeries.ShardID, kmsShardID)
	if err := validateRoutingFn(kvShardList, s.config.Server.EntityShardPins); err != nil {
		return api.NewConfigurationError("server.startGRPCServer", "validate routing config", err)
	}
	router := routing.NewWithShards(kvShardList)
	for et, shard := range s.config.Server.EntityShardPins {
		router.SetEntityShard(et, shard)
	}
	quicbaasService.SetShardRouter(router)
	quicbaasService.SetVectorShardID(vectorShardID)

	// Register QuicBaaS service
	pb.RegisterQuicBaaSServer(grpcServer, quicbaasService)

	// Register KMS services (if enabled)
	if kmsMgr := s.quicBaaSService.KMSManager(); kmsMgr != nil {
		kmsSvc := s.quicBaaSService.KMS()
		kmsServer := grpcapi.NewKMSServerWithService(kmsMgr, kmsSvc, s.logger)
		pb.RegisterKeyManagementServer(grpcServer, kmsServer)
		s.logger.Info("Registered KMS gRPC service with service layer support")

		if kmsSvc != nil {
			dekServer := grpcapi.NewDEKServer(kmsSvc, s.logger)
			pb.RegisterDEKManagementServer(grpcServer, dekServer)
			s.logger.Info("Registered DEKManagement gRPC service")
		}
	}

	// Register Secrets service (if enabled)
	if secretsSvc := s.quicBaaSService.Secrets(); secretsSvc != nil {
		secretsServer := grpcapi.NewSecretsServer(secretsSvc, s.logger)
		pb.RegisterSecretsManagementServer(grpcServer, secretsServer)
		s.logger.Info("Registered Secrets gRPC service")
	}

	// Register Crypto service (if enabled)
	if cryptoSvc := s.quicBaaSService.Crypto(); cryptoSvc != nil {
		cryptoServer := grpcapi.NewCryptoServer(cryptoSvc, s.logger)
		pb.RegisterCryptoServer(grpcServer, cryptoServer)
		s.logger.Info("Registered Crypto gRPC service")
	}

	// Register VectorDB service (if enabled)
	if vectorSvc := s.quicBaaSService.Vector(); vectorSvc != nil {
		vectorWrapper := grpcapi.NewVectorDBWrapper(quicbaasService)
		pb.RegisterVectorDBServer(grpcServer, vectorWrapper)
		s.logger.Info("Registered VectorDB gRPC service")

		vectorAdminWrapper := grpcapi.NewVectorAdminWrapper(quicbaasService)
		pb.RegisterVectorAdminServer(grpcServer, vectorAdminWrapper)
		s.logger.Info("Registered VectorAdmin gRPC service")
	}

	// Register Parameter Store service (if enabled)
	if parameterSvc := s.quicBaaSService.ParameterStoreService(); parameterSvc != nil {
		parameterServer := grpcapi.NewParameterStoreServer(parameterSvc, s.logger)
		pb.RegisterParameterStoreServer(grpcServer, parameterServer)
		s.logger.Info("Registered ParameterStore gRPC service")
	}

	// Register ObjectStorage service (if enabled)
	if objSvc := s.quicBaaSService.ObjectStorageService(); objSvc != nil {
		grpcapi.RegisterObjectStorageServer(grpcServer, objSvc)
		s.logger.Info("Registered ObjectStorage gRPC service")
	}

	// Register ObjectStorageAdmin service (if enabled)
	if objAdminSvc := s.quicBaaSService.ObjectStorageAdminService(); objAdminSvc != nil {
		grpcapi.RegisterObjectStorageAdminServer(grpcServer, objAdminSvc)
		s.logger.Info("Registered ObjectStorageAdmin gRPC service")
	}

	// Register Scheduler service (if enabled)
	if schedulerSvc := s.quicBaaSService.Scheduler(); schedulerSvc != nil {
		schedulerServer := grpcapi.NewSchedulerServer(schedulerSvc, s.logger)
		pb.RegisterSchedulerServiceServer(grpcServer, schedulerServer)
		s.logger.Info("Registered Scheduler gRPC service")
	}

	// Register Orchestrator service (if enabled)
	if agentSvc := s.quicBaaSService.Agent(); agentSvc != nil {
		orchestratorServer := grpcapi.NewOrchestratorServer(agentSvc, s.logger)
		pb.RegisterOrchestratorServiceServer(grpcServer, orchestratorServer)
		s.logger.Info("Registered Orchestrator gRPC service")
	}

	// Register Messaging service (if enabled)
	if messagingSvc := s.quicBaaSService.Messaging(); messagingSvc != nil {
		messagingServer := grpcapi.NewMessagingServer(messagingSvc, s.logger)
		pb.RegisterMessagingServiceServer(grpcServer, messagingServer)
		s.logger.Info("Registered Messaging gRPC service")
	}

	// Register Embedding service
	if s.quicBaaSService.Embedding() != nil {
		pb.RegisterEmbeddingServiceServer(grpcServer, quicbaasService)
		s.logger.Info("Registered Embedding gRPC service")
	}

	// Register IAM service (no-op when iam build tag is not set)
	s.registerIAMService(grpcServer)

	// Register Ollama service (no-op when rag build tag is not set)
	s.registerOllamaService(grpcServer)

	// Register XKMS service
	if len(xkmssdk.ServiceBackends()) > 0 {
		grpcapi.RegisterXKMSServer(grpcServer, s.logger)
		s.logger.Info("Registered XKMS gRPC service")
	}

	// Register CertificateService (CA) gRPC handlers. They live on the same
	// *grpcapi.Server that backs QuicBaaS and delegate to service.Certificate()
	// which composes the xkms TrustedCA, keeping the gRPC certificate surface
	// symmetric with the REST /api/v1/ca routes and the QUIC/MCP/Unix transports.
	s.registerCertificateService(grpcServer, quicbaasService)

	// Register TrustService (cross-sign / AIA) gRPC handlers. They live on
	// the same *grpcapi.Server and delegate to service.Trust(), keeping the
	// gRPC trust surface symmetric with REST /api/v1/trust and the QUIC/MCP/
	// Unix transports.
	s.registerTrustService(grpcServer, quicbaasService)

	// Register Barrier and Ceremony gRPC services. The handlers delegate to
	// the embedded go-qrdb instance via the QuicBaaS service facade, keeping
	// the gRPC surface symmetric with /api/v1/barrier and /api/v1/init REST.
	s.registerBarrierCeremonyServices(grpcServer)

	// Register the unified Login ceremony gRPC service. The handler extracts
	// the verified mTLS peer certificate from the gRPC TLS handshake and
	// delegates to the LoginCeremony() facade (-> embedded qrdb node).
	s.registerLoginService(grpcServer, quicbaasService)

	// Register qrdb.v1.QRDBService so SDK clients hitting quicbaasd via
	// gRPC (and Unix-over-gRPC) can reach the credential management surface
	// (Security().Credentials()) that lives on go-qrdb's proto interface.
	s.registerQRDBService(grpcServer)

	s.grpcServer = grpcServer
	s.grpcAPI = quicbaasService

	listenersStarted := 0

	// Start gRPC Unix socket listener (DEFAULT - for CLI compatibility)
	if s.config.Server.Unix.Socket != "" {
		if err := s.startUnixSocketListener(grpcServer); err != nil {
			return err
		}
		listenersStarted++
	}

	// Start TCP listener only if explicitly enabled
	if s.config.Server.GRPC.Enabled {
		tcpAddress := fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.GRPC.Port)
		tcpListener, err := listenFunc("tcp", tcpAddress)
		if err != nil {
			return api.NewOperationFailedError("create TCP listener", err)
		}
		s.grpcListener = tcpListener

		go func() {
			s.logger.Infof("gRPC server listening on TCP %s", tcpAddress)
			if err := grpcServer.Serve(tcpListener); err != nil {
				s.logger.WithError(err).Error("gRPC server TCP error")
			}
		}()
		listenersStarted++
	}

	// Ensure at least one listener is running
	if listenersStarted == 0 {
		return api.NewNoListenersConfiguredError("server.startGRPCServer",
			"no gRPC listeners configured - enable server.unix.socket or server.grpc.enabled")
	}

	// Give the server a moment to start
	select {
	case <-time.After(100 * time.Millisecond):
	case <-s.shutdownChan:
		return nil
	}

	return nil
}

// startUnixSocketListener creates and starts a Unix domain socket listener.
func (s *Server) startUnixSocketListener(grpcServer *ggrpc.Server) error {
	socketPath := s.config.Server.Unix.Socket

	// Create directory if it doesn't exist
	// #nosec G301 - Socket directory needs 0750 for group access but not world readable
	socketDir := filepath.Dir(socketPath)
	if err := os.MkdirAll(socketDir, 0750); err != nil {
		return api.NewDataDirOperationError("server.startUnixSocketListener", "create socket directory", socketDir, err)
	}

	// Remove existing socket file (in case of unclean shutdown)
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return api.NewOperationFailedError("remove existing socket file", err)
	}

	// Create Unix socket listener
	unixListener, err := listenFunc("unix", socketPath)
	if err != nil {
		return api.NewOperationFailedError("create Unix socket listener", err)
	}

	// Set permissions on socket file
	// #nosec G302 - Socket file needs 0660 for group access to allow local clients
	if err := os.Chmod(socketPath, 0660); err != nil {
		_ = unixListener.Close()
		return api.NewOperationFailedError("set socket permissions", err)
	}

	s.grpcUnixListener = unixListener

	go func() {
		s.logger.Infof("Starting gRPC server on Unix socket %s", socketPath)
		if err := grpcServer.Serve(unixListener); err != nil {
			s.logger.WithError(err).Error("gRPC server Unix socket error")
		}
	}()

	s.logger.Infof("Unix socket listener started on %s", socketPath)
	return nil
}

// stopGRPCServer gracefully shuts down the gRPC server.
func (s *Server) stopGRPCServer() {
	if s.grpcServer == nil {
		return
	}

	// Capture seam values to avoid race conditions in tests
	seamMu.Lock()
	gracefulStopFn := grpcGracefulStop
	forceStopFn := grpcForceStop
	timeout := grpcStopTimeout
	seamMu.Unlock()

	// Attempt graceful shutdown with timeout
	done := make(chan struct{})
	go func() {
		gracefulStopFn(s.grpcServer)
		close(done)
	}()

	// Wait for graceful shutdown or force stop after timeout
	select {
	case <-done:
		s.logger.Debug("gRPC server stopped gracefully")
	case <-time.After(timeout):
		s.logger.Warn("gRPC graceful shutdown timeout, forcing stop")
		forceStopFn(s.grpcServer)
	}

	// Close TCP listener
	if s.grpcListener != nil {
		_ = s.grpcListener.Close()
	}

	// Close Unix socket listener and clean up socket file
	if s.grpcUnixListener != nil {
		_ = s.grpcUnixListener.Close()
		if s.config.Server.Unix.Socket != "" {
			_ = os.Remove(s.config.Server.Unix.Socket)
		}
	}
}

// stopIndexes is a no-op. QuicBaaS operates as a BaaS client.
func (s *Server) stopIndexes() error {
	return nil
}

// startMessaging initializes and starts the messaging/push notification subsystem.
func (s *Server) startMessaging() error {
	s.logger.Info("Initializing messaging subsystem...")

	// Determine storage for the messaging service.
	var storAdapter qbstorage.Storage
	// No local pebble storage available in BaaS mode; pass nil for in-memory only.

	// Build service options from config
	var opts []messaging.ServiceOption
	if s.config.Messaging.BufferSize > 0 {
		opts = append(opts, messaging.WithServiceBufferSize(s.config.Messaging.BufferSize))
	}
	if s.config.Messaging.DedupEnabled {
		opts = append(opts, messaging.WithDedup(true))
		if s.config.Messaging.DedupWindow > 0 {
			opts = append(opts, messaging.WithDedupWindow(s.config.Messaging.DedupWindow))
		}
	}

	// Create and start the PushService
	pushService := messaging.NewPushService(s.logger, storAdapter, opts...)
	if err := pushServiceStartFn(pushService, context.Background()); err != nil {
		return api.NewOperationFailedError("start messaging service", err)
	}
	s.messagingService = pushService

	// Set the messaging service on the QuicBaaS facade
	if s.quicBaaSService != nil {
		s.quicBaaSService.SetMessagingService(pushService)
		s.logger.Info("Messaging service attached to QuicBaaS facade")
	}

	s.logger.Info("Messaging service started")

	// Start WatchBridge if enabled
	if s.config.Messaging.WatchBridge.Enabled {
		if err := s.startWatchBridge(); err != nil {
			return err
		}
	}

	return nil
}

// startWatchBridge creates and starts the WatchBridge that connects
// the watch.Manager to the messaging system.
func (s *Server) startWatchBridge() error {
	s.logger.Info("Initializing watch bridge...")

	if s.quicBaaSService == nil {
		return api.NewComponentUnavailableError("QuicBaaS service")
	}

	watchMgr := s.quicBaaSService.WatchManager()
	if watchMgr == nil {
		return api.NewComponentUnavailableError("watch manager")
	}

	// Build key-channel mappings from config
	mappings := make([]watchbridge.KeyChannelMapping, 0, len(s.config.Messaging.WatchBridge.Mappings))
	for _, m := range s.config.Messaging.WatchBridge.Mappings {
		mappings = append(mappings, watchbridge.KeyChannelMapping{
			KeyPrefix:     m.KeyPrefix,
			ChannelPrefix: m.ChannelPrefix,
			Filter: watchbridge.WatchEventFilter{
				NoPut:    m.FilterPut,
				NoDelete: m.FilterDelete,
			},
		})
	}

	// Build bridge options from config
	var opts []watchbridge.WatchBridgeOption
	opts = append(opts, watchbridge.WithLeaderOnly(s.config.Messaging.WatchBridge.LeaderOnly))
	if s.config.Messaging.WatchBridge.BufferSize > 0 {
		opts = append(opts, watchbridge.WithBridgeBufferSize(s.config.Messaging.WatchBridge.BufferSize))
	}
	if s.config.Messaging.WatchBridge.DefaultChannelPrefix != "" {
		opts = append(opts, watchbridge.WithDefaultChannelPrefix(s.config.Messaging.WatchBridge.DefaultChannelPrefix))
	}
	opts = append(opts, watchbridge.WithAutoCreateChannels(s.config.Messaging.WatchBridge.AutoCreateChannels))

	// No Raft node available in BaaS client mode; cluster info is nil.
	var clusterInfo cluster.ClusterInfo

	// Create the WatchBridge
	bridge, err := newWatchBridgeFn(
		s.logger,
		watchMgr,
		s.messagingService,
		clusterInfo,
		mappings,
		opts...,
	)
	if err != nil {
		return api.NewOperationFailedError("create watch bridge", err)
	}

	// Start the bridge
	if err := watchBridgeStartFn(bridge, context.Background()); err != nil {
		return api.NewOperationFailedError("start watch bridge", err)
	}

	s.watchBridge = bridge
	s.logger.Infof("Watch bridge started with %d mappings", len(mappings))

	return nil
}
