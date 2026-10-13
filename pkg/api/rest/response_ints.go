//go:build rest

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

package rest

import (
	"math/bits"
	"strconv"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

// responseInt64s converts uint64 values from a service response to the int64
// fields of a REST response. It keeps the first value that does not fit, so
// a handler can build the whole response and check once with Err.
type responseInt64s struct {
	op  string
	err error
}

// newResponseInt64s returns a converter whose errors name op.
func newResponseInt64s(op string) *responseInt64s {
	return &responseInt64s{op: op}
}

// Int64 returns v as an int64. A value beyond int64 records an internal
// error naming subject and returns 0; after the first failure every call
// returns 0.
func (c *responseInt64s) Int64(subject string, v uint64) int64 {
	if c.err != nil {
		return 0
	}
	n, err := conv.SafeUint64ToInt64(v)
	if err != nil {
		c.err = api.NewEncodeError(c.op, subject, err)
		return 0
	}
	return n
}

// Add returns a+b. A sum that wraps uint64 records an internal error naming
// subject, since a wrapped total would be reported as a small, wrong number.
func (c *responseInt64s) Add(subject string, a, b uint64) uint64 {
	sum, carry := bits.Add64(a, b, 0)
	if carry != 0 && c.err == nil {
		c.err = api.NewEncodeError(c.op, subject, &conv.ConversionOverflowError{
			FromType: "uint64 sum",
			ToType:   "uint64",
			Value:    strconv.FormatUint(a, 10) + " + " + strconv.FormatUint(b, 10),
		})
	}
	return sum
}

// Err returns the first conversion failure, or nil.
func (c *responseInt64s) Err() error {
	return c.err
}
