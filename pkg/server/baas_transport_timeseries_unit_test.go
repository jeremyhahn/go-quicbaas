//go:build !integration

package server

import (
	"strings"
	"testing"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// tsBuildKey
// ---------------------------------------------------------------------------

func TestTsBuildKey(t *testing.T) {
	t.Run("builds key with correct prefix and metric", func(t *testing.T) {
		key := tsBuildKey("cpu.usage", 1000000, nil)
		assert.True(t, strings.HasPrefix(key, tsKVPrefix+"cpu.usage:"))
	})

	t.Run("different timestamps produce different keys", func(t *testing.T) {
		k1 := tsBuildKey("m", 100, nil)
		k2 := tsBuildKey("m", 200, nil)
		assert.NotEqual(t, k1, k2)
	})

	t.Run("different tags produce different keys", func(t *testing.T) {
		k1 := tsBuildKey("m", 100, map[string]string{"host": "a"})
		k2 := tsBuildKey("m", 100, map[string]string{"host": "b"})
		assert.NotEqual(t, k1, k2)
	})
}

// ---------------------------------------------------------------------------
// tsHexAppend
// ---------------------------------------------------------------------------

func TestTsHexAppend(t *testing.T) {
	t.Run("encodes bytes as hex", func(t *testing.T) {
		var buf strings.Builder
		tsHexAppend(&buf, []byte{0xab, 0xcd, 0x01})
		assert.Equal(t, "abcd01", buf.String())
	})

	t.Run("empty input produces empty output", func(t *testing.T) {
		var buf strings.Builder
		tsHexAppend(&buf, nil)
		assert.Equal(t, "", buf.String())
	})
}

// ---------------------------------------------------------------------------
// tsHashTags
// ---------------------------------------------------------------------------

func TestTsHashTags(t *testing.T) {
	t.Run("nil tags return 0", func(t *testing.T) {
		assert.Equal(t, uint64(0), tsHashTags(nil))
	})

	t.Run("empty tags return 0", func(t *testing.T) {
		assert.Equal(t, uint64(0), tsHashTags(map[string]string{}))
	})

	t.Run("same tags in different order produce same hash", func(t *testing.T) {
		tags1 := map[string]string{"a": "1", "b": "2"}
		tags2 := map[string]string{"b": "2", "a": "1"}
		assert.Equal(t, tsHashTags(tags1), tsHashTags(tags2))
	})

	t.Run("different tags produce different hashes", func(t *testing.T) {
		h1 := tsHashTags(map[string]string{"host": "server1"})
		h2 := tsHashTags(map[string]string{"host": "server2"})
		assert.NotEqual(t, h1, h2)
	})
}

// ---------------------------------------------------------------------------
// tsTagsToMap / tsMapToTags
// ---------------------------------------------------------------------------

func TestTsTagsToMap(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		assert.Nil(t, tsTagsToMap(nil))
	})

	t.Run("empty returns nil", func(t *testing.T) {
		assert.Nil(t, tsTagsToMap([]transport.TSTag{}))
	})

	t.Run("converts tags to map", func(t *testing.T) {
		tags := []transport.TSTag{
			{Key: "host", Value: "server1"},
			{Key: "region", Value: "us-east"},
		}
		m := tsTagsToMap(tags)
		assert.Equal(t, "server1", m["host"])
		assert.Equal(t, "us-east", m["region"])
	})
}

func TestTsMapToTags(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		assert.Nil(t, tsMapToTags(nil))
	})

	t.Run("empty returns nil", func(t *testing.T) {
		assert.Nil(t, tsMapToTags(map[string]string{}))
	})

	t.Run("converts map to sorted tags", func(t *testing.T) {
		m := map[string]string{"z": "last", "a": "first"}
		tags := tsMapToTags(m)
		require.Len(t, tags, 2)
		assert.Equal(t, "a", tags[0].Key)
		assert.Equal(t, "z", tags[1].Key)
	})
}

// ---------------------------------------------------------------------------
// tsTagsMatch
// ---------------------------------------------------------------------------

func TestTsTagsMatch(t *testing.T) {
	point := map[string]string{"host": "s1", "region": "us"}

	t.Run("matches when all required tags present", func(t *testing.T) {
		required := []transport.TSTag{{Key: "host", Value: "s1"}}
		assert.True(t, tsTagsMatch(point, required))
	})

	t.Run("fails when required tag missing", func(t *testing.T) {
		required := []transport.TSTag{{Key: "dc", Value: "dc1"}}
		assert.False(t, tsTagsMatch(point, required))
	})

	t.Run("fails when value mismatch", func(t *testing.T) {
		required := []transport.TSTag{{Key: "host", Value: "s2"}}
		assert.False(t, tsTagsMatch(point, required))
	})

	t.Run("empty required always matches", func(t *testing.T) {
		assert.True(t, tsTagsMatch(point, nil))
	})
}

// ---------------------------------------------------------------------------
// tsAggregatePoints
// ---------------------------------------------------------------------------

func TestTsAggregatePoints(t *testing.T) {
	points := []transport.TSDataPoint{
		{Value: 10, TimestampNs: 1000},
		{Value: 20, TimestampNs: 2000},
		{Value: 30, TimestampNs: 3000},
	}

	t.Run("sum", func(t *testing.T) {
		r := tsAggregatePoints(points, transport.AggSum, 0)
		assert.InDelta(t, 60.0, r.Value, 1e-10)
		assert.Equal(t, int64(3), r.Count)
	})

	t.Run("avg", func(t *testing.T) {
		r := tsAggregatePoints(points, transport.AggAvg, 0)
		assert.InDelta(t, 20.0, r.Value, 1e-10)
	})

	t.Run("min", func(t *testing.T) {
		r := tsAggregatePoints(points, transport.AggMin, 0)
		assert.InDelta(t, 10.0, r.Value, 1e-10)
	})

	t.Run("max", func(t *testing.T) {
		r := tsAggregatePoints(points, transport.AggMax, 0)
		assert.InDelta(t, 30.0, r.Value, 1e-10)
	})

	t.Run("count", func(t *testing.T) {
		r := tsAggregatePoints(points, transport.AggCount, 0)
		assert.InDelta(t, 3.0, r.Value, 1e-10)
	})

	t.Run("first", func(t *testing.T) {
		r := tsAggregatePoints(points, transport.AggFirst, 0)
		assert.InDelta(t, 10.0, r.Value, 1e-10)
	})

	t.Run("last", func(t *testing.T) {
		r := tsAggregatePoints(points, transport.AggLast, 0)
		assert.InDelta(t, 30.0, r.Value, 1e-10)
	})

	t.Run("default/none falls back to sum", func(t *testing.T) {
		r := tsAggregatePoints(points, transport.AggNone, 0)
		assert.InDelta(t, 60.0, r.Value, 1e-10)
	})

	t.Run("empty points returns zero value", func(t *testing.T) {
		r := tsAggregatePoints(nil, transport.AggSum, 5000)
		assert.InDelta(t, 0.0, r.Value, 1e-10)
		assert.Equal(t, int64(5000), r.TimestampNs)
		assert.Equal(t, int64(0), r.Count)
	})
}
