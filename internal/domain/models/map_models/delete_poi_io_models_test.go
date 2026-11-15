package map_models_test

import (
	"encoding/json"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
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
