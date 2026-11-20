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

func TestResolveAdminAuditInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

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
