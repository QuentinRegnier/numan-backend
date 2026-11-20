package auth_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddCertificationInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilC *auth_models.AddCertificationInput
	assertBadRequest(t, nilC.Validate())

	require.NoError(t, (&auth_models.AddCertificationInput{
		Prestataire:     "federation_francaise",
		CertificationID: "LIC-12345",
	}).Validate())

	cases := []struct {
		name  string
		input auth_models.AddCertificationInput
	}{
		{
			name:  "prestataire too short",
			input: auth_models.AddCertificationInput{Prestataire: "a", CertificationID: "LIC-12345"},
		},
		{
			name:  "prestataire whitespace",
			input: auth_models.AddCertificationInput{Prestataire: "   ", CertificationID: "LIC-12345"},
		},
		{
			name:  "prestataire too long",
			input: auth_models.AddCertificationInput{Prestataire: strings.Repeat("é", 51), CertificationID: "LIC-12345"},
		},
		{
			name:  "certification_id too short",
			input: auth_models.AddCertificationInput{Prestataire: "federation_francaise", CertificationID: "ab"},
		},
		{
			name:  "certification_id too long",
			input: auth_models.AddCertificationInput{Prestataire: "federation_francaise", CertificationID: strings.Repeat("A", 101)},
		},
		{
			name:  "certification_id invalid characters",
			input: auth_models.AddCertificationInput{Prestataire: "federation_francaise", CertificationID: "LIC 12345!"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertBadRequest(t, tc.input.Validate())
		})
	}
}
