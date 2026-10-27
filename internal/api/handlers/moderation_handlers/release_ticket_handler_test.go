package moderation_handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReleaseTicketHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)

	r := gin.New()
	r.POST("/admin/moderation/tickets/release", func(c *gin.Context) {
		c.Set("userID", int64(3001))
		c.Set("user_grade", variables.UserGradeModerator)
		ReleaseTicketHandler(c)
	})

	ticketID := int64(902)
	ticket := &moderation_models.ModerationTicketRecord{
		ReportID:       ticketID,
		TargetType:     variables.ReportTargetPost,
		TargetID:       102,
		State:          variables.ReportStateInProgress,
		ClaimedByModID: 3001,
		PriorityScore:  25.0,
	}
	require.NoError(t, cache_service.SaveModerationTicket(t.Context(), ticket))

	body, _ := json.Marshal(moderation_models.ReleaseTicketInput{ReportID: ticketID})
	req := httptest.NewRequest(http.MethodPost, "/admin/moderation/tickets/release", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var res moderation_models.ModerationTicketPublicView
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	assert.Equal(t, variables.ReportStatePending, res.State)
	assert.Equal(t, int64(0), res.ClaimedByModID)
}
