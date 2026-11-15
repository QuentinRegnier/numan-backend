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

func assertBadRequest(t *testing.T, err error) {
	t.Helper()
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestAdminHardBanInput_Validate(t *testing.T) {
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

func TestResolveAdminAuditInput_Validate(t *testing.T) {
	var nilInput *admin_models.ResolveAdminAuditInput
	assertBadRequest(t, nilInput.Validate())

	valid := admin_models.ResolveAdminAuditInput{
		ReportID:              55,
		Decision:              "validate",
		Rationale:             "Audit confirmé après revue complète.",
		RevokedBlameReportIDs: []int64{1, 2},
	}
	assert.NoError(t, valid.Validate())

	revoke := valid
	revoke.Decision = "revoke"
	assert.NoError(t, revoke.Validate())

	badReportID := valid
	badReportID.ReportID = 0
	assertBadRequest(t, badReportID.Validate())

	badDecision := valid
	badDecision.Decision = "ignore"
	assertBadRequest(t, badDecision.Validate())

	shortRationale := valid
	shortRationale.Rationale = "  ok  "
	assertBadRequest(t, shortRationale.Validate())

	longRationale := valid
	longRationale.Rationale = strings.Repeat("x", 1001)
	assertBadRequest(t, longRationale.Validate())

	badRevokedElem := valid
	badRevokedElem.RevokedBlameReportIDs = []int64{1, 0}
	assertBadRequest(t, badRevokedElem.Validate())

	tooManyRevoked := valid
	tooManyRevoked.RevokedBlameReportIDs = make([]int64, 101)
	for i := range tooManyRevoked.RevokedBlameReportIDs {
		tooManyRevoked.RevokedBlameReportIDs[i] = int64(i + 1)
	}
	assertBadRequest(t, tooManyRevoked.Validate())
}
