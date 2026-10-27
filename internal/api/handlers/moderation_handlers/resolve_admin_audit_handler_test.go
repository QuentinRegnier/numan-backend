package moderation_handlers

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveAdminAuditHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)

	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		return test.NewMockRows([]string{"archived"}, [][]driver.Value{{true}}), nil
	}, nil)

	r := gin.New()
	r.POST("/admin/moderation/audit/resolve", func(c *gin.Context) {
		c.Set("userID", int64(4001))
		c.Set("user_grade", variables.UserGradeAdmin)
		ResolveAdminAuditHandler(c)
	})

	superReportID := int64(909)
	superReport := &moderation_models.ModerationTicketRecord{
		ReportID:       superReportID,
		TargetType:     variables.ReportTargetUser,
		TargetID:       7009,
		TargetOwnerID:  7009,
		ReportOrigin:   variables.ReportOriginSystem,
		State:          variables.ReportStateEscalated,
		EscalationType: "blame_quota_audit",
		AuditBlameRecords: []moderation_models.BlameRecord{
			{ProposerModID: 3001, ValidatorModID: 3002},
			{ProposerModID: 3001, ValidatorModID: 3002},
			{ProposerModID: 3001, ValidatorModID: 3002},
		},
	}
	require.NoError(t, cache_service.SaveModerationTicket(t.Context(), superReport))
	require.NoError(t, cache_service.IndexTicketInModerationQueue(t.Context(), variables.ModerationQueueAdmin, superReportID, 100.0))

	body, _ := json.Marshal(moderation_models.ResolveAdminAuditInput{
		ReportID:  superReportID,
		Decision:  "validate",
		Rationale: "Infractions répétées confirmées par l'audit",
	})
	req := httptest.NewRequest(http.MethodPost, "/admin/moderation/audit/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var res moderation_models.ResolveAdminAuditOutput
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	assert.True(t, res.TargetBanned)
	assert.Equal(t, variables.ReportStateClosed, res.Ticket.State)
}
