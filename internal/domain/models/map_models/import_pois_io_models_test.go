package map_models_test

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
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

func TestCreatePOIItemInput_Validate(t *testing.T) {
	valid := map_models.CreatePOIItemInput{
		ID:          1,
		Type:        1,
		Title:       "Café de Flore",
		Description: "Café historique",
		Latitude:    48.854028,
		Longitude:   2.332528,
		Address:     "172 Boulevard Saint-Germain",
		Rating:      4.6,
		MediaIDs:    []int64{10, 20},
	}

	var nilInput *map_models.CreatePOIItemInput
	require.Error(t, nilInput.Validate())

	tests := []struct {
		name    string
		mutate  func(*map_models.CreatePOIItemInput)
		wantErr bool
	}{
		{"valid item", func(in *map_models.CreatePOIItemInput) {}, false},
		{"zero ID allowed (auto-generated)", func(in *map_models.CreatePOIItemInput) { in.ID = 0 }, false},
		{"negative ID", func(in *map_models.CreatePOIItemInput) { in.ID = -1 }, true},
		{"empty title", func(in *map_models.CreatePOIItemInput) { in.Title = "   " }, true},
		{"title too long", func(in *map_models.CreatePOIItemInput) {
			in.Title = strings.Repeat("é", variables.MaxPOITitleLength+1)
		}, true},
		{"description too long", func(in *map_models.CreatePOIItemInput) {
			in.Description = strings.Repeat("a", variables.MaxPOIDescriptionLength+1)
		}, true},
		{"address too long", func(in *map_models.CreatePOIItemInput) {
			in.Address = strings.Repeat("a", variables.MaxPOIAddressLength+1)
		}, true},
		{"phone too long", func(in *map_models.CreatePOIItemInput) { in.Phone = strings.Repeat("1", variables.MaxPOIPhoneLength+1) }, true},
		{"website too long", func(in *map_models.CreatePOIItemInput) {
			in.Website = strings.Repeat("a", variables.MaxPOIWebsiteLength+1)
		}, true},
		{"latitude out of bounds", func(in *map_models.CreatePOIItemInput) { in.Latitude = 91 }, true},
		{"longitude NaN", func(in *map_models.CreatePOIItemInput) { in.Longitude = math.NaN() }, true},
		{"rating out of bounds", func(in *map_models.CreatePOIItemInput) { in.Rating = 5.1 }, true},
		{"negative comment count", func(in *map_models.CreatePOIItemInput) { in.CommentCount = -1 }, true},
		{"too many media IDs", func(in *map_models.CreatePOIItemInput) {
			in.MediaIDs = make([]int64, variables.MaxMediaPerPOI+1)
			for i := range in.MediaIDs {
				in.MediaIDs[i] = int64(i + 1)
			}
		}, true},
		{"non-positive media ID", func(in *map_models.CreatePOIItemInput) { in.MediaIDs = []int64{1, 0} }, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := valid
			tc.mutate(&in)
			err := in.Validate()
			if tc.wantErr {
				require.Error(t, err)
				var appErr *numan_error.AppError
				require.ErrorAs(t, err, &appErr)
				assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestImportPOIsInput_Validate(t *testing.T) {
	item := map_models.CreatePOIItemInput{
		ID:        10,
		Title:     "Louvre",
		Latitude:  48.86,
		Longitude: 2.33,
	}

	var nilImport *map_models.ImportPOIsInput
	require.Error(t, nilImport.Validate())
	require.Error(t, (&map_models.ImportPOIsInput{}).Validate())
	require.NoError(t, (&map_models.ImportPOIsInput{POIs: []map_models.CreatePOIItemInput{item}}).Validate())
	require.Error(t, (&map_models.ImportPOIsInput{POIs: []map_models.CreatePOIItemInput{item, item}}).Validate())
}
