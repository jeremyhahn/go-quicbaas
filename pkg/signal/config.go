//go:build quicbaas_signal

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

package signal

const (
	// defaultMaxAccounts is the default maximum number of accounts per tenant.
	defaultMaxAccounts = 10000

	// defaultMaxDevices is the default maximum number of devices per account.
	defaultMaxDevices = 6

	// defaultMessageRetention is the default message retention period.
	defaultMessageRetention = "30d"
)

// SignalConfig controls the global Signal service configuration.
type SignalConfig struct {
	Enabled bool `yaml:"enabled"`
}

// SignalTenantConfig controls per-tenant Signal behavior.
type SignalTenantConfig struct {
	Enabled          bool   `yaml:"enabled"`
	MaxAccounts      int    `yaml:"maxAccounts"`
	MaxDevices       int    `yaml:"maxDevices"`
	MessageRetention string `yaml:"messageRetention"`
	FIDO2Enabled     bool   `yaml:"fido2Enabled"`
	AllowFIDO2Only   bool   `yaml:"allowFido2Only"`
}

// Validate checks that all required fields in the tenant configuration are
// valid. Returns an InvalidConfigError for the first invalid field encountered.
func (c *SignalTenantConfig) Validate() error {
	if c.MaxAccounts < 0 {
		return &InvalidConfigError{
			Field:  "maxAccounts",
			Reason: "must not be negative",
		}
	}
	if c.MaxDevices < 0 {
		return &InvalidConfigError{
			Field:  "maxDevices",
			Reason: "must not be negative",
		}
	}
	if c.MessageRetention != "" {
		if err := validateRetention(c.MessageRetention); err != nil {
			return err
		}
	}
	if c.AllowFIDO2Only && !c.FIDO2Enabled {
		return &InvalidConfigError{
			Field:  "allowFido2Only",
			Reason: "requires fido2Enabled to be true",
		}
	}
	return nil
}

// ApplyDefaults sets default values for any unset configuration fields.
func (c *SignalTenantConfig) ApplyDefaults() {
	if c.MaxAccounts == 0 {
		c.MaxAccounts = defaultMaxAccounts
	}
	if c.MaxDevices == 0 {
		c.MaxDevices = defaultMaxDevices
	}
	if c.MessageRetention == "" {
		c.MessageRetention = defaultMessageRetention
	}
}

// validateRetention checks that a retention string ends with a known duration
// suffix (d for days, h for hours).
func validateRetention(retention string) error {
	if len(retention) < 2 {
		return &InvalidConfigError{
			Field:  "messageRetention",
			Reason: "must be a duration like '30d' or '720h'",
		}
	}
	suffix := retention[len(retention)-1]
	if suffix != 'd' && suffix != 'h' {
		return &InvalidConfigError{
			Field:  "messageRetention",
			Reason: "must end with 'd' (days) or 'h' (hours)",
		}
	}
	for _, ch := range retention[:len(retention)-1] {
		if ch < '0' || ch > '9' {
			return &InvalidConfigError{
				Field:  "messageRetention",
				Reason: "numeric portion must contain only digits",
			}
		}
	}
	return nil
}
