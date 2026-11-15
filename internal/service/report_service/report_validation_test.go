package report_service

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/report_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubmitReport_ValidationGuardsAndValidPostReport(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	// 1. Unauthenticated caller
	err := SubmitReport(ctx, 0, report_models.CreateReportInput{
		TargetType: variables.ReportTargetPost,
		TargetIDs:  []int64{10},
		Category:   variables.ReportCatSpam,
	})
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)

	// 2. Invalid target type
	err = SubmitReport(ctx, 1, report_models.CreateReportInput{
		TargetType: 4,
		TargetIDs:  []int64{10},
		Category:   variables.ReportCatSpam,
	})
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)

	// 3. Empty TargetIDs
	err = SubmitReport(ctx, 1, report_models.CreateReportInput{
		TargetType: variables.ReportTargetPost,
		TargetIDs:  nil,
		Category:   variables.ReportCatSpam,
	})
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)

	// 4. Negative TargetID
	err = SubmitReport(ctx, 1, report_models.CreateReportInput{
		TargetType: variables.ReportTargetPost,
		TargetIDs:  []int64{-5},
		Category:   variables.ReportCatSpam,
	})
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)

	// 5. Exceeding MaxReportTargets returns 400 INVALID_PAYLOAD (not 403)
	tooMany := make([]int64, variables.MaxReportTargets+1)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	err = SubmitReport(ctx, 1, report_models.CreateReportInput{
		TargetType: variables.ReportTargetMessage,
		TargetIDs:  tooMany,
		Category:   variables.ReportCatSpam,
	})
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)

	// 6. Invalid category
	err = SubmitReport(ctx, 1, report_models.CreateReportInput{
		TargetType: variables.ReportTargetPost,
		TargetIDs:  []int64{10},
		Category:   999,
	})
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)

	// 7. Too long reason
	err = SubmitReport(ctx, 1, report_models.CreateReportInput{
		TargetType: variables.ReportTargetPost,
		TargetIDs:  []int64{10},
		Category:   variables.ReportCatSpam,
		Reason:     strings.Repeat("x", 1001),
	})
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)

	keys, errKeys := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, errKeys)
	assert.Empty(t, keys)

	// 8. Valid Post report (TargetType == 0) succeeds and persists ticket/enqueue
	require.NoError(t, SubmitReport(ctx, 1, report_models.CreateReportInput{
		TargetType: variables.ReportTargetPost,
		TargetIDs:  []int64{500},
		Category:   variables.ReportCatSpam,
		Reason:     "Spam évident",
	}))
}
