package lite_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommunityLiteRequest_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	valid := lite_models.CommunityLiteRequest{ID: 1, Name: "Go Devs", MemberCount: 5}
	require.NoError(t, valid.Validate())

	var nilComm *lite_models.CommunityLiteRequest
	assertBadRequest(t, nilComm.Validate())
	assertBadRequest(t, (&lite_models.CommunityLiteRequest{ID: 0, Name: "Go"}).Validate())
	assertBadRequest(t, (&lite_models.CommunityLiteRequest{ID: 1, Name: "  "}).Validate())
	assertBadRequest(t, (&lite_models.CommunityLiteRequest{ID: 1, Name: strings.Repeat("a", 101)}).Validate())
	assertBadRequest(t, (&lite_models.CommunityLiteRequest{ID: 1, Name: "Go", Description: strings.Repeat("a", 501)}).Validate())
	assertBadRequest(t, (&lite_models.CommunityLiteRequest{ID: 1, Name: "Go", ProfilePictureID: -1}).Validate())
	assertBadRequest(t, (&lite_models.CommunityLiteRequest{ID: 1, Name: "Go", MemberCount: -1}).Validate())
}
