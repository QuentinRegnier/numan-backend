package map_models_test

import (
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetPOIInput_NormalizeItems(t *testing.T) {
	single := map_models.SetPOIInput{
		Title:          "Single POI",
		Latitude:       48.85,
		Longitude:      2.35,
		GoogleMapsLink: "https://maps.app.goo.gl/single",
		AppleMapsLink:  "https://maps.apple.com/?place-id=single",
	}
	items := single.NormalizeItems()
	require.Len(t, items, 1)
	assert.Equal(t, "Single POI", items[0].Title)
	assert.Equal(t, "https://maps.app.goo.gl/single", items[0].GoogleMapsLink)
	assert.Equal(t, "https://maps.apple.com/?place-id=single", items[0].AppleMapsLink)

	batch := map_models.SetPOIInput{
		POIs: []map_models.CreatePOIItemInput{
			{Title: "POI 1", Latitude: 48.85, Longitude: 2.35},
			{Title: "POI 2", Latitude: 48.86, Longitude: 2.36},
		},
	}
	assert.Len(t, batch.NormalizeItems(), 2)

	empty := map_models.SetPOIInput{}
	assert.Nil(t, empty.NormalizeItems())
}

func TestSetPOIInput_Validate(t *testing.T) {
	item := map_models.CreatePOIItemInput{
		ID:        10,
		Title:     "Louvre",
		Latitude:  48.86,
		Longitude: 2.33,
	}

	var nilSet *map_models.SetPOIInput
	require.Error(t, nilSet.Validate())
	require.Error(t, (&map_models.SetPOIInput{}).Validate())
	require.NoError(t, (&map_models.SetPOIInput{Title: "Single", Latitude: 48.85, Longitude: 2.35}).Validate())
	require.Error(t, (&map_models.SetPOIInput{POIs: []map_models.CreatePOIItemInput{item, item}}).Validate())
}
