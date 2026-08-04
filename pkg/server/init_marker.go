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

package server

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// initDoneMarkerName is the filename of the marker dropped into the data dir
// by `quicbaasd init`. It mirrors the constant of the same name in
// cmd/quicbaasd/init.go. The marker is the authoritative record of the barrier
// strategy (and shamir threshold/total) chosen at init time; the daemon reads
// it at start so the running data-plane barrier matches the init decision.
//
// The constant is duplicated rather than imported because pkg/server must not
// depend on cmd/quicbaasd (the dependency direction is cmd → pkg). The JSON
// shape below is the read-only contract shared by both ends.
const initDoneMarkerName = ".quicbaasd_init_done"

// persistedInitMarker is the read-only view of the init-done marker the daemon
// consults at start. It is a strict subset of cmd/quicbaasd.initDoneMarker —
// only the fields the data-plane barrier needs to honor the init decision are
// declared. JSON tags MUST match the writer in cmd/quicbaasd/init.go.
type persistedInitMarker struct {
	BarrierStrategy string `json:"barrier_strategy"`
	Shares          int    `json:"shares"`
	Threshold       int    `json:"threshold"`
}

// readInitMarker loads the persisted init-done marker from dataDir. It returns
// (nil, nil) when no marker is present (a fresh, never-initialised data dir),
// so callers can fall back to the config default. A malformed marker surfaces
// a DataPlaneBarrierError so the operator sees a precise failure rather than a
// silent fall-through to the wrong strategy.
func readInitMarker(dataDir string) (*persistedInitMarker, error) {
	if dataDir == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(filepath.Join(dataDir, initDoneMarkerName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, &DataPlaneBarrierError{Stage: "read init marker", Cause: err}
	}
	var marker persistedInitMarker
	if err := json.Unmarshal(raw, &marker); err != nil {
		return nil, &DataPlaneBarrierError{Stage: "parse init marker", Cause: err}
	}
	return &marker, nil
}

// resolveBarrierStrategy returns the barrier strategy parameters the data-plane
// barrier must use, honoring the precedence:
//
//  1. Explicit operator override (operatorOverride == true) — the config /env
//     value in cfg.Seal wins unconditionally. This is the escape hatch for an
//     operator who deliberately runs `quicbaasd start` against a data dir whose
//     init decision they want to supersede.
//  2. Persisted init marker — when no explicit override is present and the data
//     dir holds a `.quicbaasd_init_done` marker, the strategy (and shamir
//     threshold/total) recorded at init time wins. This is the common path that
//     makes `quicbaasd init --barrier-strategy none` followed by
//     `quicbaasd start` come up with the none strategy rather than the
//     "passphrase" config default.
//  3. Config default — a fresh data dir with no marker and no override uses
//     cfg.Seal as-is (DefaultSealConfig => "passphrase").
//
// The returned values are (strategyID, threshold, totalShares). threshold/total
// are only meaningful for the shamir strategy; for none/passphrase they are
// returned unchanged from cfg so non-shamir callers can ignore them.
func resolveBarrierStrategy(
	cfg *config.Config,
	operatorOverride bool,
) (qrdbsdk.StrategyID, int, int, error) {
	// Resolve the configured strategy, applying the P4 hardware-prefer
	// auto-detect policy for the "auto" sentinel (resolveEffectiveSealStrategy
	// returns the empty/passphrase default and every explicit value unchanged).
	cfgStrategy := string(resolveEffectiveSealStrategy(cfg, nil))
	threshold := cfg.Seal.Threshold
	totalShares := cfg.Seal.TotalShares

	// (1) Explicit operator override short-circuits the marker lookup.
	if operatorOverride {
		return qrdbsdk.StrategyID(cfgStrategy), threshold, totalShares, nil
	}

	// (2) Consult the persisted init marker.
	marker, err := readInitMarker(cfg.Storage.DataDir)
	if err != nil {
		return "", 0, 0, err
	}
	if marker != nil && marker.BarrierStrategy != "" {
		strategy := normalizeBarrierStrategy(marker.BarrierStrategy)
		if strategy == qrdbsdk.StrategyShamir {
			if marker.Threshold > 0 {
				threshold = marker.Threshold
			}
			if marker.Shares > 0 {
				totalShares = marker.Shares
			}
		}
		return strategy, threshold, totalShares, nil
	}

	// (3) No marker, no override: fall back to the config default.
	return qrdbsdk.StrategyID(cfgStrategy), threshold, totalShares, nil
}

// normalizeBarrierStrategy maps the init CLI's strategy aliases onto the
// canonical qrdb StrategyID. The init flag accepts "disabled" as a synonym for
// "none"; every other value passes through verbatim (validated upstream by the
// init flag validator and again by the qrdb strategy builder).
func normalizeBarrierStrategy(strategy string) qrdbsdk.StrategyID {
	if strategy == "disabled" {
		return qrdbsdk.StrategyNone
	}
	return qrdbsdk.StrategyID(strategy)
}
