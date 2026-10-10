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

package config

// This file defines the operator-facing configuration structs for the
// hardware- and cloud-backed sealing strategies that go-xkms provides.
//
// QuicBaaS composes go-xkms: these structs carry only the transport- and
// addressing-level parameters needed to construct the corresponding go-xkms
// backend (e.g. go-xkms/pkg/backend/tpm2.Config). The actual seal/unseal
// cryptography lives in go-xkms and is never reimplemented here.
//
// Each struct is embedded in SealConfig (config.go) under the matching
// strategy ID and is only consumed when cfg.Seal.Strategy selects that
// backend. The build-tagged cloud/HSM strategies (awskms/gcpkms/azurekv/
// vault/pkcs11) read these structs from their factory files in
// pkg/server; the build-tag-free tpm2 strategy reads TPM2SealConfig from
// pkg/server/seal_factories.go.

// AWSSealConfig configures the AWS KMS sealing strategy (strategy "awskms").
// The strategy seals the barrier root key with a customer master key (CMK)
// managed by AWS KMS; the plaintext root key never leaves the process.
//
// Credentials are optional: when AccessKeyID/SecretAccessKey are empty the
// go-xkms backend falls back to the default AWS credential chain (environment
// variables, shared config, IAM role, IRSA). Endpoint overrides the KMS
// endpoint for testing against a local emulator such as Floci.
type AWSSealConfig struct {
	// Region is the AWS region hosting the KMS key (e.g. "us-east-1"). Required.
	Region string `mapstructure:"region"`

	// KeyID identifies the KMS customer master key. Accepts a key ID, key ARN,
	// alias name, or alias ARN. Required.
	KeyID string `mapstructure:"key_id"`

	// AccessKeyID is the AWS access key ID. Optional; when empty the default
	// AWS credential chain is used.
	AccessKeyID string `mapstructure:"access_key_id"`

	// SecretAccessKey is the AWS secret access key. Optional; pairs with
	// AccessKeyID. Treat as a secret — never log it.
	SecretAccessKey string `mapstructure:"secret_access_key"`

	// SessionToken is an optional STS session token for temporary credentials.
	SessionToken string `mapstructure:"session_token"`

	// Endpoint overrides the KMS service endpoint. Optional; used to target a
	// local emulator (Floci) in tests. Empty uses the standard regional endpoint.
	Endpoint string `mapstructure:"endpoint"`
}

// GCPSealConfig configures the Google Cloud KMS sealing strategy
// (strategy "gcpkms"). The barrier root key is wrapped by a Cloud KMS key
// addressed by project / location / key ring / key.
//
// CredentialsFile is optional: when empty the go-xkms backend uses
// Application Default Credentials (ADC), which covers Workload Identity and
// the GOOGLE_APPLICATION_CREDENTIALS environment variable.
type GCPSealConfig struct {
	// ProjectID is the GCP project hosting the key ring. Required.
	ProjectID string `mapstructure:"project_id"`

	// LocationID is the Cloud KMS location (e.g. "global", "us-east1"). Required.
	LocationID string `mapstructure:"location_id"`

	// KeyRingID is the Cloud KMS key ring name. Required.
	KeyRingID string `mapstructure:"key_ring_id"`

	// KeyID is the Cloud KMS crypto key name within the key ring. Required.
	KeyID string `mapstructure:"key_id"`

	// CredentialsFile is the path to a service-account JSON key file. Optional;
	// empty uses Application Default Credentials.
	CredentialsFile string `mapstructure:"credentials_file"`

	// Endpoint overrides the Cloud KMS API endpoint. Optional; used to target a
	// local mock in tests. Empty uses the standard endpoint.
	Endpoint string `mapstructure:"endpoint"`
}

// AzureSealConfig configures the Azure Key Vault sealing strategy
// (strategy "azurekv"). The barrier root key is wrapped by a key stored in
// the named Azure Key Vault.
//
// TenantID/ClientID/ClientSecret select an explicit service-principal
// identity. When all three are empty the go-xkms backend uses
// DefaultAzureCredential (managed identity, Azure CLI, environment).
type AzureSealConfig struct {
	// VaultURL is the Key Vault base URL
	// (e.g. "https://my-vault.vault.azure.net/"). Required.
	VaultURL string `mapstructure:"vault_url"`

	// KeyName is the name of the key within the vault used to wrap the root key.
	// Required.
	KeyName string `mapstructure:"key_name"`

	// TenantID is the Azure AD tenant ID for service-principal auth. Optional.
	TenantID string `mapstructure:"tenant_id"`

	// ClientID is the service-principal application (client) ID. Optional.
	ClientID string `mapstructure:"client_id"`

	// ClientSecret is the service-principal secret. Optional; pairs with
	// ClientID/TenantID. Treat as a secret — never log it.
	ClientSecret string `mapstructure:"client_secret"`
}

// VaultSealConfig configures the HashiCorp Vault Transit sealing strategy
// (strategy "vault"). The barrier root key is wrapped by a key managed by
// Vault's Transit secrets engine; the plaintext root key never leaves the
// process.
type VaultSealConfig struct {
	// Address is the Vault server address (e.g. "https://vault:8200"). Required.
	Address string `mapstructure:"address"`

	// Token is the Vault token used to authenticate. Required unless another
	// auth method is wired by the operator. Treat as a secret — never log it.
	Token string `mapstructure:"token"`

	// TransitPath is the mount path of the Transit secrets engine.
	// Default: "transit".
	TransitPath string `mapstructure:"transit_path"`

	// KeyName is the Transit key name used to wrap the root key. Required.
	KeyName string `mapstructure:"key_name"`

	// Namespace is the Vault Enterprise namespace. Optional.
	Namespace string `mapstructure:"namespace"`

	// TLSSkipVerify disables Vault TLS certificate verification. Intended for
	// development against a self-signed Vault only — never enable in production.
	// Default: false.
	TLSSkipVerify bool `mapstructure:"tls_skip_verify"`
}

// TPM2SealConfig configures the TPM 2.0 sealing strategy (strategy "tpm2").
// The barrier root key is sealed under the TPM's Platform SRK, whose use
// go-xkms always binds to the platform PCR policy (go-xkms#681). The TPM2
// strategy is always compiled in — it carries no build tag.
//
// The fields mirror go-xkms/pkg/backend/tpm2.Config so the factory can
// construct one directly without remapping. UseSimulator selects the
// in-memory TPM simulator (requires the go-xkms tpm_simulator build tag in
// the consuming binary) for testing without real hardware.
type TPM2SealConfig struct {
	// Device is the path to the TPM character device. Default: "/dev/tpmrm0".
	// Ignored when UseSimulator is true.
	Device string `mapstructure:"device"`

	// PCR is the PCR index used for platform policy. Default: 0.
	PCR uint `mapstructure:"pcr"`

	// PCRBank is the PCR bank hash algorithm (e.g. "SHA256"). Default: "SHA256".
	PCRBank string `mapstructure:"pcr_bank"`

	// SRKHandle is the persistent handle for the Storage Root Key.
	// Default: 0x81000001 (applied by the go-xkms backend).
	SRKHandle uint32 `mapstructure:"srk_handle"`

	// KeyDir is the directory where the TPM backend stores key blob metadata.
	// The sealed key material itself stays in the TPM. Default: "./tpm2-keys".
	KeyDir string `mapstructure:"key_dir"`

	// UseSimulator selects the in-memory TPM simulator instead of hardware.
	// Intended for tests. Default: false.
	UseSimulator bool `mapstructure:"use_simulator"`

	// SimulatorHost is the simulator hostname. Default: "localhost".
	// Only consulted when UseSimulator is true.
	SimulatorHost string `mapstructure:"simulator_host"`

	// SimulatorPort is the simulator port. Default: 2321.
	// Only consulted when UseSimulator is true.
	SimulatorPort int `mapstructure:"simulator_port"`
}

// PKCS11SealConfig configures the PKCS#11 HSM sealing strategy
// (strategy "pkcs11"). The barrier root key is wrapped by a key resident on a
// PKCS#11 token (HSM, SoftHSM, smart card). The plaintext root key never
// leaves the process.
type PKCS11SealConfig struct {
	// Library is the filesystem path to the PKCS#11 provider shared object
	// (e.g. "/usr/lib/softhsm/libsofthsm2.so"). Required.
	Library string `mapstructure:"library"`

	// TokenLabel selects the token by label. One of TokenLabel or Slot must be
	// set; TokenLabel takes precedence when both are present.
	TokenLabel string `mapstructure:"token_label"`

	// Slot selects the token by slot number. Optional pointer so that "slot 0"
	// is distinguishable from "slot unset". One of TokenLabel or Slot must be set.
	Slot *int `mapstructure:"slot"`

	// PIN is the user PIN for the token. Required. Treat as a secret — never log it.
	PIN string `mapstructure:"pin" json:"pin,omitempty" yaml:"pin,omitempty"`

	// KeyLabel is the label of the wrapping key on the token. Required.
	KeyLabel string `mapstructure:"key_label"`
}
