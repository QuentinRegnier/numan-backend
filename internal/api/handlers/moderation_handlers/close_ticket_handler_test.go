package moderation_handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/api/handlers/moderation_handlers"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloseTicketHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	modID := int64(3001)
	ticketID := int64(7001)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userID", modID)
		c.Next()
	})
	r.POST("/admin/moderation/tickets/close", moderation_handlers.CloseTicketHandler)

	t.Run("closes_ticket_with_dismissal_and_archives", func(t *testing.T) {
		ticket := &moderation_models.ModerationTicketRecord{
			ReportID:       ticketID,
			TargetType:     variables.ReportTargetPost,
			TargetID:       101,
			State:          variables.ReportStateInProgress,
			ClaimedByModID: modID,
			ReporterIDs:    []int64{8001},
			CreatedAt:      time.Now().UnixMilli(),
			UpdatedAt:      time.Now().UnixMilli(),
		}
		require.NoError(t, cache_service.SaveModerationTicket(ctx, ticket))

		input := moderation_models.CloseTicketInput{
			ReportID:  ticketID,
			IsDismiss: true,
			Rationale: "Signalement abusif sans fondement réel.",
		}
		body, _ := json.Marshal(input)

		req := httptest.NewRequest(http.MethodPost, "/admin/moderation/tickets/close", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var output moderation_models.CloseTicketOutput
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &output))
		assert.Equal(t, ticketID, output.ReportID)
		assert.Equal(t, variables.ReportStateClosed, output.State)
		assert.Equal(t, "dismissed", output.Outcome)

		// Vérification de la persistance de l'état clôturé
		closedTicket, errGet := cache_service.GetModerationTicket(ctx, ticketID)
		require.NoError(t, errGet)
		assert.Equal(t, variables.ReportStateClosed, closedTicket.State)
		assert.Equal(t, "dismissed", closedTicket.ResolutionOutcome)
	})

	t.Run("closes_ticket_with_confirmation", func(t *testing.T) {
		ticketID2 := int64(7002)
		ticket2 := &moderation_models.ModerationTicketRecord{
			ReportID:       ticketID2,
			TargetType:     variables.ReportTargetPost,
			TargetID:       102,
			State:          variables.ReportStateInProgress,
			ClaimedByModID: modID,
			ReporterIDs:    []int64{8002},
			CreatedAt:      time.Now().UnixMilli(),
			UpdatedAt:      time.Now().UnixMilli(),
		}
		require.NoError(t, cache_service.SaveModerationTicket(ctx, ticket2))

		input := moderation_models.CloseTicketInput{
			ReportID:  ticketID2,
			IsDismiss: false,
			Rationale: "Contenu traité et sanctionné conformément aux règles.",
		}
		body, _ := json.Marshal(input)

		req := httptest.NewRequest(http.MethodPost, "/admin/moderation/tickets/close", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var output moderation_models.CloseTicketOutput
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &output))
		assert.Equal(t, "confirmed", output.Outcome)
	})

	t.Run("returns_400_when_payload_is_malformed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/admin/moderation/tickets/close", bytes.NewBufferString(`invalid-json`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}
