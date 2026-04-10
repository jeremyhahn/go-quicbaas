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

package storage

import (
	"encoding/base64"
	"errors"
	"testing"
)

// iterator that supports Error injection and simple data.
type spIter struct {
	data []KV
	i    int
	err  error
}

func newSPIter(kvs []KV) *spIter { return &spIter{data: kvs, i: -1} }
func (m *spIter) SeekToFirst() bool {
	if len(m.data) == 0 {
		return false
	}
	m.i = 0
	return true
}

func (m *spIter) SeekToLast() bool {
	if len(m.data) == 0 {
		return false
	}
	m.i = len(m.data) - 1
	return true
}

func (m *spIter) Seek(key []byte) bool {
	for idx, kv := range m.data {
		if string(kv.Key) >= string(key) {
			m.i = idx
			return true
		}
	}
	return false
}

func (m *spIter) Next() bool {
	if m.i+1 >= len(m.data) {
		return false
	}
	m.i++
	return true
}

func (m *spIter) Prev() bool {
	if m.i-1 < 0 {
		return false
	}
	m.i--
	return true
}
func (m *spIter) Valid() bool   { return m.i >= 0 && m.i < len(m.data) }
func (m *spIter) Key() []byte   { return m.data[m.i].Key }
func (m *spIter) Value() []byte { return m.data[m.i].Value }
func (m *spIter) Error() error  { return m.err }
func (m *spIter) Close() error  { return nil }

func TestStreamPage_BasicAndCursor(t *testing.T) {
	kvs := []KV{{[]byte("a"), []byte("1")}, {[]byte("b"), []byte("2")}, {[]byte("c"), []byte("3")}}
	// full page should yield nil next cursor
	it := newSPIter(kvs)
	next, err := StreamPage(it, 3, nil, func(k, v []byte) bool { return true })
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if next == nil {
		t.Fatalf("expected a cursor even after full page per implementation")
	}
	dec0 := make([]byte, base64.StdEncoding.DecodedLen(len(next)))
	n0, derr0 := base64.StdEncoding.Decode(dec0, next)
	if derr0 != nil {
		t.Fatalf("decode err: %v", derr0)
	}
	if string(dec0[:n0]) != "c" {
		t.Fatalf("expected cursor 'c', got %q", string(dec0[:n0]))
	}

	// partial page should yield next cursor pointing to next key
	it2 := newSPIter(kvs)
	next2, err := StreamPage(it2, 2, nil, func(k, v []byte) bool { return true })
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if next2 == nil {
		t.Fatalf("expected next cursor")
	}
	dec := make([]byte, base64.StdEncoding.DecodedLen(len(next2)))
	n, derr := base64.StdEncoding.Decode(dec, next2)
	if derr != nil {
		t.Fatalf("decode err: %v", derr)
	}
	if string(dec[:n]) != "c" {
		t.Fatalf("expected cursor 'c', got %q", string(dec[:n]))
	}
}

func TestStreamPage_EarlyStop(t *testing.T) {
	kvs := []KV{{[]byte("a"), []byte("1")}, {[]byte("b"), []byte("2")}, {[]byte("c"), []byte("3")}}
	it := newSPIter(kvs)
	count := 0
	next, err := StreamPage(it, 10, nil, func(k, v []byte) bool {
		count++
		return false // stop after first item
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected processed 1, got %d", count)
	}
	if next == nil {
		t.Fatalf("expected non-nil next cursor for remaining items")
	}
}

func TestStreamPage_ErrorPropagation(t *testing.T) {
	kvs := []KV{{[]byte("a"), []byte("1")}}
	it := newSPIter(kvs)
	// advance one item so Next() will stop; then inject error
	_ = it.SeekToFirst()
	it.err = errors.New("boom")
	_, err := StreamPage(it, 1, nil, func(k, v []byte) bool { return true })
	if err == nil {
		t.Fatalf("expected error from iterator")
	}
}

func TestPageIterator_Close_NilIter(t *testing.T) {
	pi := &PageIterator{}
	if err := pi.Close(); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestStreamPage_InvalidCursorAndSeekMiss(t *testing.T) {
	kvs := []KV{{[]byte("a"), []byte("1")}}
	it := newSPIter(kvs)
	// invalid base64
	if _, err := StreamPage(it, 10, []byte("!!!"), func(k, v []byte) bool { return true }); err == nil {
		t.Fatalf("expected error for invalid cursor")
	}

	// seek miss from cursor beyond range
	it2 := newSPIter(kvs)
	next, err := StreamPage(it2, 10, encodeCursor([]byte("z")), func(k, v []byte) bool { return true })
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if next != nil {
		t.Fatalf("expected nil cursor when nothing iterated")
	}
}
