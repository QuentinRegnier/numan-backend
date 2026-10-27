package media_models_test

import (
	"encoding/json"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
	"go.mongodb.org/mongo-driver/bson"
)

func TestMediaPayload_Serialization(t *testing.T) {
	payload := media_models.MediaPayload{
		ID:          12345,
		OwnerID:     67890,
		StoragePath: "uploads/test.avif",
		Visibility:  variables.MediaVisibilityPublic,
		IAScore:     0.02,
		CreatedAt:   1000,
		UpdatedAt:   1000,
	}

	// 1. JSON
	jsonBytes, errJSON := json.Marshal(payload)
	require.NoError(t, errJSON)
	var decodedJSON media_models.MediaPayload
	require.NoError(t, json.Unmarshal(jsonBytes, &decodedJSON))
	assert.Equal(t, payload, decodedJSON)

	// 2. BSON
	bsonBytes, errBSON := bson.Marshal(payload)
	require.NoError(t, errBSON)
	var decodedBSON media_models.MediaPayload
	require.NoError(t, bson.Unmarshal(bsonBytes, &decodedBSON))
	assert.Equal(t, payload, decodedBSON)

	// 3. MsgPack
	msgpackBytes, errMsg := msgpack.Marshal(payload)
	require.NoError(t, errMsg)
	var decodedMsgpack media_models.MediaPayload
	require.NoError(t, msgpack.Unmarshal(msgpackBytes, &decodedMsgpack))
	assert.Equal(t, payload, decodedMsgpack)
}

func TestMediaPayload_VisibilityStates(t *testing.T) {
	states := []int{
		variables.MediaVisibilityRejected,
		variables.MediaVisibilitySoftDeleted,
		variables.MediaVisibilityPending,
		variables.MediaVisibilityPublic,
		variables.MediaVisibilityQuarantine,
	}

	for _, state := range states {
		p := media_models.MediaPayload{
			ID:         1,
			Visibility: state,
		}
		data, err := json.Marshal(p)
		require.NoError(t, err)
		var out media_models.MediaPayload
		require.NoError(t, json.Unmarshal(data, &out))
		assert.Equal(t, state, out.Visibility)
	}
}
