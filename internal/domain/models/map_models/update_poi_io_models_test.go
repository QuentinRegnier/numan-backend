package map_models_test

import (
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdatePOIInput_NormalizeItems(t *testing.T) {
	newTitle := "Updated Title"
	gLink := "https://maps.app.goo.gl/updated"
	aLink := "https://maps.apple.com/?place-id=updated"
	single := map_models.UpdatePOIInput{
		ID:             99,
		Title:          &newTitle,
		GoogleMapsLink: &gLink,
		AppleMapsLink:  &aLink,
	}
	items := single.NormalizeItems()
	require.Len(t, items, 1)
	assert.Equal(t, int64(99), items[0].ID)
	require.NotNil(t, items[0].Title)
	assert.Equal(t, "Updated Title", *items[0].Title)
	require.NotNil(t, items[0].GoogleMapsLink)
	assert.Equal(t, gLink, *items[0].GoogleMapsLink)
	require.NotNil(t, items[0].AppleMapsLink)
	assert.Equal(t, aLink, *items[0].AppleMapsLink)

	batch := map_models.UpdatePOIInput{
		POIs: []map_models.UpdatePOIItemInput{
			{ID: 1, Title: &newTitle},
			{ID: 2, Title: &newTitle},
		},
	}
	assert.Len(t, batch.NormalizeItems(), 2)

	empty := map_models.UpdatePOIInput{}
	assert.Nil(t, empty.NormalizeItems())
}

func TestUpdatePOIInput_Validate(t *testing.T) {
	title := "Valid Title"
	emptyTitle := "   "
	badLat := 95.0
	badRating := 6.0
	badMedia := []int64{0}

	var nilUpdItem *map_models.UpdatePOIItemInput
	require.Error(t, nilUpdItem.Validate())
	require.NoError(t, (&map_models.UpdatePOIItemInput{ID: 1, Title: &title}).Validate())
	require.Error(t, (&map_models.UpdatePOIItemInput{ID: 0, Title: &title}).Validate())
	require.Error(t, (&map_models.UpdatePOIItemInput{ID: 1, Title: &emptyTitle}).Validate())
	require.Error(t, (&map_models.UpdatePOIItemInput{ID: 1, Latitude: &badLat}).Validate())
	require.Error(t, (&map_models.UpdatePOIItemInput{ID: 1, Rating: &badRating}).Validate())
	require.Error(t, (&map_models.UpdatePOIItemInput{ID: 1, MediaIDs: &badMedia}).Validate())

	var nilUpd *map_models.UpdatePOIInput
	require.Error(t, nilUpd.Validate())
	require.Error(t, (&map_models.UpdatePOIInput{}).Validate())
	require.NoError(t, (&map_models.UpdatePOIInput{ID: 1, Title: &title}).Validate())
	require.Error(t, (&map_models.UpdatePOIInput{
		POIs: []map_models.UpdatePOIItemInput{{ID: 1, Title: &title}, {ID: 1, Title: &title}},
	}).Validate())
}
