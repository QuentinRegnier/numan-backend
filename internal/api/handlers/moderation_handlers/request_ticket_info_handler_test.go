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

func TestRequestTicketInfoHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)

	r := gin.New()
	r.POST("/admin/moderation/tickets/info/request", func(c *gin.Context) {
		c.Set("userID", int64(3001))
		c.Set("user_grade", variables.UserGradeModerator)
		RequestTicketInfoHandler(c)
	})

	ticketID := int64(903)
	ticket := &moderation_models.ModerationTicketRecord{
		ReportID:       ticketID,
		TargetType:     variables.ReportTargetPost,
		TargetID:       103,
		State:          variables.ReportStateInProgress,
		ClaimedByModID: 3001,
	}
	require.NoError(t, cache_service.SaveModerationTicket(t.Context(), ticket))

	body, _ := json.Marshal(moderation_models.RequestTicketInfoInput{
		ReportID: ticketID,
		Question: "Pouvez-vous préciser le contexte ?",
	})
	req := httptest.NewRequest(http.MethodPost, "/admin/moderation/tickets/info/request", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var res moderation_models.ModerationTicketPublicView
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	assert.Equal(t, variables.ReportStateWaitingForInfo, res.State)
	assert.Equal(t, "Pouvez-vous préciser le contexte ?", res.InfoRequestMessage)
}
