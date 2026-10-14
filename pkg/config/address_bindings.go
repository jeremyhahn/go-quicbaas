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
	"fmt"
	"net"
	"strconv"

	"github.com/spf13/viper"
)

// Address keys: a bind host, an advertised port or an upstream target that
// must be settable from the config file, the environment and a quicbaasd
// flag. Each environment name is QUICBAAS_ plus the key with "." as "_".
const (
	KeyServerHost                       = "server.host"
	KeyRaftEnrollmentACMEChallengePort  = "raft.raft_enrollment.acme.challenge_port"
	KeyRaftEnrollmentAdvertiseNoisePort = "raft.raft_enrollment.advertise_noise_port"
	KeyRAGEmbeddingAPIURL               = "rag.embedding.api_url"
	KeyCacheL2RedisAddr                 = "cache.l2_redis_addr"
	KeyDiscoveryMulticastGroup          = "discovery.multicast.group"
)

// addressKeys lists every address key BindAddressEnv binds.
var addressKeys = []string{
	KeyServerHost,
	KeyRaftEnrollmentACMEChallengePort,
	KeyRaftEnrollmentAdvertiseNoisePort,
	KeyRAGEmbeddingAPIURL,
	KeyCacheL2RedisAddr,
	KeyDiscoveryMulticastGroup,
}

// BindAddressEnv binds every address key to its QUICBAAS_ environment name
// on v. Both viper instances that load the daemon call it: without the
// explicit binding viper's Unmarshal never consults the environment for a
// key absent from the config file.
func BindAddressEnv(v *viper.Viper) {
	for _, key := range addressKeys {
		_ = v.BindEnv(key)
	}
}

// validateAddressBindings rejects an address value that decoded but cannot
// be used: a challenge port that is not a port number, an advertised port
// out of range, or a multicast group that is not a multicast IP.
func (v *Validator) validateAddressBindings(cfg *Config) {
	if port := cfg.Raft.RaftEnrollment.ACME.ChallengePort; port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			v.addError(KeyRaftEnrollmentACMEChallengePort, fmt.Sprintf("challenge port must be a number between 1 and 65535, got %q", port))
		}
	}

	// 0 selects the qrdb clusterenroll default port.
	if port := cfg.Raft.RaftEnrollment.AdvertiseNoisePort; port < 0 || port > 65535 {
		v.addError(KeyRaftEnrollmentAdvertiseNoisePort, fmt.Sprintf("advertise noise port must be 0 (default) or between 1 and 65535, got %d", port))
	}

	if group := cfg.Discovery.Multicast.Group; group != "" {
		if ip := net.ParseIP(group); ip == nil || !ip.IsMulticast() {
			v.addError(KeyDiscoveryMulticastGroup, fmt.Sprintf("multicast group must be a multicast IP address, got %q", group))
		}
	}
}
