package models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertBadRequest(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestExternalLinks_Validate(t *testing.T) {
	t.Run("nil receiver fails", func(t *testing.T) {
		var p *models.ExternalLinks
		assertBadRequest(t, p.Validate())
	})

	t.Run("empty struct is valid", func(t *testing.T) {
		require.NoError(t, (&models.ExternalLinks{}).Validate())
	})

	t.Run("valid https link passes", func(t *testing.T) {
		p := models.ExternalLinks{Title: "Site officiel", URL: "https://example.com/community"}
		require.NoError(t, p.Validate())
	})

	t.Run("valid http link without title passes", func(t *testing.T) {
		p := models.ExternalLinks{URL: "http://example.com"}
		require.NoError(t, p.Validate())
	})

	cases := []struct {
		name string
		link models.ExternalLinks
	}{
		{"title without URL", models.ExternalLinks{Title: "Only Title"}},
		{"invalid scheme javascript", models.ExternalLinks{Title: "Bad", URL: "javascript:alert(1)"}},
		{"invalid scheme ftp", models.ExternalLinks{Title: "FTP", URL: "ftp://example.com"}},
		{"missing host", models.ExternalLinks{Title: "No Host", URL: "https:///path"}},
		{"title > 100 runes", models.ExternalLinks{Title: strings.Repeat("é", 101), URL: "https://example.com"}},
		{"url > 2048 chars", models.ExternalLinks{Title: "Long", URL: "https://example.com/" + strings.Repeat("a", 2030)}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := tc.link
			assertBadRequest(t, l.Validate())
		})
	}
}
