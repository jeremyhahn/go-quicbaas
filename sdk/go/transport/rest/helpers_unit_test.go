// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package rest

import "testing"

// TestExtractStringField covers every branch of the reflective string-field
// extractor: nil, nil pointer, non-struct, present field, missing field and
// wrong-kind field.
func TestExtractStringField(t *testing.T) {
	type sample struct {
		Path string
		Num  int
	}

	t.Run("present", func(t *testing.T) {
		v, ok := extractStringField(&sample{Path: "p"}, "Path")
		if !ok || v != "p" {
			t.Errorf("expected (p,true), got (%q,%v)", v, ok)
		}
	})
	t.Run("value struct", func(t *testing.T) {
		v, ok := extractStringField(sample{Path: "q"}, "Path")
		if !ok || v != "q" {
			t.Errorf("expected (q,true), got (%q,%v)", v, ok)
		}
	})
	t.Run("nil interface", func(t *testing.T) {
		if _, ok := extractStringField(nil, "Path"); ok {
			t.Error("expected false for nil")
		}
	})
	t.Run("nil pointer", func(t *testing.T) {
		var p *sample
		if _, ok := extractStringField(p, "Path"); ok {
			t.Error("expected false for nil pointer")
		}
	})
	t.Run("non-struct", func(t *testing.T) {
		if _, ok := extractStringField("just a string", "Path"); ok {
			t.Error("expected false for non-struct")
		}
	})
	t.Run("missing field", func(t *testing.T) {
		if _, ok := extractStringField(&sample{}, "Nope"); ok {
			t.Error("expected false for missing field")
		}
	})
	t.Run("wrong kind", func(t *testing.T) {
		if _, ok := extractStringField(&sample{Num: 1}, "Num"); ok {
			t.Error("expected false for non-string field")
		}
	})
}

// TestExtractIntField covers signed, unsigned, missing, wrong-kind, nil and
// non-struct cases of the reflective integer-field extractor.
func TestExtractIntField(t *testing.T) {
	type sample struct {
		Signed   int32
		Unsigned uint16
		Text     string
	}

	t.Run("signed", func(t *testing.T) {
		v, ok := extractIntField(&sample{Signed: -5}, "Signed")
		if !ok || v != -5 {
			t.Errorf("expected (-5,true), got (%d,%v)", v, ok)
		}
	})
	t.Run("unsigned", func(t *testing.T) {
		v, ok := extractIntField(&sample{Unsigned: 7}, "Unsigned")
		if !ok || v != 7 {
			t.Errorf("expected (7,true), got (%d,%v)", v, ok)
		}
	})
	t.Run("wrong kind", func(t *testing.T) {
		if _, ok := extractIntField(&sample{Text: "x"}, "Text"); ok {
			t.Error("expected false for string field")
		}
	})
	t.Run("missing", func(t *testing.T) {
		if _, ok := extractIntField(&sample{}, "Nope"); ok {
			t.Error("expected false for missing field")
		}
	})
	t.Run("nil interface", func(t *testing.T) {
		if _, ok := extractIntField(nil, "Signed"); ok {
			t.Error("expected false for nil")
		}
	})
	t.Run("nil pointer", func(t *testing.T) {
		var p *sample
		if _, ok := extractIntField(p, "Signed"); ok {
			t.Error("expected false for nil pointer")
		}
	})
	t.Run("non-struct", func(t *testing.T) {
		if _, ok := extractIntField(42, "Signed"); ok {
			t.Error("expected false for non-struct")
		}
	})
}

// TestExtractKeyIDField verifies the KeyID convenience wrapper.
func TestExtractKeyIDField(t *testing.T) {
	type req struct{ KeyID string }
	if v, ok := extractKeyIDField(&req{KeyID: "k1"}); !ok || v != "k1" {
		t.Errorf("expected (k1,true), got (%q,%v)", v, ok)
	}
	if _, ok := extractKeyIDField(&struct{ Other string }{}); ok {
		t.Error("expected false when KeyID absent")
	}
}

// TestBackupFieldGetters covers the snake_case/PascalCase/absent branches of the
// backup response field accessors.
func TestBackupFieldGetters(t *testing.T) {
	t.Run("getFloat64Field", func(t *testing.T) {
		if v, ok := getFloat64Field(map[string]interface{}{"ratio": 1.5}, "ratio", "Ratio"); !ok || v != 1.5 {
			t.Errorf("snake: got (%v,%v)", v, ok)
		}
		if v, ok := getFloat64Field(map[string]interface{}{"Ratio": 2.5}, "ratio", "Ratio"); !ok || v != 2.5 {
			t.Errorf("pascal: got (%v,%v)", v, ok)
		}
		if _, ok := getFloat64Field(map[string]interface{}{}, "ratio", "Ratio"); ok {
			t.Error("expected false when absent")
		}
	})
	t.Run("getBoolField", func(t *testing.T) {
		if v, ok := getBoolField(map[string]interface{}{"done": true}, "done", "Done"); !ok || !v {
			t.Errorf("snake: got (%v,%v)", v, ok)
		}
		if v, ok := getBoolField(map[string]interface{}{"Done": true}, "done", "Done"); !ok || !v {
			t.Errorf("pascal: got (%v,%v)", v, ok)
		}
		if _, ok := getBoolField(map[string]interface{}{}, "done", "Done"); ok {
			t.Error("expected false when absent")
		}
	})
	t.Run("getStringField", func(t *testing.T) {
		if v, ok := getStringField(map[string]interface{}{"Name": "x"}, "name", "Name"); !ok || v != "x" {
			t.Errorf("pascal: got (%v,%v)", v, ok)
		}
		if _, ok := getStringField(map[string]interface{}{}, "name", "Name"); ok {
			t.Error("expected false when absent")
		}
	})
	t.Run("getUint64Field", func(t *testing.T) {
		if v, ok := getUint64Field(map[string]interface{}{"Size": float64(9)}, "size", "Size"); !ok || v != 9 {
			t.Errorf("pascal: got (%v,%v)", v, ok)
		}
		if _, ok := getUint64Field(map[string]interface{}{}, "size", "Size"); ok {
			t.Error("expected false when absent")
		}
	})
	t.Run("getInt64Field", func(t *testing.T) {
		if v, ok := getInt64Field(map[string]interface{}{"Count": float64(-3)}, "count", "Count"); !ok || v != -3 {
			t.Errorf("pascal: got (%v,%v)", v, ok)
		}
		if _, ok := getInt64Field(map[string]interface{}{}, "count", "Count"); ok {
			t.Error("expected false when absent")
		}
	})
	t.Run("getSliceField", func(t *testing.T) {
		if s := getSliceField(map[string]interface{}{"items": []interface{}{1}}, "items", "Items"); len(s) != 1 {
			t.Errorf("snake: got %v", s)
		}
		if s := getSliceField(map[string]interface{}{"Items": []interface{}{1, 2}}, "items", "Items"); len(s) != 2 {
			t.Errorf("pascal: got %v", s)
		}
		if s := getSliceField(map[string]interface{}{}, "items", "Items"); s != nil {
			t.Errorf("expected nil when absent, got %v", s)
		}
	})
}

// TestPathSegmentHelpers covers splitPathSegments and joinPathSegments.
func TestPathSegmentHelpers(t *testing.T) {
	segs := splitPathSegments("/app/key/sub")
	if len(segs) != 3 || segs[0] != "app" || segs[2] != "sub" {
		t.Fatalf("unexpected split: %v", segs)
	}
	if joined := joinPathSegments(segs); joined != "app/key/sub" {
		t.Errorf("unexpected join: %q", joined)
	}
	// No leading slash.
	if segs := splitPathSegments("single"); len(segs) != 1 || segs[0] != "single" {
		t.Errorf("unexpected single split: %v", segs)
	}
}
