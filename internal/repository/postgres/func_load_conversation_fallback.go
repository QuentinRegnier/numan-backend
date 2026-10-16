package postgres

import (
	"context"
	"encoding/json"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/lib/pq"
)

type inboxFallbackResult struct {
	Conversation lite_models.ConvLiteRequest   `json:"conversation"`
	Member       lite_models.MemberLiteRequest `json:"member"`
}

func FuncLoadConversationFallback(ctx context.Context, userID int64, convIDs []int64) ([]inboxFallbackResult, error) {
	query := `SELECT messaging.func_load_conversation_fallback_json($1, $2)`

	var b []byte
	err := postgres.PostgresDB.QueryRowContext(
		ctx,
		query,
		userID,
		pq.Array(convIDs),
	).Scan(&b)
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("Erreur SQL lors du chargement du fallback de conversation")
		return nil, numan_error.NewInternal(err)
	}

	var results []inboxFallbackResult
	err = json.Unmarshal(b, &results)
	if err != nil {
		numan_log.Warn(ctx).Err(err).Msg("Erreur lors de la désérialisation du fallback de conversation")
		return nil, numan_error.NewInternal(err)
	}

	return results, nil
}
