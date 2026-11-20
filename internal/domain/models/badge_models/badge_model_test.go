package badge_models_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/badge_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertBadRequest(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestBadgeMetadata_Validate(t *testing.T) {
	now := time.Now().UTC()
	future := now.Add(24 * time.Hour)
	past := now.Add(-24 * time.Hour)

	var nilMeta *badge_models.BadgeMetadata
	assertBadRequest(t, nilMeta.Validate())

	assertBadRequest(t, (&badge_models.BadgeMetadata{}).Validate())
	assertBadRequest(t, (&badge_models.BadgeMetadata{IssuedAt: now, ExpiresAt: &past}).Validate())

	require.NoError(t, (&badge_models.BadgeMetadata{IssuedAt: now}).Validate())
	require.NoError(t, (&badge_models.BadgeMetadata{IssuedAt: now, ExpiresAt: &future}).Validate())
	require.NoError(t, (&badge_models.BadgeMetadata{IssuedAt: now, ExpiresAt: &now}).Validate())
}

func TestUserBadges_Validate(t *testing.T) {
	now := time.Now().UTC()

	var nilBadges *badge_models.UserBadges
	assertBadRequest(t, nilBadges.Validate())

	empty := badge_models.UserBadges{}
	require.NoError(t, empty.Validate())

	valid := badge_models.UserBadges{
		"certification_federation_francaise": {IssuedAt: now},
	}
	require.NoError(t, valid.Validate())

	blankKey := badge_models.UserBadges{"   ": {IssuedAt: now}}
	assertBadRequest(t, blankKey.Validate())

	tooLongKey := badge_models.UserBadges{strings.Repeat("a", 101): {IssuedAt: now}}
	assertBadRequest(t, tooLongKey.Validate())

	invalidMeta := badge_models.UserBadges{"verified": {}}
	assertBadRequest(t, invalidMeta.Validate())
}

func TestUserBadges_Scan(t *testing.T) {
	now := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	exp := now.Add(48 * time.Hour)

	t.Run("nil value initializes empty map", func(t *testing.T) {
		var b badge_models.UserBadges
		require.NoError(t, b.Scan(nil))
		require.NotNil(t, b)
		assert.Empty(t, b)
	})

	t.Run("empty or null bytes initialize empty map", func(t *testing.T) {
		for _, raw := range [][]byte{{}, []byte("null")} {
			var b badge_models.UserBadges
			require.NoError(t, b.Scan(raw))
			require.NotNil(t, b)
			assert.Empty(t, b)
		}
	})

	t.Run("json bytes and string", func(t *testing.T) {
		payload, err := json.Marshal(badge_models.UserBadges{
			"verified": {IssuedAt: now, ExpiresAt: &exp},
		})
		require.NoError(t, err)

		var fromBytes badge_models.UserBadges
		require.NoError(t, fromBytes.Scan(payload))
		meta, ok := fromBytes.Get("verified")
		require.True(t, ok)
		assert.True(t, meta.IssuedAt.Equal(now))
		require.NotNil(t, meta.ExpiresAt)
		assert.True(t, meta.ExpiresAt.Equal(exp))

		var fromStr badge_models.UserBadges
		require.NoError(t, fromStr.Scan(string(payload)))
		metaStr, ok := fromStr.Get("verified")
		require.True(t, ok)
		assert.True(t, metaStr.IssuedAt.Equal(now))
	})

	t.Run("legacy array string", func(t *testing.T) {
		var b badge_models.UserBadges
		require.NoError(t, b.Scan(`["cert1","","cert2"]`))
		assert.Len(t, b, 2)
		m1, ok1 := b.Get("cert1")
		require.True(t, ok1)
		assert.False(t, m1.IssuedAt.IsZero())
		assert.Nil(t, m1.ExpiresAt)
		_, ok2 := b.Get("cert2")
		assert.True(t, ok2)
	})

	t.Run("incompatible type returns error", func(t *testing.T) {
		var b badge_models.UserBadges
		err := b.Scan(12345)
		require.Error(t, err)
	})

	t.Run("invalid json returns error", func(t *testing.T) {
		var b badge_models.UserBadges
		err := b.Scan([]byte("{invalid"))
		require.Error(t, err)
	})
}

func TestUserBadges_Value(t *testing.T) {
	t.Run("nil map serializes as empty json object", func(t *testing.T) {
		var b badge_models.UserBadges
		v, err := b.Value()
		require.NoError(t, err)
		assert.Equal(t, "{}", v)
	})

	t.Run("populated map serializes as json string", func(t *testing.T) {
		now := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
		b := badge_models.UserBadges{
			"coach": {IssuedAt: now},
		}
		v, err := b.Value()
		require.NoError(t, err)
		str, ok := v.(string)
		require.True(t, ok)

		var decoded badge_models.UserBadges
		require.NoError(t, decoded.Scan(str))
		meta, exists := decoded.Get("coach")
		require.True(t, exists)
		assert.True(t, meta.IssuedAt.Equal(now))
	})
}

func TestUserBadges_JSON(t *testing.T) {
	t.Run("MarshalJSON nil map outputs {}", func(t *testing.T) {
		var b badge_models.UserBadges
		raw, err := json.Marshal(b)
		require.NoError(t, err)
		assert.Equal(t, `{}`, string(raw))
	})

	t.Run("MarshalJSON populated map roundtrips", func(t *testing.T) {
		now := time.Date(2026, 3, 10, 8, 0, 0, 0, time.UTC)
		b := badge_models.UserBadges{
			"verified": {IssuedAt: now},
		}
		raw, err := json.Marshal(b)
		require.NoError(t, err)

		var out badge_models.UserBadges
		require.NoError(t, json.Unmarshal(raw, &out))
		meta, ok := out.Get("verified")
		require.True(t, ok)
		assert.True(t, meta.IssuedAt.Equal(now))
	})

	t.Run("UnmarshalJSON empty or null data", func(t *testing.T) {
		var b badge_models.UserBadges
		require.NoError(t, b.UnmarshalJSON(nil))
		require.NotNil(t, b)
		assert.Empty(t, b)

		require.NoError(t, b.UnmarshalJSON([]byte("null")))
		require.NotNil(t, b)
		assert.Empty(t, b)
	})

	t.Run("UnmarshalJSON legacy string array", func(t *testing.T) {
		var b badge_models.UserBadges
		require.NoError(t, json.Unmarshal([]byte(`["beta","founder",""]`), &b))
		assert.Len(t, b, 2)
		_, ok := b.Get("beta")
		assert.True(t, ok)
		_, ok = b.Get("founder")
		assert.True(t, ok)
	})

	t.Run("UnmarshalJSON invalid json fails", func(t *testing.T) {
		var b badge_models.UserBadges
		assert.Error(t, b.UnmarshalJSON([]byte(`[123]`)))
		assert.Error(t, b.UnmarshalJSON([]byte(`"not-an-object"`)))
	})
}

func TestUserBadges_Get(t *testing.T) {
	var nilBadges badge_models.UserBadges
	meta, ok := nilBadges.Get("verified")
	assert.False(t, ok)
	assert.Equal(t, badge_models.BadgeMetadata{}, meta)

	now := time.Now().UTC()
	badges := badge_models.UserBadges{
		"verified": {IssuedAt: now},
	}

	got, ok := badges.Get("verified")
	assert.True(t, ok)
	assert.Equal(t, now, got.IssuedAt)

	_, ok = badges.Get("missing")
	assert.False(t, ok)
}
