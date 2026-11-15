package variables_test

import (
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
)

func TestMapConstants(t *testing.T) {
	assert.Equal(t, 500, variables.MaxPOIDescriptionLength)
	assert.Equal(t, 200, variables.MaxPOITitleLength)
	assert.Equal(t, -90.0, variables.MinLatitude)
	assert.Equal(t, 90.0, variables.MaxLatitude)
	assert.Equal(t, -180.0, variables.MinLongitude)
	assert.Equal(t, 180.0, variables.MaxLongitude)
	assert.Equal(t, 0.0, variables.MinPOIRating)
	assert.Equal(t, 5.0, variables.MaxPOIRating)
	assert.Greater(t, variables.MaxMediaPerPOI, 0)
	assert.Greater(t, variables.MaxPOIGeoSearchCount, 0)
	assert.Greater(t, variables.MaxPOIImportBatch, 0)
	assert.Greater(t, variables.MaxPOISetBatch, 0)
	assert.Greater(t, variables.MaxPOIUpdateBatch, 0)
	assert.Greater(t, variables.MaxPOIDeleteBatch, 0)
	assert.Equal(t, 1, variables.MinPOIReviewRating)
	assert.Equal(t, 5, variables.MaxPOIReviewRating)
	assert.Greater(t, variables.MaxPOIReviewContentLength, 0)
	assert.Greater(t, variables.MaxZsetPOIReview, 0)
	assert.Equal(t, -1, variables.POIVisibilitySoftDeleted)
	assert.Equal(t, 0, variables.POIVisibilityPublic)
	assert.Equal(t, -1, variables.POIReviewVisibilitySoftDeleted)
	assert.Equal(t, 0, variables.POIReviewVisibilityPublic)
}
