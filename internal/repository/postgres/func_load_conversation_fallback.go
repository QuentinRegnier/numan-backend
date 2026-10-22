package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/lib/pq"
)

type inboxFallbackResult struct {
	Conversation lite_models.ConvLiteRequest   `json:"conversation"`
	Member       lite_models.MemberLiteRequest `json:"member"`
}

func FuncLoadConversationFallback(ctx context.Context, userID int64, convIDs []int64) ([]inboxFallbackResult, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT messaging.func_load_conversation_fallback_json($1, $2)`

	var b []byte
	err := postgres.PostgresDB.QueryRowContext(
		ctx,
		query,
		userID,
		pq.Array(convIDs),
	).Scan(&b)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []inboxFallbackResult{}, nil
		}
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger le fallback des conversations.",
			err,
		)
	}

	if b == nil {
		return []inboxFallbackResult{}, nil
	}

	var results []inboxFallbackResult
	if err := json.Unmarshal(b, &results); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de désérialiser les conversations de secours.",
			err,
		)
	}

	if results == nil {
		results = []inboxFallbackResult{}
	}

	return results, nil
}
