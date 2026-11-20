package postgres_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	infraPostgres "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoPostgres "github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuncUpsertArticle(t *testing.T) {
	ctx := context.Background()

	t.Run("uninitialized_database", func(t *testing.T) {
		origDB := infraPostgres.PostgresDB
		infraPostgres.PostgresDB = nil
		defer func() { infraPostgres.PostgresDB = origDB }()

		err := repoPostgres.FuncUpsertArticle(ctx, article_models.ArticlePayload{ID: 1})
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("success_with_expires_at_and_nil_attachments", func(t *testing.T) {
		test.SetupMockDB(t, nil, func(query string, args []driver.NamedValue) (driver.Result, error) {
			assert.Contains(t, query, "content.func_upsert_article")
			require.Len(t, args, 11)
			return driver.RowsAffected(1), nil
		})

		exp := int64(1900000000000)
		err := repoPostgres.FuncUpsertArticle(ctx, article_models.ArticlePayload{
			ID:                 101,
			Title:              "Guide",
			Summary:            "Résumé",
			CoverMediaID:       201,
			MarkdownMediaID:    301,
			AttachmentMediaIDs: []int64{401, 402},
			PartnerURL:         "https://partner.fr",
			Visibility:         0,
			ExpiresAt:          &exp,
			CreatedAt:          1700000000000,
			UpdatedAt:          1700000005000,
		})
		require.NoError(t, err)

		errNilAtt := repoPostgres.FuncUpsertArticle(ctx, article_models.ArticlePayload{
			ID:              102,
			Title:           "Guide 2",
			MarkdownMediaID: 302,
			CreatedAt:       1700000000000,
			UpdatedAt:       1700000000000,
		})
		require.NoError(t, errNilAtt)
	})

	t.Run("exec_error", func(t *testing.T) {
		test.SetupMockDB(t, nil, func(query string, args []driver.NamedValue) (driver.Result, error) {
			return nil, errors.New("db exec err")
		})

		err := repoPostgres.FuncUpsertArticle(ctx, article_models.ArticlePayload{ID: 101})
		test.AssertDatabaseError_Postgres(t, err)
	})
}
