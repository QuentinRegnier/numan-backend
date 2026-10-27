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

func TestFinalVoteBlameHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)

	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		return test.NewMockRows([]string{"archived"}, [][]driver.Value{{true}}), nil
	}, nil)

	r := gin.New()
	r.POST("/admin/moderation/tickets/blame/final-vote", func(c *gin.Context) {
		c.Set("userID", int64(3003))
		c.Set("user_grade", variables.UserGradeModerator)
		FinalVoteBlameHandler(c)
	})

	ticketID := int64(907)
	ticket := &moderation_models.ModerationTicketRecord{
		ReportID:          ticketID,
		TargetType:        variables.ReportTargetPost,
		TargetID:          107,
		State:             variables.ReportStatePendingFinalVote,
		ProposerModID:     3001,
		ProposerPseudonym: "mod_anon_3001",
		ReviewerModID:     3002,
		BlameMotivation:   "Contradiction",
		PriorityScore:     35.0,
	}
	require.NoError(t, cache_service.SaveModerationTicket(t.Context(), ticket))
	require.NoError(t, cache_service.IndexTicketInModerationQueue(t.Context(), variables.ModerationQueueFinalVote, ticketID, 35.0))

	// Profil Super-Modérateur Mod C (T_mod = 0.95)
	require.NoError(t, cache_service.SaveModeratorStats(t.Context(), &moderation_models.ModeratorStatsRecord{
		UserID:              3003,
		Grade:               variables.UserGradeModerator,
		ModeratorTrustScore: 0.95,
	}))

	body, _ := json.Marshal(moderation_models.FinalVoteBlameInput{
		ReportID:  ticketID,
		Decision:  "reject_blame",
		Rationale: "Rejeté après analyse complémentaire",
	})
	req := httptest.NewRequest(http.MethodPost, "/admin/moderation/tickets/blame/final-vote", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var res moderation_models.ModerationTicketPublicView
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	assert.Equal(t, variables.ReportStateClosed, res.State)
	assert.Equal(t, "reject_blame", res.ResolutionOutcome)
}
