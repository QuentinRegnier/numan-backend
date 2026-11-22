package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/lib/pq"
)

// FuncUpsertArticle insère ou met à jour de manière idempotente un article en PostgreSQL L3.
func FuncUpsertArticle(ctx context.Context, a article_models.ArticlePayload) error {
	if postgres.WriteDB() == nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	var expiresAt any
	if a.ExpiresAt != nil && *a.ExpiresAt > 0 {
		expiresAt = *a.ExpiresAt
	}

	attachments := a.AttachmentMediaIDs
	if attachments == nil {
		attachments = []int64{}
	}

	query := `
		SELECT content.func_upsert_article($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`

	_, err := postgres.WriteDB().ExecContext(
		ctx,
		query,
		a.ID,
		a.Title,
		a.Summary,
		a.CoverMediaID,
		a.MarkdownMediaID,
		pq.Array(attachments),
		a.PartnerURL,
		a.Visibility,
		expiresAt,
		a.CreatedAt,
		a.UpdatedAt,
	)
	if err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'enregistrer l'article en base de données.",
			err,
		)
	}

	return nil
}
