package pkg

import (
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractSnowflakeTime(t *testing.T) {
	t.Run("rejects non-positive and pre-epoch IDs", func(t *testing.T) {
		_, ok := ExtractSnowflakeTime(0)
		assert.False(t, ok)

		_, ok = ExtractSnowflakeTime(-42)
		assert.False(t, ok)

		// Small legacy integer ID (deltaMs == 0 after >> 22)
		_, ok = ExtractSnowflakeTime(12345)
		assert.False(t, ok)
	})

	t.Run("decodes known timestamp accurately", func(t *testing.T) {
		expected := time.Date(2026, 6, 15, 12, 30, 45, 123*int(time.Millisecond), time.UTC)
		deltaMs := expected.UnixMilli() - variables.Epoch
		id := (deltaMs << variables.TimeShift) | (7 << variables.NodeShift) | 42

		got, ok := ExtractSnowflakeTime(id)
		require.True(t, ok)
		assert.Equal(t, expected, got)
	})

	t.Run("round-trips generated Snowflake ID", func(t *testing.T) {
		require.NoError(t, InitSnowflake(1))
		before := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Second)
		id := GenerateID()
		after := time.Now().UTC().Add(time.Second)

		got, ok := ExtractSnowflakeTime(id)
		require.True(t, ok)
		assert.False(t, got.Before(before))
		assert.False(t, got.After(after))
	})
}
