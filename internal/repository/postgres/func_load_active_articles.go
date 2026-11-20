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

// FuncLoadActiveArticles charge tous les articles actifs (publiés et brouillons) depuis PostgreSQL L3 pour le seeding et la reconstruction L1.
func FuncLoadActiveArticles(ctx context.Context) ([]article_models.ArticlePayload, error) {
	if postgres.PostgresDB == nil {
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
		FROM content.func_load_active_articles()
	`

	rows, err := postgres.PostgresDB.QueryContext(ctx, query)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les articles actifs depuis PostgreSQL.",
			err,
		)
	}
	defer func() {
		if errClose := rows.Close(); errClose != nil {
			numan_log.Error(ctx).Err(errClose).Msg("Impossible de fermer le curseur SQL pour FuncLoadActiveArticles")
		}
	}()

	return scanArticleRows(rows)
}
