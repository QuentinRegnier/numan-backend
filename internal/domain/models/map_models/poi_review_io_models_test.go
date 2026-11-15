package map_models_test

import (
	"encoding/json"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPOIReviewIOModels(t *testing.T) {
	payload := map_models.POIReviewPayload{
		ID:        10,
		POIID:     20,
		UserID:    30,
		Rating:    4,
		Content:   "Très sympa",
		CreatedAt: 1700000000000,
		UpdatedAt: 1700000000000,
	}
	view := payload.ToView("alice", media_models.MediaView{MediaID: 99, URL: "https://cdn.numan.app/avatar.avif"})
	assert.Equal(t, int64(10), view.ID)
	assert.Equal(t, "alice", view.AuthorUsername)
	assert.Equal(t, int64(99), view.AuthorAvatar.MediaID)

	createIn := map_models.CreatePOIReviewInput{POIID: 20, Rating: 5, Content: "Top"}
	raw, err := json.Marshal(createIn)
	require.NoError(t, err)
	var decodedCreate map_models.CreatePOIReviewInput
	require.NoError(t, json.Unmarshal(raw, &decodedCreate))
	assert.Equal(t, createIn, decodedCreate)

	getIn := map_models.GetPOIReviewsInput{POIID: 20, Limit: 10, Offset: 5}
	rawGet, err := json.Marshal(getIn)
	require.NoError(t, err)
	var decodedGet map_models.GetPOIReviewsInput
	require.NoError(t, json.Unmarshal(rawGet, &decodedGet))
	assert.Equal(t, getIn, decodedGet)
}
