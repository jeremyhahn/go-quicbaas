// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.

package helpers

import "crypto/tls"

// QUICTLSConfig returns a clone of base for a QUIC client with NextProtos
// cleared, so the SDK QUIC transport chooses its own ALPN (HTTP/3, the
// protocol the node serves). base itself is left unchanged.
func QUICTLSConfig(base *tls.Config) *tls.Config {
	cfg := base.Clone()
	if cfg == nil {
		cfg = &tls.Config{MinVersion: tls.VersionTLS13}
	}
	cfg.NextProtos = nil
	return cfg
}
