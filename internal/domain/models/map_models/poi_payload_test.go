package map_models_test

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
	"go.mongodb.org/mongo-driver/bson"
)

func TestPOIPayload_Serialization(t *testing.T) {
	p := map_models.POIPayload{
		ID:             987654321,
		Type:           2,
		Title:          "Café de Flore",
		Description:    "Café historique de Saint-Germain-des-Prés",
		Latitude:       48.854028,
		Longitude:      2.332528,
		Address:        "172 Boulevard Saint-Germain, 75006 Paris",
		Phone:          "+33145485526",
		Website:        "https://cafedeflore.fr",
		Rating:         4.6,
		CommentCount:   128,
		MediaIDs:       []int64{101, 102},
		GoogleMapsLink: "https://maps.app.goo.gl/cafedeflore",
		AppleMapsLink:  "https://maps.apple.com/?place-id=123",
		Visibility:     variables.POIVisibilityPublic,
		CreatedAt:      1700000000000,
		UpdatedAt:      1700000000000,
	}

	// MsgPack serialization preserves GoogleMapsLink and AppleMapsLink
	msgBytes, err := msgpack.Marshal(p)
	require.NoError(t, err)
	var decodedMsg map_models.POIPayload
	require.NoError(t, msgpack.Unmarshal(msgBytes, &decodedMsg))
	assert.Equal(t, p, decodedMsg)

	// BSON serialization preserves GoogleMapsLink and AppleMapsLink
	bsonBytes, err := bson.Marshal(p)
	require.NoError(t, err)
	var decodedBSON map_models.POIPayload
	require.NoError(t, bson.Unmarshal(bsonBytes, &decodedBSON))
	assert.Equal(t, p, decodedBSON)

	// JSON serialization preserves GoogleMapsLink and AppleMapsLink
	jsonBytes, err := json.Marshal(p)
	require.NoError(t, err)
	var decodedJSON map_models.POIPayload
	require.NoError(t, json.Unmarshal(jsonBytes, &decodedJSON))
	assert.Equal(t, p, decodedJSON)
}

func TestPOIPayload_Validate(t *testing.T) {
	valid := map_models.POIPayload{
		ID:           1,
		Type:         1,
		Title:        "Tour Eiffel",
		Description:  "Monument emblématique",
		Latitude:     48.858370,
		Longitude:    2.294481,
		Address:      "Champ de Mars, 5 Av. Anatole France, 75007 Paris",
		Rating:       4.8,
		CommentCount: 42,
		MediaIDs:     []int64{10, 20},
		Visibility:   variables.POIVisibilityPublic,
	}
	require.NoError(t, valid.Validate())

	softDeleted := valid
	softDeleted.Visibility = variables.POIVisibilitySoftDeleted
	require.NoError(t, softDeleted.Validate())

	badVisibility := valid
	badVisibility.Visibility = 99
	require.Error(t, badVisibility.Validate())

	var nilPayload *map_models.POIPayload
	require.Error(t, nilPayload.Validate())

	emptyTitle := valid
	emptyTitle.Title = "   "
	require.Error(t, emptyTitle.Validate())

	longDesc := valid
	longDesc.Description = strings.Repeat("é", 501)
	require.Error(t, longDesc.Validate())

	exactDesc := valid
	exactDesc.Description = strings.Repeat("é", 500)
	require.NoError(t, exactDesc.Validate())

	badLat := valid
	badLat.Latitude = 91.0
	require.Error(t, badLat.Validate())

	badLng := valid
	badLng.Longitude = -181.0
	require.Error(t, badLng.Validate())

	nanLat := valid
	nanLat.Latitude = math.NaN()
	require.Error(t, nanLat.Validate())

	badRating := valid
	badRating.Rating = 5.5
	require.Error(t, badRating.Validate())

	badComments := valid
	badComments.CommentCount = -1
	require.Error(t, badComments.Validate())

	badMedia := valid
	badMedia.MediaIDs = []int64{0}
	require.Error(t, badMedia.Validate())
}
