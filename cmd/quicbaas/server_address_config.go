// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Licensed under the GNU Affero General Public License v3.0 (AGPL-3.0).
// See LICENSE file in the root directory for details.

package main

import (
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Viper keys for the CLI's upstream server addresses. The keys sit under
// "client" so they never collide with the daemon's server.* settings.
const (
	serverAddressKey   = "client.address"
	transactionRESTKey = "client.transaction_rest"
)

// The address settings resolve through viper with the precedence
// flag > env > config file > default.
var (
	serverAddressBinding   = flagBinding{key: serverAddressKey, flag: "address", env: "QUICBAAS_CLIENT_ADDRESS"}
	transactionRESTBinding = flagBinding{key: transactionRESTKey, flag: "rest", env: "QUICBAAS_CLIENT_TRANSACTION_REST"}
	serverAddressBindings  = []flagBinding{serverAddressBinding, transactionRESTBinding}
)

// ErrMalformedServerAddress is returned when a configured server address is
// not a Unix socket path, a URL with a host, or host:port.
var ErrMalformedServerAddress = errors.New(errors.ErrConfiguration, "resolveServerAddresses", "malformed server address")

// addServerAddressFlag registers the global --address flag.
func addServerAddressFlag(flags *pflag.FlagSet) {
	flags.StringVar(&address, "address", config.DefaultUnixSocket, "QuicBaaS server address (Unix socket path or host:port)")
}

// bindServerAddressFlags binds --address on the root command and --rest on
// the txn command to their viper keys and environment variables.
func bindServerAddressFlags(v *viper.Viper, rootFlags, txnFlags *pflag.FlagSet) error {
	if err := bindFlagKeys(v, rootFlags, serverAddressBinding); err != nil {
		return err
	}
	return bindFlagKeys(v, txnFlags, transactionRESTBinding)
}

// resolveServerAddresses is the one place the CLI resolves its server
// addresses. It validates the flag, env, file or default value and stores
// it in address and restAddress, which every transport factory reads.
// flags is the executing command's merged flag set; a flag it lacks, such as
// --rest outside txn, cannot have been set.
func resolveServerAddresses(v *viper.Viper, flags *pflag.FlagSet) error {
	server, err := resolveAddress(v, flags, serverAddressBinding, validateServerAddress)
	if err != nil {
		return err
	}
	rest, err := resolveAddress(v, flags, transactionRESTBinding, validateRESTAddress)
	if err != nil {
		return err
	}
	address = server
	restAddress = rest
	return nil
}

// resolveAddress reads one address setting and validates it. Viper skips a
// set-but-empty environment variable, so an empty variable that would win
// is read directly and refused rather than silently falling through.
func resolveAddress(v *viper.Viper, flags *pflag.FlagSet, b flagBinding, validate func(string) error) (string, error) {
	value := v.GetString(b.key)
	source := addressSource(v, flags, b)
	if source == b.env {
		value = os.Getenv(b.env)
	}
	if err := validate(value); err != nil {
		return "", malformedServerAddress(b.key, source, value, err)
	}
	return value, nil
}

// addressSource names where an address setting comes from, following the
// precedence flag > env > config file > default.
func addressSource(v *viper.Viper, flags *pflag.FlagSet, b flagBinding) string {
	if f := flags.Lookup(b.flag); f != nil && f.Changed {
		return "--" + b.flag
	}
	if _, ok := os.LookupEnv(b.env); ok {
		return b.env
	}
	if v.InConfig(b.key) {
		return "config file"
	}
	return "default"
}

// validateServerAddress accepts a Unix socket path, a URL with a host, or
// host:port.
func validateServerAddress(value string) error {
	switch {
	case value == "":
		return errAddressEmpty
	case hasSpaceOrControl(value):
		return errAddressHasSpace
	case isUnixSocketAddress(value):
		return nil
	case strings.Contains(value, "://"):
		_, err := parseURLAddress(value)
		return err
	default:
		return validateHostPort(value)
	}
}

// validateRESTAddress accepts an http or https URL with a host, or host:port.
func validateRESTAddress(value string) error {
	switch {
	case value == "":
		return errAddressEmpty
	case hasSpaceOrControl(value):
		return errAddressHasSpace
	case strings.Contains(value, "://"):
		u, err := parseURLAddress(value)
		if err != nil {
			return err
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return errAddressScheme
		}
		return nil
	default:
		return validateHostPort(value)
	}
}

// Reasons a server address is malformed, wrapped by malformedServerAddress.
var (
	errAddressEmpty    = errors.New(errors.ErrConfiguration, "validateServerAddress", "address is empty")
	errAddressHasSpace = errors.New(errors.ErrConfiguration, "validateServerAddress", "address contains whitespace or control characters")
	errAddressNoHost   = errors.New(errors.ErrConfiguration, "validateServerAddress", "URL has no host")
	errAddressScheme   = errors.New(errors.ErrConfiguration, "validateServerAddress", "URL scheme is not http or https")
	errAddressPort     = errors.New(errors.ErrConfiguration, "validateServerAddress", "port is not a number from 1 to 65535")
)

func hasSpaceOrControl(value string) bool {
	return strings.IndexFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) >= 0
}

func parseURLAddress(value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil {
		return nil, err
	}
	if u.Host == "" {
		return nil, errAddressNoHost
	}
	return u, nil
}

func validateHostPort(value string) error {
	_, port, err := net.SplitHostPort(value)
	if err != nil {
		return err
	}
	n, err := strconv.ParseUint(port, 10, 16)
	if err != nil || n == 0 {
		return errAddressPort
	}
	return nil
}

// malformedServerAddress wraps ErrMalformedServerAddress with the key, its
// source, the offending value and the reason.
func malformedServerAddress(key, source, value string, reason error) error {
	return errors.Wrapf(ErrMalformedServerAddress, errors.ErrConfiguration, "resolveServerAddresses",
		"%s from %s %q: %s", key, source, value, reason.Error())
}
