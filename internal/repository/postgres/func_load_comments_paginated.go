package postgres

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/comment_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncLoadCommentsKeyset lit L3 et trie par pertinence via content.func_load_comments_keyset.
func FuncLoadCommentsKeyset(ctx context.Context, postID int64, afterScore *int, afterCreatedAt *string, afterID *int64, limit int64) ([]comment_models.CommentPayload, error) {
	if afterCreatedAt != nil {
		if _, err := time.Parse(time.RFC3339Nano, *afterCreatedAt); err != nil {
			if _, err2 := time.Parse(time.RFC3339, *afterCreatedAt); err2 != nil {
				return nil, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Horodatage invalide", err2)
			}
		}
	}

	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT id, post_id, user_id, content, visibility, like_count, score, created_at, updated_at FROM content.func_load_comments_keyset($1, $2, $3, $4, $5)`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query, postID, afterScore, afterCreatedAt, afterID, limit)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les commentaires.",
			err,
		)
	}
	defer func() { _ = rows.Close() }()

	comments := make([]comment_models.CommentPayload, 0)
	for rows.Next() {
		var c comment_models.CommentPayload
		if err := rows.Scan(&c.ID, &c.PostID, &c.UserID, &c.Content, &c.Visibility, &c.LikeCount, &c.Score, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de scanner les données du commentaire.",
				err,
			)
		}
		comments = append(comments, c)
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les commentaires.",
			err,
		)
	}

	return comments, nil
}
