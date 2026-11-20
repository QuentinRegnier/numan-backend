package media_models_test

import (
	"encoding/json"
	"math"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
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

func TestMediaPayload_Validate(t *testing.T) {
	assertBadRequestCode := func(t *testing.T, err error, code string) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, code, appErr.Code)
	}

	valid := media_models.MediaPayload{
		ID:          101,
		OwnerID:     42,
		StoragePath: "users/42/media/img.avif",
		Visibility:  variables.MediaVisibilityPublic,
		IAScore:     0.25,
	}
	require.NoError(t, valid.Validate())

	for _, vis := range []int{
		variables.MediaVisibilityRejected,
		variables.MediaVisibilitySoftDeleted,
		variables.MediaVisibilityPending,
		variables.MediaVisibilityPublic,
		variables.MediaVisibilityQuarantine,
	} {
		p := valid
		p.Visibility = vis
		assert.NoError(t, p.Validate())
	}

	var nilPtr *media_models.MediaPayload
	assertBadRequestCode(t, nilPtr.Validate(), numan_error.CodeInvalidPayload)

	tests := []struct {
		name   string
		mutate func(*media_models.MediaPayload)
	}{
		{"zero id", func(p *media_models.MediaPayload) { p.ID = 0 }},
		{"negative owner", func(p *media_models.MediaPayload) { p.OwnerID = -1 }},
		{"whitespace path", func(p *media_models.MediaPayload) { p.StoragePath = "   " }},
		{"invalid visibility", func(p *media_models.MediaPayload) { p.Visibility = 99 }},
		{"negative ia score", func(p *media_models.MediaPayload) { p.IAScore = -0.01 }},
		{"above one ia score", func(p *media_models.MediaPayload) { p.IAScore = 1.01 }},
		{"nan ia score", func(p *media_models.MediaPayload) { p.IAScore = math.NaN() }},
		{"inf ia score", func(p *media_models.MediaPayload) { p.IAScore = math.Inf(1) }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := valid
			tc.mutate(&p)
			assertBadRequestCode(t, p.Validate(), numan_error.CodeInvalidPayload)
		})
	}
}
