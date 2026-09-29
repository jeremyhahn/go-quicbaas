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

package fleet

import (
	"bytes"
	"errors"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// The daemon config file is Config written as YAML: one schema for every
// daemon setting, whether it arrives on `fleet serve` flags, in a file an
// operator wrote, or in the file `fleet deploy` renders from a node spec's
// `agent:` section. There is no second, file-only shape to keep in step.

// ConfigFileError reports a daemon config file that could not be read or
// decoded. Path names the file so the operator does not have to guess which
// of several configs was refused.
type ConfigFileError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *ConfigFileError) Error() string {
	return "agent: config file " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns the underlying read or decode failure.
func (e *ConfigFileError) Unwrap() error { return e.Cause }

// LoadConfigFile decodes the YAML config file at path onto into.
//
// Keys the file does not name keep whatever into already holds, so a caller
// seeds into with its defaults and the file overrides only what it states.
// That is also what lets `fleet serve` layer explicitly-set flags on top.
//
// Unknown keys are refused: a misspelt setting would otherwise be dropped in
// silence and the daemon would run on the default the operator meant to
// change. The result is not validated; callers run Validate once every layer
// has been applied.
func LoadConfigFile(path string, into *Config) error {
	if into == nil {
		return ErrConfigNil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the path is the operator's own --config argument.
	if err != nil {
		return &ConfigFileError{Path: path, Cause: err}
	}
	if err := DecodeConfig(data, into); err != nil {
		return &ConfigFileError{Path: path, Cause: err}
	}
	return nil
}

// DecodeConfig decodes YAML config bytes onto into with the same rules as
// LoadConfigFile: absent keys keep into's values and unknown keys are an
// error. An empty document leaves into unchanged.
func DecodeConfig(data []byte, into *Config) error {
	if into == nil {
		return ErrConfigNil
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(into); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// MarshalConfig renders cfg as the YAML document LoadConfigFile reads back.
func MarshalConfig(cfg *Config) ([]byte, error) {
	if cfg == nil {
		return nil, ErrConfigNil
	}
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(cfg); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
