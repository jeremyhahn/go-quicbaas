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

package config

// DatabasesConfig configures the multi-tenant database subsystem.
type DatabasesConfig struct {
	// Enabled controls whether the database management subsystem is active.
	Enabled bool `mapstructure:"enabled" json:"enabled" yaml:"enabled"`

	// DataDir is the root directory for database storage.
	DataDir string `mapstructure:"data_dir" json:"data_dir" yaml:"data_dir"`

	// MaxPerTenant is the maximum number of user databases per tenant.
	MaxPerTenant int `mapstructure:"max_per_tenant" json:"max_per_tenant" yaml:"max_per_tenant"`

	// DefaultBackend is the default storage backend for new databases.
	DefaultBackend string `mapstructure:"default_backend" json:"default_backend" yaml:"default_backend"`

	// DefaultEngine is the default engine type for new databases.
	DefaultEngine string `mapstructure:"default_engine" json:"default_engine" yaml:"default_engine"`

	// AllowedEngines lists the engine types tenants can use.
	AllowedEngines []string `mapstructure:"allowed_engines" json:"allowed_engines" yaml:"allowed_engines"`

	// AllowedBackends lists the backend types tenants can use.
	AllowedBackends []string `mapstructure:"allowed_backends" json:"allowed_backends" yaml:"allowed_backends"`

	// EnablePerDatabaseEncryption enables barrier encryption for each database.
	EnablePerDatabaseEncryption bool `mapstructure:"enable_per_database_encryption" json:"enable_per_database_encryption" yaml:"enable_per_database_encryption"`
}

// DefaultDatabasesConfig returns the default databases configuration.
func DefaultDatabasesConfig() DatabasesConfig {
	return DatabasesConfig{
		Enabled:                     false,
		DataDir:                     "data",
		MaxPerTenant:                50,
		DefaultBackend:              "pebble",
		DefaultEngine:               "kv",
		AllowedEngines:              []string{"kv", "timeseries", "vector"},
		AllowedBackends:             []string{"memory", "pebble"},
		EnablePerDatabaseEncryption: true,
	}
}
