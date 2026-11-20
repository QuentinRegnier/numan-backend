package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncGetArticleByID récupère un article actif par son identifiant depuis PostgreSQL L3.
func FuncGetArticleByID(ctx context.Context, articleID int64) (article_models.ArticlePayload, bool, error) {
	if articleID <= 0 {
		return article_models.ArticlePayload{}, false, nil
	}

	if postgres.PostgresDB == nil {
		return article_models.ArticlePayload{}, false, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `
		SELECT id, title, summary, cover_media_id, markdown_media_id, attachment_media_ids,
		       partner_url, visibility, expires_at, created_at, updated_at
		FROM content.func_get_article_by_id($1)
	`

	rows, err := postgres.PostgresDB.QueryContext(ctx, query, articleID)
	if err != nil {
		return article_models.ArticlePayload{}, false, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de récupérer l'article depuis PostgreSQL.",
			err,
		)
	}
	defer func() {
		if errClose := rows.Close(); errClose != nil {
			numan_log.Error(ctx).Err(errClose).Msg("Impossible de fermer le curseur SQL pour FuncGetArticleByID")
		}
	}()

	articles, errScan := scanArticleRows(rows)
	if errScan != nil {
		return article_models.ArticlePayload{}, false, errScan
	}
	if len(articles) == 0 {
		return article_models.ArticlePayload{}, false, nil
	}

	return articles[0], true, nil
}
