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

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidEnvValue is the sentinel matched by errors.Is when a QUICBAAS_
// environment override cannot be parsed into its field's type.
var ErrInvalidEnvValue = errors.New("config: invalid environment variable value")

// InvalidEnvValueError names the environment variable whose value could not
// be parsed, so a malformed override fails the load instead of silently
// leaving the file or default value in place.
type InvalidEnvValueError struct {
	Name  string
	Value string
	Kind  string
}

// Error implements the error interface.
func (e *InvalidEnvValueError) Error() string {
	return ErrInvalidEnvValue.Error() + ": " + e.Name + "=" + strconv.Quote(e.Value) + " is not a valid " + e.Kind
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *InvalidEnvValueError) Unwrap() error {
	return ErrInvalidEnvValue
}

// envString sets *dst to the named variable's value when it is set and
// non-empty.
func envString(name string, dst *string) {
	if val := os.Getenv(name); val != "" {
		*dst = val
	}
}

// EnvBool sets *dst from the named variable. It accepts true/1/yes/on and
// false/0/no/off, case-insensitively.
func EnvBool(name string, dst *bool) error {
	val := os.Getenv(name)
	if val == "" {
		return nil
	}
	switch strings.ToLower(val) {
	case "true", "1", "yes", "on":
		*dst = true
	case "false", "0", "no", "off":
		*dst = false
	default:
		return &InvalidEnvValueError{Name: name, Value: val, Kind: "boolean"}
	}
	return nil
}

// envInt sets *dst from the named variable parsed as a base-10 integer.
func envInt(name string, dst *int) error {
	val := os.Getenv(name)
	if val == "" {
		return nil
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return &InvalidEnvValueError{Name: name, Value: val, Kind: "integer"}
	}
	*dst = n
	return nil
}

// EnvDuration sets *dst from the named variable parsed by time.ParseDuration.
func EnvDuration(name string, dst *time.Duration) error {
	val := os.Getenv(name)
	if val == "" {
		return nil
	}
	d, err := time.ParseDuration(val)
	if err != nil {
		return &InvalidEnvValueError{Name: name, Value: val, Kind: "duration"}
	}
	*dst = d
	return nil
}
