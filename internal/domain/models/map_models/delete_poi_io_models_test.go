package map_models_test

import (
	"encoding/json"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeletePOIIO_Serialization(t *testing.T) {
	in := map_models.DeletePOIInput{
		POIIDs: []int64{101, 102},
	}
	raw, err := json.Marshal(in)
	require.NoError(t, err)

	var decoded map_models.DeletePOIInput
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.Equal(t, []int64{101, 102}, decoded.POIIDs)
}

func TestDeletePOIInput_Validate(t *testing.T) {
	var nilDel *map_models.DeletePOIInput
	require.Error(t, nilDel.Validate())
	require.Error(t, (&map_models.DeletePOIInput{}).Validate())
	require.Error(t, (&map_models.DeletePOIInput{POIIDs: []int64{1, -5}}).Validate())
	require.Error(t, (&map_models.DeletePOIInput{POIIDs: []int64{0}}).Validate())
	require.NoError(t, (&map_models.DeletePOIInput{POIIDs: []int64{1, 2}}).Validate())

	tooMany := make([]int64, variables.MaxPOIDeleteBatch+1)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	require.Error(t, (&map_models.DeletePOIInput{POIIDs: tooMany}).Validate())
}
