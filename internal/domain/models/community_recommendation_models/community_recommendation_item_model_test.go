package community_recommendation_models_test

import (
	"encoding/json"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/community_recommendation_models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommunityRecommendationItem_JSONContract(t *testing.T) {
	t.Run("pinned_item_includes_pin_slot_and_never_exposes_is_joined", func(t *testing.T) {
		item := community_recommendation_models.CommunityRecommendationItem{
			ID:            7412985012345678901,
			Name:          "Randonnées FR",
			Description:   "Sorties nature",
			AvatarMediaID: 99001,
			Lang:          "fr",
			Mode:          "free_chat",
			MemberCount:   342,
			IsPartner:     true,
			IsPinned:      true,
			PinSlot:       1,
			FriendsCount:  4,
		}

		raw, err := json.Marshal(item)
		require.NoError(t, err)

		var payload map[string]any
		require.NoError(t, json.Unmarshal(raw, &payload))

		assert.NotContains(t, payload, "is_joined")
		assert.Equal(t, float64(1), payload["pin_slot"])
		assert.Equal(t, true, payload["is_pinned"])
		assert.Equal(t, true, payload["is_partner"])
		assert.Equal(t, "free_chat", payload["mode"])
		assert.Equal(t, float64(4), payload["friends_count"])

		var decoded community_recommendation_models.CommunityRecommendationItem
		require.NoError(t, json.Unmarshal(raw, &decoded))
		assert.Equal(t, item, decoded)
	})

	t.Run("unpinned_item_omits_zero_pin_slot", func(t *testing.T) {
		item := community_recommendation_models.CommunityRecommendationItem{
			ID:           102,
			Name:         "Tech EN",
			Lang:         "en",
			Mode:         "subreddit",
			MemberCount:  15,
			IsPartner:    false,
			IsPinned:     false,
			PinSlot:      0,
			FriendsCount: 0,
		}

		raw, err := json.Marshal(item)
		require.NoError(t, err)

		var payload map[string]any
		require.NoError(t, json.Unmarshal(raw, &payload))

		assert.NotContains(t, payload, "pin_slot")
		assert.NotContains(t, payload, "is_joined")
		assert.Equal(t, false, payload["is_pinned"])
	})
}
