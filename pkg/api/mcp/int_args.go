//go:build mcp

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

package mcp

import (
	"strconv"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

// uint64Arg reads key as a uint64. An absent argument, or one of a
// non-numeric type, reads as zero. A negative number, a number of 2^64 or
// more, or a string that is not a base-10 uint64 is refused.
func uint64Arg(args map[string]interface{}, key string) (uint64, error) {
	switch v := args[key].(type) {
	case float64:
		return conv.SafeFloat64ToUint64(v)
	case int:
		return conv.SafeIntToUint64(v)
	case int64:
		return conv.SafeInt64ToUint64(v)
	case uint64:
		return v, nil
	case string:
		if v == "" {
			return 0, nil
		}
		return strconv.ParseUint(v, 10, 64)
	}
	return 0, nil
}

// newIntArgError is the invalid-input error for a whole-number tool
// argument that cannot be read into the type its request field uses.
func newIntArgError(key string, cause error) error {
	return api.NewInvalidInputCauseError("invalid "+key, cause)
}

// intArgReader reads whole-number tool arguments into the integer types the
// service requests use. A value the target type cannot hold is refused as
// invalid input, never narrowed by a cast. The reader keeps the first
// refusal, so a handler reads every argument and then checks Err once.
type intArgReader struct {
	args map[string]interface{}
	err  error
}

func newIntArgReader(args map[string]interface{}) *intArgReader {
	return &intArgReader{args: args}
}

// Uint64 reads key as a uint64; an absent argument reads as zero.
func (r *intArgReader) Uint64(key string) uint64 {
	n, err := uint64Arg(r.args, key)
	r.keep(key, err)
	return n
}

// Int reads key as an int; an absent argument reads as zero.
func (r *intArgReader) Int(key string) int {
	n, err := uint64Arg(r.args, key)
	if err != nil {
		r.keep(key, err)
		return 0
	}
	v, err := conv.SafeUint64ToInt(n)
	r.keep(key, err)
	return v
}

// IntOr reads key as an int, or returns def when the argument is absent
// or zero.
func (r *intArgReader) IntOr(key string, def int) int {
	if v := r.Int(key); v != 0 {
		return v
	}
	return def
}

// Int64 reads key as an int64; an absent argument reads as zero.
func (r *intArgReader) Int64(key string) int64 {
	n, err := uint64Arg(r.args, key)
	if err != nil {
		r.keep(key, err)
		return 0
	}
	v, err := conv.SafeUint64ToInt64(n)
	r.keep(key, err)
	return v
}

// Err returns the first argument the reader refused, or nil.
func (r *intArgReader) Err() error {
	return r.err
}

func (r *intArgReader) keep(key string, err error) {
	if err != nil && r.err == nil {
		r.err = newIntArgError(key, err)
	}
}

// cappedIntArg reads key as an int no greater than limit. Saturation is the
// page-size contract of the list tools: a request for more than the cap,
// however large, receives the cap. An argument that is not a whole number
// zero or more is refused.
func cappedIntArg(args map[string]interface{}, key string, limit int) (int, error) {
	n, err := uint64Arg(args, key)
	if err != nil {
		return 0, newIntArgError(key, err)
	}
	return min(conv.ClampUint64ToInt(n), limit), nil
}
