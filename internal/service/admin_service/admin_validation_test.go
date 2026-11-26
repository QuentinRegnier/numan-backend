package admin_service

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertAppErrorStatusAndCode(t *testing.T, err error, status int, code string) {
	t.Helper()
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, status, appErr.HTTPStatus)
	if code != "" {
		assert.Equal(t, code, appErr.Code)
	}
}

func TestAdminServices_ValidationGuardsBeforeRedis(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	// 1. ExecuteAdminHardBanService
	_, err := ExecuteAdminHardBanService(ctx, 0, admin_models.AdminHardBanInput{
		TargetUserID: 42,
		Reason:       "Raison valide suffisamment longue",
	})
	assertAppErrorStatusAndCode(t, err, http.StatusUnauthorized, numan_error.CodeUnauthorized)

	_, err = ExecuteAdminHardBanService(ctx, 1, admin_models.AdminHardBanInput{
		TargetUserID: 0,
		Reason:       "Raison valide suffisamment longue",
	})
	assertAppErrorStatusAndCode(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = ExecuteAdminHardBanService(ctx, 1, admin_models.AdminHardBanInput{
		TargetUserID: 42,
		ReportID:     -1,
		Reason:       "Raison valide suffisamment longue",
	})
	assertAppErrorStatusAndCode(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = ExecuteAdminHardBanService(ctx, 1, admin_models.AdminHardBanInput{
		TargetUserID: 42,
		Reason:       "abc",
	})
	assertAppErrorStatusAndCode(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = ExecuteAdminHardBanService(ctx, 1, admin_models.AdminHardBanInput{
		TargetUserID:    42,
		Reason:          "Raison valide suffisamment longue",
		CGUFeedbackNote: strings.Repeat("a", 2001),
	})
	assertAppErrorStatusAndCode(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	// 2. ResolveAdminAuditService
	_, err = ResolveAdminAuditService(ctx, 0, variables.UserGradeAdmin, admin_models.ResolveAdminAuditInput{
		ReportID:  100,
		Decision:  "validate",
		Rationale: "Décision valide",
	})
	assertAppErrorStatusAndCode(t, err, http.StatusUnauthorized, numan_error.CodeUnauthorized)

	_, err = ResolveAdminAuditService(ctx, 10, variables.UserGradeModerator, admin_models.ResolveAdminAuditInput{
		ReportID:  100,
		Decision:  "validate",
		Rationale: "Décision valide",
	})
	assertAppErrorStatusAndCode(t, err, http.StatusForbidden, numan_error.CodeForbidden)

	_, err = ResolveAdminAuditService(ctx, 10, variables.UserGradeAdmin, admin_models.ResolveAdminAuditInput{
		ReportID:  0,
		Decision:  "validate",
		Rationale: "Décision valide",
	})
	assertAppErrorStatusAndCode(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = ResolveAdminAuditService(ctx, 10, variables.UserGradeAdmin, admin_models.ResolveAdminAuditInput{
		ReportID:  100,
		Decision:  "invalid",
		Rationale: "Décision valide",
	})
	assertAppErrorStatusAndCode(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = ResolveAdminAuditService(ctx, 10, variables.UserGradeAdmin, admin_models.ResolveAdminAuditInput{
		ReportID:  100,
		Decision:  "revoke",
		Rationale: "non",
	})
	assertAppErrorStatusAndCode(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = ResolveAdminAuditService(ctx, 10, variables.UserGradeAdmin, admin_models.ResolveAdminAuditInput{
		ReportID:              100,
		Decision:              "revoke",
		Rationale:             "Décision valide",
		RevokedBlameReportIDs: []int64{-5},
	})
	assertAppErrorStatusAndCode(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	// 3. PublishTrustScoreEvent
	err = PublishTrustScoreEvent(ctx, 42, "UNKNOWN_EVENT_TYPE", 10, "motif")
	assertAppErrorStatusAndCode(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	// Guard: Redis must remain completely untouched
	keys, errKeys := repoRedis.ScanKeys(ctx, "*", 200)
	require.NoError(t, errKeys)
	assert.Empty(t, keys)
}
