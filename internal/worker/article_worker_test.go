package worker

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestArticleMapper_ToRowAndBuildUpdateQuery(t *testing.T) {
	mapper := getMapper(redis.EntityArticle)
	require.NotNil(t, mapper)
	assert.Equal(t, "content.articles", mapper.TableName())
	assert.Len(t, mapper.Columns(), 11)

	// 1. Complete payload
	exp := int64(1900000000000)
	full := article_models.ArticlePayload{
		ID:                 101,
		Title:              "Guide",
		Summary:            "Résumé",
		CoverMediaID:       201,
		MarkdownMediaID:    301,
		AttachmentMediaIDs: []int64{401, 402},
		PartnerURL:         "https://partner.example.com",
		Visibility:         variables.ArticleVisibilityPublished,
		ExpiresAt:          &exp,
		CreatedAt:          1700000001000,
		UpdatedAt:          1700000002000,
	}
	row, err := mapper.ToRow(full)
	require.NoError(t, err)
	require.Len(t, row, 11)
	assert.Equal(t, int64(101), row[0])
	assert.Equal(t, "Guide", row[1])
	assert.Equal(t, int64(201), row[3])
	assert.Equal(t, int64(301), row[4])
	assert.Equal(t, "https://partner.example.com", row[6])
	assert.Equal(t, int64(1900000000000), row[8])

	// 2. Nullable fields (0 media IDs, empty partner URL, nil expires_at)
	minimal := article_models.ArticlePayload{
		ID:         102,
		Title:      "Draft",
		Visibility: variables.ArticleVisibilityDraft,
		CreatedAt:  1700000001000,
		UpdatedAt:  1700000001000,
	}
	minRow, err := mapper.ToRow(minimal)
	require.NoError(t, err)
	assert.Nil(t, minRow[3], "cover_media_id <= 0 must be SQL NULL")
	assert.Nil(t, minRow[4], "markdown_media_id <= 0 must be SQL NULL")
	assert.Nil(t, minRow[6], "empty partner_url must be SQL NULL")
	assert.Nil(t, minRow[8], "nil expires_at must be SQL NULL")

	// 3. Monotonic BuildUpdateQuery
	updateSQL := mapper.BuildUpdateQuery("tmp_articles")
	assert.Contains(t, updateSQL, "UPDATE content.articles SET")
	assert.Contains(t, updateSQL, "content.articles.updated_at <= tmp_articles.updated_at")
	assert.Contains(t, updateSQL, "content.articles.visibility >= 0")
}

func TestArticleWriteBehind_OrderingAndIdempotency_MongoAndPostgres(t *testing.T) {
	ctx := context.Background()
	now := domain.NowTime()

	v1 := article_models.ArticlePayload{
		ID:        501,
		Title:     "V1 Old",
		CreatedAt: 1700000001000,
		UpdatedAt: 1700000001000,
	}
	v2Newer := article_models.ArticlePayload{
		ID:        501,
		Title:     "V2 Newer",
		CreatedAt: 1700000001000,
		UpdatedAt: 1700000005000,
	}

	// Out-of-order events in batch: v2Newer arrives before v1
	evts := []redis.AsyncEvent{
		{ID: 501, Type: redis.EntityArticle, Action: redis.ActionUpdate, Payload: v2Newer, Timestamp: 1700000005000},
		{ID: 501, Type: redis.EntityArticle, Action: redis.ActionCreate, Payload: v1, Timestamp: 1700000001000},
	}

	models := buildArticleMongoWriteModels(evts, now)
	require.Len(t, models, 1, "Deux événements pour le même article doivent être consolidés en 1 opération")

	// Verify Postgres handleArticleUpsert keeps V2 Newer and does not overwrite with V1 Old
	var upsertedTitle string
	var softDeletedIDs any
	test.SetupMockDB(t, nil, func(query string, args []driver.NamedValue) (driver.Result, error) {
		if strings.Contains(query, "func_upsert_article") && len(args) >= 2 {
			upsertedTitle, _ = args[1].Value.(string)
		}
		if strings.Contains(query, "func_bulk_soft_delete_articles") && len(args) >= 1 {
			softDeletedIDs = args[0].Value
		}
		return driver.RowsAffected(1), nil
	})

	flushPostgres(ctx, evts)
	assert.Equal(t, "V2 Newer", upsertedTitle, "L'événement obsolète V1 ne doit jamais écraser la version plus récente V2")

	// Soft-delete event via flushPostgres
	delEvts := []redis.AsyncEvent{
		{ID: 501, Type: redis.EntityArticle, Action: redis.ActionDelete, Payload: article_models.ArticlePayload{ID: 501, Visibility: -1, UpdatedAt: 1700000009000}},
	}
	flushPostgres(ctx, delEvts)
	assert.NotNil(t, softDeletedIDs)

	// Soft-delete irreversibility check in shouldReplaceArticleState
	deletedState := article_models.ArticlePayload{ID: 501, Visibility: variables.ArticleVisibilitySoftDeleted, UpdatedAt: 1700000006000}
	lateActiveState := article_models.ArticlePayload{ID: 501, Visibility: variables.ArticleVisibilityPublished, UpdatedAt: 1700000008000}
	assert.False(t, shouldReplaceArticleState(deletedState, true, lateActiveState, false), "Un article supprimé ne doit pas être ressuscité par un événement actif hors-ordre")

	// FlushMongo integration
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	defer test.ResetCollections()()
	mt.Run("flushMongo processes EntityArticle", func(mt *mtest.T) {
		mongo.Articles = test.NewMockCollection(mt, "content.articles", nil)
		mt.AddMockResponses(mtest.CreateSuccessResponse())
		flushMongo(ctx, evts)
	})
}
