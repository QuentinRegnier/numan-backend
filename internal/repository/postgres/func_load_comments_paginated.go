package postgres

import (
	"context"
	"database/sql"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/comment_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncLoadCommentsKeyset lit L3 et trie par pertinence via content.func_load_comments_keyset.
func FuncLoadCommentsKeyset(ctx context.Context, postID int64, afterScore *int, afterCreatedAt *string, afterID *int64, limit int64) ([]comment_models.CommentPayload, error) {
	if afterCreatedAt != nil {
		if _, err := time.Parse(time.RFC3339Nano, *afterCreatedAt); err != nil {
			if _, err2 := time.Parse(time.RFC3339, *afterCreatedAt); err2 != nil {
				numan_log.Error(ctx).Err(err2).Str("after_created_at", *afterCreatedAt).Msg("Format d'horodatage invalide pour afterCreatedAt")
				return nil, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Horodatage invalide", err2)
			}
		}
	}

	var comments []comment_models.CommentPayload

	query := `SELECT id, post_id, user_id, content, visibility, like_count, score, created_at, updated_at FROM content.func_load_comments_keyset($1, $2, $3, $4, $5)`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query, postID, afterScore, afterCreatedAt, afterID, limit)
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("Échec de l'exécution de la requête SQL (QueryContext)")
		return nil, numan_error.NewInternal()
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			numan_log.Error(ctx).Err(err).Msg("Erreur lors de la fermeture des lignes Postgres")
		}
	}(rows)

	for rows.Next() {
		var c comment_models.CommentPayload
		if err := rows.Scan(&c.ID, &c.PostID, &c.UserID, &c.Content, &c.Visibility, &c.LikeCount, &c.Score, &c.CreatedAt, &c.UpdatedAt); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Erreur lors du scan d'un commentaire (Postgres)")
			return nil, numan_error.NewInternal()
		}
		comments = append(comments, c)
	}
	if err := rows.Err(); err != nil {
		numan_log.Error(ctx).Err(err).Msg("Erreur lors de l'itération sur les résultats SQL (rows.Err)")
		return nil, numan_error.NewInternal()
	}
	return comments, nil
}
