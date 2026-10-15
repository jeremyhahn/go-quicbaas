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
	"net"

	"github.com/spf13/viper"
)

// RaftAddressKeys are the Raft address keys, bound to the environment by
// BindRaftAddressEnv (QUICBAAS_RAFT_RAFT_ADDRESS, QUICBAAS_RAFT_LISTEN_ADDRESS,
// QUICBAAS_RAFT_LISTEN_HOST). The daemon and the Loader bind from this one
// list so neither can leave a key reachable only from a file.
var RaftAddressKeys = []string{
	"raft.raft_address",
	"raft.listen_address",
	"raft.listen_host",
}

// BindRaftAddressEnv binds the Raft address keys to their environment
// variables.
func BindRaftAddressEnv(v *viper.Viper) {
	for _, key := range RaftAddressKeys {
		_ = v.BindEnv(key)
	}
}

// RaftListenAddressError reports that the Raft listen address could not be
// derived: raft.listen_address is unset and raft.raft_address either carries
// no port to listen on or, with raft.listen_host also unset, names no host to
// bind (see RaftAdvertisedHostError).
type RaftListenAddressError struct {
	// RaftAddress is the configured raft.raft_address.
	RaftAddress string

	// ListenHost is the configured raft.listen_host.
	ListenHost string

	// Cause is the parse failure of RaftAddress or a *RaftAdvertisedHostError.
	Cause error
}

// Error implements the error interface.
func (e *RaftListenAddressError) Error() string {
	return "raft.listen_address is unset and cannot be derived from raft.raft_address " +
		"\"" + e.RaftAddress + "\": " + e.Cause.Error()
}

// Unwrap returns the reason the listen address could not be derived.
func (e *RaftListenAddressError) Unwrap() error { return e.Cause }

// RaftAdvertisedHostError reports that the host of raft.raft_address is empty
// or a wildcard (0.0.0.0, ::). Such a host is never bound by default: it is
// not an address a peer can dial, and binding it would silently listen on
// every interface.
type RaftAdvertisedHostError struct {
	// Host is the host part of raft.raft_address.
	Host string
}

// Error implements the error interface.
func (e *RaftAdvertisedHostError) Error() string {
	return "advertised host \"" + e.Host + "\" is empty or a wildcard; set raft.raft_address " +
		"(--raft-address) to a host:port peers can reach, or set raft.listen_host or raft.listen_address explicitly"
}

// ListenAddr returns the host:port the Raft transport binds, by the
// precedence documented on RaftConfig.ListenAddress.
func (c *RaftConfig) ListenAddr() (string, error) {
	if c.ListenAddress != "" {
		return c.ListenAddress, nil
	}
	host, port, err := net.SplitHostPort(c.RaftAddress)
	if err == nil && port == "" {
		err = &net.AddrError{Err: "missing port in address", Addr: c.RaftAddress}
	}
	if err != nil {
		return "", &RaftListenAddressError{RaftAddress: c.RaftAddress, ListenHost: c.ListenHost, Cause: err}
	}
	if c.ListenHost != "" {
		return net.JoinHostPort(c.ListenHost, port), nil
	}
	if isWildcardHost(host) {
		return "", &RaftListenAddressError{RaftAddress: c.RaftAddress, Cause: &RaftAdvertisedHostError{Host: host}}
	}
	return net.JoinHostPort(host, port), nil
}

// AdvertisedHost returns the host part of raft_address: the name peers
// and clients reach this node by. A missing port, or a wildcard host, is
// a RaftListenAddressError, the latter wrapping RaftAdvertisedHostError.
func (c *RaftConfig) AdvertisedHost() (string, error) {
	host, port, err := net.SplitHostPort(c.RaftAddress)
	if err == nil && port == "" {
		err = &net.AddrError{Err: "missing port in address", Addr: c.RaftAddress}
	}
	if err != nil {
		return "", &RaftListenAddressError{RaftAddress: c.RaftAddress, Cause: err}
	}
	if isWildcardHost(host) {
		return "", &RaftListenAddressError{RaftAddress: c.RaftAddress, Cause: &RaftAdvertisedHostError{Host: host}}
	}
	return host, nil
}

// isWildcardHost reports whether host is empty or an unspecified IP address.
func isWildcardHost(host string) bool {
	if host == "" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}
