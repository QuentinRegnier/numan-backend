package two_factor_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertBadRequestPasskeyPayload(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestPasskeyPayload_Validate(t *testing.T) {
	var nilPk *two_factor_models.PasskeyPayload
	assertBadRequestPasskeyPayload(t, nilPk.Validate())

	pk := two_factor_models.PasskeyPayload{
		ID:           1,
		UserID:       2,
		CredentialID: "cred",
		PublicKey:    []byte{1, 2, 3},
		SignCount:    0,
		DeviceName:   "iPhone",
	}
	require.NoError(t, pk.Validate())

	pkBadID := pk
	pkBadID.ID = 0
	assertBadRequestPasskeyPayload(t, pkBadID.Validate())

	pkBadUser := pk
	pkBadUser.UserID = -1
	assertBadRequestPasskeyPayload(t, pkBadUser.Validate())

	pkNoCred := pk
	pkNoCred.CredentialID = "   "
	assertBadRequestPasskeyPayload(t, pkNoCred.Validate())

	pkNoKey := pk
	pkNoKey.PublicKey = nil
	assertBadRequestPasskeyPayload(t, pkNoKey.Validate())

	pkBadSign := pk
	pkBadSign.SignCount = -1
	assertBadRequestPasskeyPayload(t, pkBadSign.Validate())

	pkLongDevice := pk
	pkLongDevice.DeviceName = strings.Repeat("é", 65)
	assertBadRequestPasskeyPayload(t, pkLongDevice.Validate())
}
