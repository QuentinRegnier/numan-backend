package like_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/like_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLikePayload_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilP *like_models.LikePayload
	assertBadRequest(t, nilP.Validate())

	valid := like_models.LikePayload{
		ID:         1,
		TargetType: variables.LikeTargetTypePost,
		TargetID:   10,
		UserID:     20,
	}
	require.NoError(t, valid.Validate())
	valid.TargetType = variables.LikeTargetTypeComment
	require.NoError(t, valid.Validate())

	bad := valid
	bad.ID = 0
	assertBadRequest(t, bad.Validate())

	bad = valid
	bad.TargetType = 99
	assertBadRequest(t, bad.Validate())

	bad = valid
	bad.TargetID = -1
	assertBadRequest(t, bad.Validate())

	bad = valid
	bad.UserID = 0
	assertBadRequest(t, bad.Validate())
}
