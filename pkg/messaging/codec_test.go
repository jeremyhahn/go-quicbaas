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

package messaging

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// entityWithCodecTag is a test struct with quicbaas codec tag.
type entityWithCodecTag struct {
	ID   string `json:"id" quicbaas:"codec=msgpack"`
	Name string `json:"name"`
}

// entityWithoutCodecTag is a test struct without codec tag.
type entityWithoutCodecTag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// entityWithEmptyTag has empty quicbaas tag.
type entityWithEmptyTag struct {
	ID string `json:"id" quicbaas:""`
}

// entityWithOtherTag has quicbaas tag but not codec.
type entityWithOtherTag struct {
	ID string `json:"id" quicbaas:"index=primary"`
}

func newCodecTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "console",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	return log
}

func TestNewCodecResolver(t *testing.T) {
	t.Parallel()

	t.Run("creates resolver with default codec", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		if r == nil {
			t.Fatal("expected non-nil CodecResolver")
		}
		if r.DefaultCodecName() != CodecJSON {
			t.Errorf("expected default codec %q, got %q", CodecJSON, r.DefaultCodecName())
		}
	})

	t.Run("creates resolver with custom default codec", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log, WithResolverDefaultCodec(CodecMsgpack))

		if r.DefaultCodecName() != CodecMsgpack {
			t.Errorf("expected default codec %q, got %q", CodecMsgpack, r.DefaultCodecName())
		}
	})

	t.Run("creates resolver with custom logger", func(t *testing.T) {
		t.Parallel()
		defaultLog := newCodecTestLogger(t)
		customLog := newCodecTestLogger(t)

		r := NewCodecResolver(defaultLog, WithResolverLogger(customLog))

		// The resolver should use the custom logger
		if r.logger != customLog {
			t.Error("expected custom logger to be set")
		}
	})

	t.Run("registers built-in codecs", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		codecs := r.ListCodecs()
		foundJSON := false
		foundMsgpack := false
		for _, name := range codecs {
			if name == CodecJSON {
				foundJSON = true
			}
			if name == CodecMsgpack {
				foundMsgpack = true
			}
		}

		if !foundJSON {
			t.Error("expected JSON codec to be registered")
		}
		if !foundMsgpack {
			t.Error("expected msgpack codec to be registered")
		}
	})
}

func TestCodecResolver_GetCodec(t *testing.T) {
	t.Parallel()

	t.Run("returns JSON codec", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		codec, err := r.GetCodec(CodecJSON)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if codec.Name() != CodecJSON {
			t.Errorf("expected codec name %q, got %q", CodecJSON, codec.Name())
		}
	})

	t.Run("returns msgpack codec", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		codec, err := r.GetCodec(CodecMsgpack)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if codec.Name() != CodecMsgpack {
			t.Errorf("expected codec name %q, got %q", CodecMsgpack, codec.Name())
		}
	})

	t.Run("returns error for unknown codec", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		_, err := r.GetCodec("unknown")
		if err == nil {
			t.Error("expected error for unknown codec")
		}
	})

	t.Run("returns error for empty name", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		_, err := r.GetCodec("")
		if err == nil {
			t.Error("expected error for empty codec name")
		}
	})
}

func TestCodecResolver_RegisterCodec(t *testing.T) {
	t.Parallel()

	t.Run("registers custom codec", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		customCodec := &jsonPushCodec{} // Just reuse JSON codec for testing
		// Register with a different name to simulate custom codec
		err := r.RegisterCodec(&testCodec{name: "custom", base: customCodec})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		codec, err := r.GetCodec("custom")
		if err != nil {
			t.Fatalf("unexpected error getting custom codec: %v", err)
		}
		if codec.Name() != "custom" {
			t.Errorf("expected codec name 'custom', got %q", codec.Name())
		}
	})

	t.Run("returns error for nil codec", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		err := r.RegisterCodec(nil)
		if err == nil {
			t.Error("expected error for nil codec")
		}
	})

	t.Run("returns error for codec with empty name", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		err := r.RegisterCodec(&testCodec{name: ""})
		if err == nil {
			t.Error("expected error for codec with empty name")
		}
	})
}

type testCodec struct {
	name string
	base CodecInterface
}

func (c *testCodec) Name() string { return c.name }
func (c *testCodec) Encode(entity interface{}) ([]byte, error) {
	if c.base != nil {
		return c.base.Encode(entity)
	}
	return nil, nil
}
func (c *testCodec) Decode(data []byte, entity interface{}) error {
	if c.base != nil {
		return c.base.Decode(data, entity)
	}
	return nil
}

func TestCodecResolver_ResolveCodec(t *testing.T) {
	t.Parallel()

	t.Run("resolves message codec first", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		codec, err := r.ResolveCodec(CodecMsgpack, CodecJSON, CodecJSON, CodecJSON)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if codec.Name() != CodecMsgpack {
			t.Errorf("expected message codec %q, got %q", CodecMsgpack, codec.Name())
		}
	})

	t.Run("resolves subscription codec second", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		codec, err := r.ResolveCodec("", CodecMsgpack, CodecJSON, CodecJSON)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if codec.Name() != CodecMsgpack {
			t.Errorf("expected subscription codec %q, got %q", CodecMsgpack, codec.Name())
		}
	})

	t.Run("resolves channel codec third", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		codec, err := r.ResolveCodec("", "", CodecMsgpack, CodecJSON)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if codec.Name() != CodecMsgpack {
			t.Errorf("expected channel codec %q, got %q", CodecMsgpack, codec.Name())
		}
	})

	t.Run("resolves session codec fourth", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		codec, err := r.ResolveCodec("", "", "", CodecMsgpack)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if codec.Name() != CodecMsgpack {
			t.Errorf("expected session codec %q, got %q", CodecMsgpack, codec.Name())
		}
	})

	t.Run("falls back to default codec", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		codec, err := r.ResolveCodec("", "", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if codec.Name() != CodecJSON {
			t.Errorf("expected default codec %q, got %q", CodecJSON, codec.Name())
		}
	})

	t.Run("skips unavailable codec and continues", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		codec, err := r.ResolveCodec("nonexistent", CodecMsgpack, "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if codec.Name() != CodecMsgpack {
			t.Errorf("expected fallback codec %q, got %q", CodecMsgpack, codec.Name())
		}
	})
}

func TestCodecResolver_DetectCodec(t *testing.T) {
	t.Parallel()

	t.Run("detects codec from struct tag", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		codecName, err := r.DetectCodec(&entityWithCodecTag{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if codecName != CodecMsgpack {
			t.Errorf("expected codec %q, got %q", CodecMsgpack, codecName)
		}
	})

	t.Run("returns empty for struct without codec tag", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		codecName, err := r.DetectCodec(&entityWithoutCodecTag{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if codecName != "" {
			t.Errorf("expected empty codec name, got %q", codecName)
		}
	})

	t.Run("returns empty for struct with empty tag", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		codecName, err := r.DetectCodec(&entityWithEmptyTag{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if codecName != "" {
			t.Errorf("expected empty codec name, got %q", codecName)
		}
	})

	t.Run("returns empty for struct with other tag", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		codecName, err := r.DetectCodec(&entityWithOtherTag{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if codecName != "" {
			t.Errorf("expected empty codec name, got %q", codecName)
		}
	})

	t.Run("returns error for nil entity", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		_, err := r.DetectCodec(nil)
		if err == nil {
			t.Error("expected error for nil entity")
		}
	})

	t.Run("returns empty for non-struct type", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		codecName, err := r.DetectCodec("string value")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if codecName != "" {
			t.Errorf("expected empty codec name for non-struct, got %q", codecName)
		}
	})
}

func TestCodecResolver_Encode(t *testing.T) {
	t.Parallel()

	t.Run("encodes with specified codec", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		entity := &entityWithoutCodecTag{ID: "1", Name: "Test"}
		data, err := r.Encode(entity, CodecJSON)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(data) == 0 {
			t.Error("expected non-empty encoded data")
		}
	})

	t.Run("encodes with default codec when name is empty", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		entity := &entityWithoutCodecTag{ID: "1", Name: "Test"}
		data, err := r.Encode(entity, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(data) == 0 {
			t.Error("expected non-empty encoded data")
		}
	})

	t.Run("returns error for nil entity", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		_, err := r.Encode(nil, CodecJSON)
		if err == nil {
			t.Error("expected error for nil entity")
		}
	})
}

func TestCodecResolver_Decode(t *testing.T) {
	t.Parallel()

	t.Run("decodes with specified codec", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		data := []byte(`{"id":"1","name":"Test"}`)
		entity := &entityWithoutCodecTag{}
		err := r.Decode(data, entity, CodecJSON)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if entity.ID != "1" || entity.Name != "Test" {
			t.Errorf("unexpected decoded values: %+v", entity)
		}
	})

	t.Run("decodes with default codec when name is empty", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		data := []byte(`{"id":"2","name":"Default"}`)
		entity := &entityWithoutCodecTag{}
		err := r.Decode(data, entity, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if entity.ID != "2" {
			t.Errorf("expected ID '2', got %q", entity.ID)
		}
	})

	t.Run("returns error for empty data", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		entity := &entityWithoutCodecTag{}
		err := r.Decode(nil, entity, CodecJSON)
		if err == nil {
			t.Error("expected error for empty data")
		}
	})

	t.Run("returns error for nil entity", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		err := r.Decode([]byte(`{}`), nil, CodecJSON)
		if err == nil {
			t.Error("expected error for nil entity")
		}
	})
}

func TestCodecResolver_SetDefaultCodec(t *testing.T) {
	t.Parallel()

	t.Run("changes default codec", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		err := r.SetDefaultCodec(CodecMsgpack)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r.DefaultCodecName() != CodecMsgpack {
			t.Errorf("expected default codec %q, got %q", CodecMsgpack, r.DefaultCodecName())
		}
	})

	t.Run("returns error for empty name", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		err := r.SetDefaultCodec("")
		if err == nil {
			t.Error("expected error for empty codec name")
		}
	})

	t.Run("returns error for unregistered codec", func(t *testing.T) {
		t.Parallel()
		log := newCodecTestLogger(t)
		r := NewCodecResolver(log)

		err := r.SetDefaultCodec("nonexistent")
		if err == nil {
			t.Error("expected error for unregistered codec")
		}
	})
}

func TestJSONPushCodec(t *testing.T) {
	t.Parallel()

	t.Run("name returns json", func(t *testing.T) {
		t.Parallel()
		codec := &jsonPushCodec{}
		if codec.Name() != CodecJSON {
			t.Errorf("expected name %q, got %q", CodecJSON, codec.Name())
		}
	})

	t.Run("encode and decode roundtrip", func(t *testing.T) {
		t.Parallel()
		codec := &jsonPushCodec{}

		original := &entityWithoutCodecTag{ID: "test-id", Name: "Test Name"}
		data, err := codec.Encode(original)
		if err != nil {
			t.Fatalf("encode error: %v", err)
		}

		decoded := &entityWithoutCodecTag{}
		err = codec.Decode(data, decoded)
		if err != nil {
			t.Fatalf("decode error: %v", err)
		}

		if decoded.ID != original.ID || decoded.Name != original.Name {
			t.Errorf("roundtrip failed: got %+v, want %+v", decoded, original)
		}
	})

	t.Run("encode returns error for nil", func(t *testing.T) {
		t.Parallel()
		codec := &jsonPushCodec{}

		_, err := codec.Encode(nil)
		if err == nil {
			t.Error("expected error for nil entity")
		}
	})

	t.Run("decode returns error for empty data", func(t *testing.T) {
		t.Parallel()
		codec := &jsonPushCodec{}

		entity := &entityWithoutCodecTag{}
		err := codec.Decode(nil, entity)
		if err == nil {
			t.Error("expected error for empty data")
		}
	})

	t.Run("decode returns error for nil entity", func(t *testing.T) {
		t.Parallel()
		codec := &jsonPushCodec{}

		err := codec.Decode([]byte(`{}`), nil)
		if err == nil {
			t.Error("expected error for nil entity")
		}
	})

	t.Run("decode returns error for non-pointer", func(t *testing.T) {
		t.Parallel()
		codec := &jsonPushCodec{}

		var entity entityWithoutCodecTag
		err := codec.Decode([]byte(`{}`), entity)
		if err == nil {
			t.Error("expected error for non-pointer entity")
		}
	})
}

func TestMsgpackPushCodec(t *testing.T) {
	t.Parallel()

	t.Run("name returns msgpack", func(t *testing.T) {
		t.Parallel()
		codec := &msgpackPushCodec{}
		if codec.Name() != CodecMsgpack {
			t.Errorf("expected name %q, got %q", CodecMsgpack, codec.Name())
		}
	})

	t.Run("encode and decode roundtrip", func(t *testing.T) {
		t.Parallel()
		codec := &msgpackPushCodec{}

		original := &entityWithoutCodecTag{ID: "msgpack-id", Name: "Msgpack Name"}
		data, err := codec.Encode(original)
		if err != nil {
			t.Fatalf("encode error: %v", err)
		}

		decoded := &entityWithoutCodecTag{}
		err = codec.Decode(data, decoded)
		if err != nil {
			t.Fatalf("decode error: %v", err)
		}

		if decoded.ID != original.ID || decoded.Name != original.Name {
			t.Errorf("roundtrip failed: got %+v, want %+v", decoded, original)
		}
	})

	t.Run("encode returns error for nil", func(t *testing.T) {
		t.Parallel()
		codec := &msgpackPushCodec{}

		_, err := codec.Encode(nil)
		if err == nil {
			t.Error("expected error for nil entity")
		}
	})

	t.Run("decode returns error for empty data", func(t *testing.T) {
		t.Parallel()
		codec := &msgpackPushCodec{}

		entity := &entityWithoutCodecTag{}
		err := codec.Decode(nil, entity)
		if err == nil {
			t.Error("expected error for empty data")
		}
	})

	t.Run("decode returns error for nil entity", func(t *testing.T) {
		t.Parallel()
		codec := &msgpackPushCodec{}

		err := codec.Decode([]byte{0x80}, nil)
		if err == nil {
			t.Error("expected error for nil entity")
		}
	})

	t.Run("decode returns error for non-pointer", func(t *testing.T) {
		t.Parallel()
		codec := &msgpackPushCodec{}

		var entity entityWithoutCodecTag
		err := codec.Decode([]byte{0x80}, entity)
		if err == nil {
			t.Error("expected error for non-pointer entity")
		}
	})
}

func TestCodecErrorTypes(t *testing.T) {
	t.Parallel()

	t.Run("NilCodecError message", func(t *testing.T) {
		t.Parallel()
		err := &NilCodecError{}
		if err.Error() != "codec is nil" {
			t.Errorf("unexpected error message: %q", err.Error())
		}
	})

	t.Run("EmptyCodecNameError message", func(t *testing.T) {
		t.Parallel()
		err := &EmptyCodecNameError{}
		if err.Error() != "codec name is empty" {
			t.Errorf("unexpected error message: %q", err.Error())
		}
	})

	t.Run("NilCodecNameError message", func(t *testing.T) {
		t.Parallel()
		err := &NilCodecNameError{}
		if err.Error() != "default codec name is nil or invalid" {
			t.Errorf("unexpected error message: %q", err.Error())
		}
	})

	t.Run("NilEntityError message", func(t *testing.T) {
		t.Parallel()
		err := &NilEntityError{}
		if err.Error() != "entity is nil" {
			t.Errorf("unexpected error message: %q", err.Error())
		}
	})

	t.Run("EmptyDataError message", func(t *testing.T) {
		t.Parallel()
		err := &EmptyDataError{}
		if err.Error() != "data is empty" {
			t.Errorf("unexpected error message: %q", err.Error())
		}
	})

	t.Run("CodecTypeAssertionError message", func(t *testing.T) {
		t.Parallel()
		err := &CodecTypeAssertionError{Name: "test"}
		expected := "codec type assertion failed for: test"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("DefaultCodecNotFoundError message", func(t *testing.T) {
		t.Parallel()
		err := &DefaultCodecNotFoundError{Name: "missing"}
		expected := "default codec not found: missing"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("NotPointerError message", func(t *testing.T) {
		t.Parallel()
		err := &NotPointerError{Type: "string"}
		expected := "entity must be a pointer, got: string"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("EncodeError message with cause", func(t *testing.T) {
		t.Parallel()
		cause := &NilEntityError{}
		err := &EncodeError{Codec: "json", Cause: cause}
		expected := "failed to encode with json: entity is nil"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
		if err.Unwrap() != cause {
			t.Error("Unwrap should return cause")
		}
	})

	t.Run("EncodeError message without cause", func(t *testing.T) {
		t.Parallel()
		err := &EncodeError{Codec: "json"}
		expected := "failed to encode with json"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("DecodeError message with cause", func(t *testing.T) {
		t.Parallel()
		cause := &EmptyDataError{}
		err := &DecodeError{Codec: "msgpack", Cause: cause}
		expected := "failed to decode with msgpack: data is empty"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
		if err.Unwrap() != cause {
			t.Error("Unwrap should return cause")
		}
	})

	t.Run("DecodeError message without cause", func(t *testing.T) {
		t.Parallel()
		err := &DecodeError{Codec: "msgpack"}
		expected := "failed to decode with msgpack"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})
}
