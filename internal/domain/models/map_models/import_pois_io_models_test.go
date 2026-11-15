package map_models_test

import (
	"encoding/json"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportPOIsIO_Serialization(t *testing.T) {
	in := map_models.ImportPOIsInput{
		POIs: []map_models.CreatePOIItemInput{
			{
				ID:             10,
				Type:           1,
				Title:          "Tour Eiffel",
				Latitude:       48.85837,
				Longitude:      2.29448,
				GoogleMapsLink: "https://maps.app.goo.gl/eiffel",
				AppleMapsLink:  "https://maps.apple.com/?place-id=eiffel",
			},
		},
	}

	raw, err := json.Marshal(in)
	require.NoError(t, err)

	var decoded map_models.ImportPOIsInput
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Len(t, decoded.POIs, 1)
	assert.Equal(t, "https://maps.app.goo.gl/eiffel", decoded.POIs[0].GoogleMapsLink)
	assert.Equal(t, "https://maps.apple.com/?place-id=eiffel", decoded.POIs[0].AppleMapsLink)
}
