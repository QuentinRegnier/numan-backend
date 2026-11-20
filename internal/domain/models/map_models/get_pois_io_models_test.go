package map_models_test

import (
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPOIPayload_ToView(t *testing.T) {
	p := map_models.POIPayload{
		ID:             42,
		Type:           3,
		Title:          "Louvre",
		Description:    "Musée d'art",
		Latitude:       48.8606,
		Longitude:      2.3376,
		Address:        "Rue de Rivoli, 75001 Paris",
		Phone:          "+33140205050",
		Website:        "https://www.louvre.fr",
		Rating:         4.9,
		CommentCount:   1000,
		MediaIDs:       []int64{500},
		GoogleMapsLink: "https://maps.app.goo.gl/louvre",
		AppleMapsLink:  "https://maps.apple.com/?place-id=louvre",
	}
	media := []media_models.MediaView{
		{MediaID: 500, URL: "https://cdn.numan.app/louvre.avif"},
	}
	view := p.ToView(media)
	assert.Equal(t, int64(42), view.ID)
	assert.Equal(t, "Louvre", view.Title)
	assert.Len(t, view.Media, 1)
	assert.Equal(t, "https://cdn.numan.app/louvre.avif", view.Media[0].URL)
	assert.Equal(t, "https://maps.app.goo.gl/louvre", view.GoogleMapsLink)
	assert.Equal(t, "https://maps.apple.com/?place-id=louvre", view.AppleMapsLink)
}

func TestGetPoisInBBoxInput_Validate(t *testing.T) {
	var nilBBox *map_models.GetPoisInBBoxInput
	require.Error(t, nilBBox.Validate())
	require.NoError(t, (&map_models.GetPoisInBBoxInput{SWLat: 48.0, SWLng: 2.0, NELat: 49.0, NELng: 3.0}).Validate())
	require.Error(t, (&map_models.GetPoisInBBoxInput{SWLat: 50.0, SWLng: 2.0, NELat: 49.0, NELng: 3.0}).Validate())
}
