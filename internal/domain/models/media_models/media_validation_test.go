package media_models_test

import (
	"math"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertBadRequestCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, code, appErr.Code)
}

func TestMediaPayload_Validate(t *testing.T) {
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

func TestSignMediaInput_Validate(t *testing.T) {
	require.NoError(t, (&media_models.SignMediaInput{MediaID: 1}).Validate())
	require.NoError(t, (&media_models.SignMediaInput{MediaID: 1, PostID: 10}).Validate())
	require.NoError(t, (&media_models.SignMediaInput{MediaID: 1, ConversationID: 20}).Validate())

	var nilPtr *media_models.SignMediaInput
	assertBadRequestCode(t, nilPtr.Validate(), numan_error.CodeInvalidPayload)
	assertBadRequestCode(t, (&media_models.SignMediaInput{MediaID: 0}).Validate(), numan_error.CodeInvalidPayload)
	assertBadRequestCode(t, (&media_models.SignMediaInput{MediaID: 1, PostID: -5}).Validate(), numan_error.CodeInvalidPayload)
	assertBadRequestCode(t, (&media_models.SignMediaInput{MediaID: 1, ConversationID: -5}).Validate(), numan_error.CodeInvalidPayload)
	assertBadRequestCode(t, (&media_models.SignMediaInput{MediaID: 1, PostID: 10, ConversationID: 20}).Validate(), "AMBIGUOUS_CONTEXT")
}
