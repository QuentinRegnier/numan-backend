package map_models_test

import (
	"math"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestImportAndSetPOIInput_Validate(t *testing.T) {
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

	var nilSet *map_models.SetPOIInput
	require.Error(t, nilSet.Validate())
	require.Error(t, (&map_models.SetPOIInput{}).Validate())
	require.NoError(t, (&map_models.SetPOIInput{Title: "Single", Latitude: 48.85, Longitude: 2.35}).Validate())
	require.Error(t, (&map_models.SetPOIInput{POIs: []map_models.CreatePOIItemInput{item, item}}).Validate())
}

func TestUpdateAndDeletePOIInput_Validate(t *testing.T) {
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

	var nilDel *map_models.DeletePOIInput
	require.Error(t, nilDel.Validate())
	require.Error(t, (&map_models.DeletePOIInput{}).Validate())
	require.Error(t, (&map_models.DeletePOIInput{POIIDs: []int64{1, -5}}).Validate())
	require.NoError(t, (&map_models.DeletePOIInput{POIIDs: []int64{1, 2}}).Validate())

	var nilBBox *map_models.GetPoisInBBoxInput
	require.Error(t, nilBBox.Validate())
	require.NoError(t, (&map_models.GetPoisInBBoxInput{SWLat: 48.0, SWLng: 2.0, NELat: 49.0, NELng: 3.0}).Validate())
	require.Error(t, (&map_models.GetPoisInBBoxInput{SWLat: 50.0, SWLng: 2.0, NELat: 49.0, NELng: 3.0}).Validate())
}

func TestPOIReviewIOInputs_Validate(t *testing.T) {
	var nilCreate *map_models.CreatePOIReviewInput
	require.Error(t, nilCreate.Validate())
	require.NoError(t, (&map_models.CreatePOIReviewInput{POIID: 1, Rating: 5, Content: "Super"}).Validate())
	require.Error(t, (&map_models.CreatePOIReviewInput{POIID: 0, Rating: 5}).Validate())
	require.Error(t, (&map_models.CreatePOIReviewInput{POIID: 1, Rating: 0}).Validate())
	require.Error(t, (&map_models.CreatePOIReviewInput{POIID: 1, Rating: 6}).Validate())
	require.Error(t, (&map_models.CreatePOIReviewInput{
		POIID:   1,
		Rating:  5,
		Content: strings.Repeat("é", variables.MaxPOIReviewContentLength+1),
	}).Validate())

	var nilGet *map_models.GetPOIReviewsInput
	require.Error(t, nilGet.Validate())
	require.NoError(t, (&map_models.GetPOIReviewsInput{POIID: 1, Limit: 0, Offset: 0}).Validate())
	require.Error(t, (&map_models.GetPOIReviewsInput{POIID: 0}).Validate())
	require.Error(t, (&map_models.GetPOIReviewsInput{POIID: 1, Offset: -1}).Validate())
	require.Error(t, (&map_models.GetPOIReviewsInput{POIID: 1, Limit: -1}).Validate())

	var nilDel *map_models.DeletePOIReviewInput
	require.Error(t, nilDel.Validate())
	require.NoError(t, (&map_models.DeletePOIReviewInput{POIID: 1}).Validate())
	require.Error(t, (&map_models.DeletePOIReviewInput{POIID: 0}).Validate())
}
