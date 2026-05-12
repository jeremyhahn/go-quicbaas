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
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// BotConfig is the top-level configuration loaded from disk.
type BotConfig struct {
	// BotIdentity is the E.164 phone number or username the bot
	// operates under on the go-signal server.
	BotIdentity string `yaml:"botIdentity"`

	// GoSignalURL is the base URL of the user's go-signal deployment.
	GoSignalURL string `yaml:"goSignalURL"`

	// DeviceName is advertised to the Signal server as the bot's
	// device name.
	DeviceName string `yaml:"deviceName"`

	// TenantID is the QuicBaaS tenant the bot operates under.
	TenantID string `yaml:"tenantID"`

	// QuicBaaSAddr is the central QuicBaaS endpoint. Can be a REST URL,
	// gRPC host:port, QUIC host:port, or unix socket path depending on
	// QuicBaaSTransport.
	QuicBaaSAddr string `yaml:"quicbaasAddr"`

	// QuicBaaSTransport selects the SDK transport. Defaults to "grpc".
	QuicBaaSTransport string `yaml:"quicbaasTransport"`

	// QuicBaaSCA is the path to the TLS CA certificate used to verify
	// the central.
	QuicBaaSCA string `yaml:"quicbaasCA"`

	// QuicBaaSCert / QuicBaaSKey enable mutual TLS against the central.
	QuicBaaSCert string `yaml:"quicbaasCert"`
	QuicBaaSKey  string `yaml:"quicbaasKey"`

	// AuthMode selects the Signal-to-QuicBaaS identity policy.
	AuthMode AuthMode `yaml:"authMode"`

	// ServiceAccount is the QuicBaaS operator subject used under
	// AuthModeImplicit and AuthModeGroupOnly.
	ServiceAccount string `yaml:"serviceAccount"`

	// StatePath is the filesystem directory for persisted state. The
	// state file lives at "{StatePath}/state.json".
	StatePath string `yaml:"statePath"`

	// VerbosityDefaults are the out-of-the-box presentation prefs
	// applied to new conversations.
	VerbosityDefaults VerbosityPrefs `yaml:"verbosityDefaults"`

	// RateLimit caps outbound sends per second per conversation.
	RateLimit float64 `yaml:"rateLimit"`

	// PreKeyRotationThreshold triggers a pre-key refresh when the
	// server reports fewer than this many one-time pre-keys remaining.
	PreKeyRotationThreshold int `yaml:"preKeyRotationThreshold"`

	// PreKeyCheckInterval is how often the maintenance loop polls.
	PreKeyCheckInterval time.Duration `yaml:"preKeyCheckInterval"`

	// LinkedOperators is the signal-id → operator mapping under
	// AuthModeLinked.
	LinkedOperators map[string]OperatorIdentity `yaml:"linkedOperators,omitempty"`

	// AllowedGroups is the conversation-id allowlist under
	// AuthModeGroupOnly.
	AllowedGroups []string `yaml:"allowedGroups,omitempty"`
}

// LoadBotConfig reads and decodes a YAML config file.
func LoadBotConfig(path string) (*BotConfig, error) {
	const op = "signalbridge.LoadBotConfig"
	if path == "" {
		return nil, newError(op, ErrKindInvalidInput, "path required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, wrapError(op, ErrKindStorage, err, "read config file")
	}
	var cfg BotConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, wrapError(op, ErrKindInvalidInput, err, "decode config yaml")
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// applyDefaults fills in reasonable defaults for unset fields.
func (c *BotConfig) applyDefaults() {
	if c.DeviceName == "" {
		c.DeviceName = "orchestrator-bot"
	}
	if c.TenantID == "" {
		c.TenantID = "default"
	}
	if c.QuicBaaSTransport == "" {
		c.QuicBaaSTransport = "grpc"
	}
	if c.AuthMode == "" {
		c.AuthMode = AuthModeImplicit
	}
	if c.StatePath == "" {
		c.StatePath = "/var/lib/quicbaas-signalbridge"
	}
	if c.VerbosityDefaults == (VerbosityPrefs{}) {
		c.VerbosityDefaults = DefaultVerbosityPrefs()
	}
	if c.PreKeyCheckInterval <= 0 {
		c.PreKeyCheckInterval = 10 * time.Minute
	}
	if c.PreKeyRotationThreshold <= 0 {
		c.PreKeyRotationThreshold = 10
	}
}

// Validate enforces required fields. Returns ErrKindInvalidInput.
func (c *BotConfig) Validate() error {
	const op = "signalbridge.BotConfig.Validate"
	if c.BotIdentity == "" {
		return newError(op, ErrKindInvalidInput, "botIdentity is required")
	}
	if c.GoSignalURL == "" {
		return newError(op, ErrKindInvalidInput, "goSignalURL is required")
	}
	if c.QuicBaaSAddr == "" {
		return newError(op, ErrKindInvalidInput, "quicbaasAddr is required")
	}
	switch c.AuthMode {
	case AuthModeImplicit, AuthModeLinked, AuthModeGroupOnly:
		// ok
	default:
		return newError(op, ErrKindInvalidInput, "authMode must be one of implicit|linked|group-only")
	}
	if c.AuthMode == AuthModeImplicit && c.ServiceAccount == "" {
		return newError(op, ErrKindInvalidInput, "serviceAccount is required for implicit auth mode")
	}
	return nil
}
