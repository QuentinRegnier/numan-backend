package postgres

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/like_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncLoadLikes charge les objets complets de likes pour permettre l'auto-guérison L2.
// On passe <= 0 pour targetID ou userID si on ne souhaite pas filtrer sur ces champs.
func FuncLoadLikes(ctx context.Context, targetType int, targetID int64, userID int64, limit int64, orderMode int64) ([]like_models.LikePayload, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	// Préparation des paramètres pour gérer les valeurs NULL (le fallback par défaut de la fonction SQL)
	var pTargetType, pTargetID, pUserID any

	if targetType >= 0 {
		pTargetType = targetType
	}
	if targetID > 0 {
		pTargetID = targetID
	}
	if userID > 0 {
		pUserID = userID
	}

	query := `SELECT id, target_type, target_id, user_id, created_at 
	          FROM content.func_load_likes($1, $2, $3, $4, $5)`

	rows, err := postgres.PostgresDB.QueryContext(ctx, query, pTargetType, pTargetID, pUserID, limit, orderMode)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les likes.",
			err,
		)
	}
	defer func() { _ = rows.Close() }()

	likes := make([]like_models.LikePayload, 0)
	for rows.Next() {
		var l like_models.LikePayload
		var createdAt time.Time

		if err := rows.Scan(&l.ID, &l.TargetType, &l.TargetID, &l.UserID, &createdAt); err == nil {
			// Le Payload Go attend une string pour la date
			l.CreatedAt = createdAt.UTC().Format(time.RFC3339)
			likes = append(likes, l)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les likes.",
			err,
		)
	}

	return likes, nil
}
