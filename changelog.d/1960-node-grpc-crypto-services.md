### Fixed

- The node's TCP gRPC port now serves KeyManagement, DEKManagement, Crypto, CertificateService, TrustService, AttestationService and PIVService. The SDK's gRPC transport calls these quicbaas.v1 services. Before this fix only the Unix socket served them, so `quicbaas --transport grpc kms key generate` failed with `unknown service quicbaas.kms.v1.KeyManagement`. They use the same gates as the Unix listener, and go-xkms's `xkms.v1` surface still runs beside them under its own service names (#1960).
