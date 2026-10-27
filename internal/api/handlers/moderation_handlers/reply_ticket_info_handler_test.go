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

func TestReplyTicketInfoHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)

	r := gin.New()
	r.POST("/moderation/tickets/info/reply", func(c *gin.Context) {
		c.Set("userID", int64(6001))
		ReplyTicketInfoHandler(c)
	})

	ticketID := int64(904)
	ticket := &moderation_models.ModerationTicketRecord{
		ReportID:           ticketID,
		TargetType:         variables.ReportTargetPost,
		TargetID:           104,
		State:              variables.ReportStateWaitingForInfo,
		ClaimedByModID:     3001,
		ReporterIDs:        []int64{6001},
		InfoRequestMessage: "Question ?",
	}
	require.NoError(t, cache_service.SaveModerationTicket(t.Context(), ticket))

	body, _ := json.Marshal(moderation_models.ReplyTicketInfoInput{
		ReportID: ticketID,
		Reply:    "Voici ma réponse complète.",
	})
	req := httptest.NewRequest(http.MethodPost, "/moderation/tickets/info/reply", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var res moderation_models.ModerationTicketPublicView
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	assert.Equal(t, variables.ReportStateInProgress, res.State)
	assert.Equal(t, "Voici ma réponse complète.", res.InfoReplyMessage)
	assert.Equal(t, int64(3001), res.ClaimedByModID)
}
