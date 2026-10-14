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

package main

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// addressFlagKeys maps each address flag to the config key it sets.
var addressFlagKeys = map[string]string{
	"host":                                 config.KeyServerHost,
	"raft-enrollment-acme-challenge-port":  config.KeyRaftEnrollmentACMEChallengePort,
	"raft-enrollment-advertise-noise-port": config.KeyRaftEnrollmentAdvertiseNoisePort,
	"rag-embedding-api-url":                config.KeyRAGEmbeddingAPIURL,
	"cache-l2-redis-addr":                  config.KeyCacheL2RedisAddr,
	"discovery-multicast-group":            config.KeyDiscoveryMulticastGroup,
}

// addressFlags holds the address flags. The start, node and cluster commands
// share its flag values, so one viper binding per key serves whichever
// command parsed them.
var addressFlags = newAddressFlags()

// newAddressFlags builds the address flag set. Each default is the
// defaults-layer value, so an unset flag never masks a lower layer with a
// different value.
func newAddressFlags() *pflag.FlagSet {
	defaults := config.Default()
	fs := pflag.NewFlagSet("address", pflag.ContinueOnError)
	fs.String("host", defaults.Server.Host,
		"Bind host for the client-facing listeners (server.host, env QUICBAAS_SERVER_HOST)")
	fs.String("raft-enrollment-acme-challenge-port", defaults.Raft.RaftEnrollment.ACME.ChallengePort,
		"Local HTTP port for the ACME enroll-01 exchange (raft.raft_enrollment.acme.challenge_port, default 8080)")
	fs.Int("raft-enrollment-advertise-noise-port", defaults.Raft.RaftEnrollment.AdvertiseNoisePort,
		"TCP port the bootstrapping node serves Noise NK enrollment on (raft.raft_enrollment.advertise_noise_port, 0 = qrdb default)")
	fs.String("rag-embedding-api-url", defaults.RAG.Embedding.APIURL,
		"RAG embedding provider endpoint (rag.embedding.api_url, empty = provider default)")
	fs.String("cache-l2-redis-addr", defaults.Cache.L2RedisAddr,
		"Redis address for the L2 cache (cache.l2_redis_addr)")
	fs.String("discovery-multicast-group", defaults.Discovery.Multicast.Group,
		"Multicast IP for peer discovery (discovery.multicast.group, empty = qrdb default)")
	return fs
}

// addAddressFlags registers the address flags on each command.
func addAddressFlags(cmds ...*cobra.Command) {
	for _, cmd := range cmds {
		cmd.Flags().AddFlagSet(addressFlags)
	}
}

// bindAddressKeys binds every address key to its environment name and its
// flag on v, so v resolves each one as flag > env > file > defaults.
func bindAddressKeys(v *viper.Viper) error {
	config.BindAddressEnv(v)
	for name, key := range addressFlagKeys {
		if err := v.BindPFlag(key, addressFlags.Lookup(name)); err != nil {
			return api.NewFlagBindError("quicbaasd.bindAddressKeys", name, err)
		}
	}
	return nil
}
