package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/lib/pq"
)

// FuncGetArticlesByIDs récupère un lot d'articles actifs par leurs identifiants depuis PostgreSQL L3.
func FuncGetArticlesByIDs(ctx context.Context, articleIDs []int64) ([]article_models.ArticlePayload, error) {
	if len(articleIDs) == 0 {
		return []article_models.ArticlePayload{}, nil
	}

	if postgres.ReadDB() == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `
		SELECT id, title, summary, cover_media_id, markdown_media_id, attachment_media_ids,
		       partner_url, visibility, expires_at, created_at, updated_at
		FROM content.func_get_articles_by_ids($1)
	`

	rows, err := postgres.ReadDB().QueryContext(ctx, query, pq.Array(articleIDs))
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les articles par identifiants.",
			err,
		)
	}
	defer func() {
		if errClose := rows.Close(); errClose != nil {
			numan_log.Error(ctx).Err(errClose).Msg("Impossible de fermer le curseur SQL pour FuncGetArticlesByIDs")
		}
	}()

	return scanArticleRows(rows)
}
