package moderation_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlameRecord_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	assertBadRequest(t, (*moderation_models.BlameRecord)(nil).Validate())
	assertBadRequest(t, (&moderation_models.BlameRecord{TargetType: variables.ReportTargetPost}).Validate())
	assert.NoError(t, (&moderation_models.BlameRecord{ProposerModID: 1, ValidatorModID: 2}).Validate())

	assertBadRequest(t, (&moderation_models.BlameRecord{ReportID: -1, ProposerModID: 1}).Validate())
	assertBadRequest(t, (&moderation_models.BlameRecord{TargetType: 99, ProposerModID: 1}).Validate())
	assertBadRequest(t, (&moderation_models.BlameRecord{TargetID: -1, ProposerModID: 1}).Validate())
	assertBadRequest(t, (&moderation_models.BlameRecord{ProposerModID: -1, ValidatorModID: 2}).Validate())
	assertBadRequest(t, (&moderation_models.BlameRecord{ProposerModID: 1, ValidatorModID: -1}).Validate())
	assertBadRequest(t, (&moderation_models.BlameRecord{ProposerModID: 1, FinalVoterModID: -1}).Validate())
	assertBadRequest(t, (&moderation_models.BlameRecord{ProposerModID: 1, Motivation: strings.Repeat("m", 1001)}).Validate())
	assertBadRequest(t, (&moderation_models.BlameRecord{ProposerModID: 1, CreatedAt: -1}).Validate())
	assert.NoError(t, (&moderation_models.BlameRecord{
		ReportID:        10,
		TargetType:      variables.ReportTargetPost,
		TargetID:        20,
		ProposerModID:   1,
		ValidatorModID:  2,
		FinalVoterModID: 3,
		Motivation:      strings.Repeat("m", 1000),
		CreatedAt:       100,
	}).Validate())
}
