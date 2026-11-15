package map_models_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

func TestPOIReviewPayload_ValidateAndSerialization(t *testing.T) {
	valid := map_models.POIReviewPayload{
		ID:        1001,
		POIID:     5001,
		UserID:    2001,
		Rating:    5,
		Content:   "Magnifique monument !",
		CreatedAt: 1700000000000,
		UpdatedAt: 1700000000000,
	}
	require.NoError(t, valid.Validate())

	// Pure rating (empty content) is valid
	pureRating := valid
	pureRating.Content = ""
	require.NoError(t, pureRating.Validate())

	// MsgPack roundtrip
	msgBytes, err := msgpack.Marshal(valid)
	require.NoError(t, err)
	var decodedMsg map_models.POIReviewPayload
	require.NoError(t, msgpack.Unmarshal(msgBytes, &decodedMsg))
	assert.Equal(t, valid, decodedMsg)

	// JSON roundtrip
	jsonBytes, err := json.Marshal(valid)
	require.NoError(t, err)
	var decodedJSON map_models.POIReviewPayload
	require.NoError(t, json.Unmarshal(jsonBytes, &decodedJSON))
	assert.Equal(t, valid, decodedJSON)

	// Invalid cases
	var nilReview *map_models.POIReviewPayload
	require.Error(t, nilReview.Validate())

	badPOI := valid
	badPOI.POIID = 0
	require.Error(t, badPOI.Validate())

	badUser := valid
	badUser.UserID = -1
	require.Error(t, badUser.Validate())

	lowRating := valid
	lowRating.Rating = 0
	require.Error(t, lowRating.Validate())

	highRating := valid
	highRating.Rating = 6
	require.Error(t, highRating.Validate())

	tooLong := valid
	tooLong.Content = strings.Repeat("a", variables.MaxPOIReviewContentLength+1)
	require.Error(t, tooLong.Validate())

	softDeleted := valid
	softDeleted.Visibility = variables.POIReviewVisibilitySoftDeleted
	require.NoError(t, softDeleted.Validate())

	badVisibility := valid
	badVisibility.Visibility = 42
	require.Error(t, badVisibility.Validate())
}
