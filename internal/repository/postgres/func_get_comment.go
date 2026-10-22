package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/comment_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncGetComment récupère l'intégralité d'un commentaire depuis L3 via sa fonction SQL dédiée.
func FuncGetComment(ctx context.Context, commentID int64) (comment_models.CommentPayload, error) {
	if postgres.PostgresDB == nil {
		return comment_models.CommentPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	var c comment_models.CommentPayload

	query := `SELECT id, post_id, user_id, content, visibility, like_count, score, created_at, updated_at FROM content.func_get_comment($1)`
	err := postgres.PostgresDB.QueryRowContext(ctx, query, commentID).Scan(
		&c.ID, &c.PostID, &c.UserID, &c.Content, &c.Visibility, &c.LikeCount, &c.Score, &c.CreatedAt, &c.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return comment_models.CommentPayload{}, numan_error.NewNotFound(
				"COMMENT_NOT_FOUND",
				"Commentaire introuvable.",
				fmt.Errorf("comment %d was not found in PostgreSQL (L3)", commentID),
			)
		}
		return comment_models.CommentPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de récupérer le commentaire.",
			err,
		)
	}

	return c, nil
}
