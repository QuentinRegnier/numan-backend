package admin_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminHardBanInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilInput *admin_models.AdminHardBanInput
	assertBadRequest(t, nilInput.Validate())

	valid := admin_models.AdminHardBanInput{
		TargetUserID:    42,
		ReportID:        100,
		Reason:          "Violation grave et répétée des conditions.",
		CGUFeedbackNote: "Article 4.2",
	}
	assert.NoError(t, valid.Validate())

	// ReportID = 0 is valid (optional direct hard ban)
	noReport := valid
	noReport.ReportID = 0
	assert.NoError(t, noReport.Validate())

	// Invalid cases
	invalidTarget := valid
	invalidTarget.TargetUserID = 0
	assertBadRequest(t, invalidTarget.Validate())

	negTarget := valid
	negTarget.TargetUserID = -5
	assertBadRequest(t, negTarget.Validate())

	negReport := valid
	negReport.ReportID = -1
	assertBadRequest(t, negReport.Validate())

	shortReason := valid
	shortReason.Reason = "   abc   "
	assertBadRequest(t, shortReason.Validate())

	longReason := valid
	longReason.Reason = strings.Repeat("a", 1001)
	assertBadRequest(t, longReason.Validate())

	longNote := valid
	longNote.CGUFeedbackNote = strings.Repeat("é", 2001)
	assertBadRequest(t, longNote.Validate())

	// Multi-byte UTF-8 rune count within bounds
	unicodeReason := valid
	unicodeReason.Reason = strings.Repeat("é", 1000)
	assert.NoError(t, unicodeReason.Validate())
}
