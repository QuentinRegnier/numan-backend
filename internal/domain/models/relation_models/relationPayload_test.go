package relation_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/relation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelationPayload_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilP *relation_models.RelationPayload
	assertBadRequest(t, nilP.Validate())

	cases := []struct {
		name    string
		payload relation_models.RelationPayload
		valid   bool
	}{
		{
			name:    "valid follow",
			payload: relation_models.RelationPayload{ID: 1, PrimaryID: 10, SecondaryID: 20, State: variables.RelationStateFollow},
			valid:   true,
		},
		{
			name:    "valid cache sync zero id",
			payload: relation_models.RelationPayload{ID: 0, PrimaryID: 10, SecondaryID: 20, State: variables.RelationStateFriend},
			valid:   true,
		},
		{
			name:    "valid blocked state",
			payload: relation_models.RelationPayload{ID: 5, PrimaryID: 10, SecondaryID: 20, State: variables.RelationStateBlocked},
			valid:   true,
		},
		{
			name:    "valid none state",
			payload: relation_models.RelationPayload{ID: 5, PrimaryID: 10, SecondaryID: 20, State: variables.RelationStateNone},
			valid:   true,
		},
		{
			name:    "negative id",
			payload: relation_models.RelationPayload{ID: -1, PrimaryID: 10, SecondaryID: 20, State: variables.RelationStateFollow},
		},
		{
			name:    "zero primary id",
			payload: relation_models.RelationPayload{ID: 1, PrimaryID: 0, SecondaryID: 20, State: variables.RelationStateFollow},
		},
		{
			name:    "zero secondary id",
			payload: relation_models.RelationPayload{ID: 1, PrimaryID: 10, SecondaryID: 0, State: variables.RelationStateFollow},
		},
		{
			name:    "self relation",
			payload: relation_models.RelationPayload{ID: 1, PrimaryID: 10, SecondaryID: 10, State: variables.RelationStateFollow},
		},
		{
			name:    "state too low",
			payload: relation_models.RelationPayload{ID: 1, PrimaryID: 10, SecondaryID: 20, State: -2},
		},
		{
			name:    "state too high",
			payload: relation_models.RelationPayload{ID: 1, PrimaryID: 10, SecondaryID: 20, State: 3},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.payload
			if tc.valid {
				require.NoError(t, p.Validate())
			} else {
				assertBadRequest(t, p.Validate())
			}
		})
	}
}
