package object_cache_service_test

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArticleObjectCache_SetGetDeleteAndPersistentTTL(t *testing.T) {
	ctx := context.Background()
	srv := test.SetupTestRedis(t)

	art1 := article_models.ArticlePayload{
		ID:                 1001,
		Title:              "Guide Paris",
		Summary:            "Découverte",
		CoverMediaID:       2001,
		MarkdownMediaID:    3001,
		AttachmentMediaIDs: []int64{4001},
		PartnerURL:         "https://partner.example.com",
		Visibility:         variables.ArticleVisibilityPublished,
		CreatedAt:          1700000001000,
		UpdatedAt:          1700000001000,
	}

	require.NoError(t, object_cache_service.SetArticleInCache(ctx, art1))

	// Vérification critique : TTL = -1 (persistant / non-évictable sous volatile-lfu)
	assert.Equal(t, time.Duration(-1), srv.KeyTTL(variables.RedisKeyArticlesIndexData))
	assert.Equal(t, time.Duration(-1), srv.KeyTTL(variables.RedisKeyArticlesIndexOrder))
	assert.Equal(t, time.Duration(-1), srv.KeyTTL(variables.RedisKeyArticlesIndexInit))

	// Lecture L1 hit
	got, err := object_cache_service.GetArticleFromCache(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, art1.ID, got.ID)
	assert.Equal(t, art1.Title, got.Title)
	assert.Equal(t, []int64{4001}, got.AttachmentMediaIDs)

	// Soft-delete retire l'article de L1
	artDeleted := art1
	artDeleted.Visibility = variables.ArticleVisibilitySoftDeleted
	require.NoError(t, object_cache_service.SetArticleInCache(ctx, artDeleted))

	assert.False(t, srv.HashExists(variables.RedisKeyArticlesIndexData, "1001"))
}

func TestArticleObjectCache_ListOrderingAndPartialSelfHealing(t *testing.T) {
	ctx := context.Background()
	srv := test.SetupTestRedis(t)
	defer test.ResetCollections()()
	mongo.Articles = nil

	cols := []string{
		"id", "title", "summary", "cover_media_id", "markdown_media_id",
		"attachment_media_ids", "partner_url", "visibility", "expires_at", "created_at", "updated_at",
	}

	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		if strings.Contains(query, "func_get_articles_by_ids") {
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(1002), "Healed Article 2", "Summary 2", int64(202), int64(302), "{}", "", int64(0), nil, int64(1700000002000), int64(1700000002000)},
			}), nil
		}
		if strings.Contains(query, "func_get_article_by_id") {
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(1004), "Cascade L3 Article", "Summary 4", int64(204), int64(304), "{}", "", int64(1), nil, int64(1700000004000), int64(1700000004000)},
			}), nil
		}
		return test.NewMockRows(cols, nil), nil
	}, nil)

	art1 := article_models.ArticlePayload{
		ID:         1001,
		Title:      "Article 1",
		Visibility: variables.ArticleVisibilityPublished,
		CreatedAt:  1700000001000,
		UpdatedAt:  1700000001000,
	}
	art2 := article_models.ArticlePayload{
		ID:         1002,
		Title:      "Article 2",
		Visibility: variables.ArticleVisibilityPublished,
		CreatedAt:  1700000002000,
		UpdatedAt:  1700000002000,
	}
	art3 := article_models.ArticlePayload{
		ID:         1003,
		Title:      "Article 3",
		Visibility: variables.ArticleVisibilityDraft,
		CreatedAt:  1700000003000,
		UpdatedAt:  1700000003000,
	}

	require.NoError(t, object_cache_service.SetArticlesBatchInCache(ctx, []article_models.ArticlePayload{art1, art2, art3}))

	// Simuler une corruption partielle : supprimer 1002 du HASH mais le laisser dans le ZSET,
	// et ajouter un membre orphelin 9999 dans le ZSET qui n'existe ni en L2 ni en L3
	srv.HashDelete(variables.RedisKeyArticlesIndexData, "1002")
	srv.ZSetAdd(variables.RedisKeyArticlesIndexOrder, "9999", 1700000009000)

	list, err := object_cache_service.ListArticlesFromCache(ctx)
	require.NoError(t, err)
	require.Len(t, list, 3)

	// Tri décroissant : 1003 (3000), 1002 (2000, auto-réparé depuis L3), 1001 (1000)
	assert.Equal(t, int64(1003), list[0].ID)
	assert.Equal(t, int64(1002), list[1].ID)
	assert.Equal(t, "Healed Article 2", list[1].Title)
	assert.Equal(t, int64(1001), list[2].ID)

	// Vérifier que 1002 a été réécrit dans le HASH L1
	assert.True(t, srv.HashExists(variables.RedisKeyArticlesIndexData, "1002"))

	// Test GetArticleFromCache cascade L1 miss -> L3 hit -> L1 heal
	art4, err := object_cache_service.GetArticleFromCache(ctx, 1004)
	require.NoError(t, err)
	assert.Equal(t, int64(1004), art4.ID)
	assert.Equal(t, "Cascade L3 Article", art4.Title)

	assert.True(t, srv.HashExists(variables.RedisKeyArticlesIndexData, "1004"))
}

func TestArticleObjectCache_RebuildOnWipedRedisAndEmptySeededIndex(t *testing.T) {
	ctx := context.Background()
	test.SetupTestRedis(t)
	defer test.ResetCollections()()
	mongo.Articles = nil

	cols := []string{
		"id", "title", "summary", "cover_media_id", "markdown_media_id",
		"attachment_media_ids", "partner_url", "visibility", "expires_at", "created_at", "updated_at",
	}

	l3CallCount := 0
	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		if strings.Contains(query, "func_load_active_articles") {
			l3CallCount++
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(5001), "Rebuilt 1", "S1", int64(0), int64(0), "{}", "", int64(0), nil, int64(1700000001000), int64(1700000001000)},
				{int64(5002), "Rebuilt 2", "S2", int64(0), int64(0), "{}", "", int64(1), nil, int64(1700000005000), int64(1700000005000)},
			}), nil
		}
		return test.NewMockRows(cols, nil), nil
	}, nil)

	// Redis est vierge (pas de clé articles:index:initialized) -> ListArticlesFromCache reconstruit depuis L3
	rebuilt, err := object_cache_service.ListArticlesFromCache(ctx)
	require.NoError(t, err)
	require.Len(t, rebuilt, 2)
	assert.Equal(t, int64(5002), rebuilt[0].ID)
	assert.Equal(t, int64(5001), rebuilt[1].ID)
	assert.Equal(t, 1, l3CallCount)

	// Si tous les articles sont supprimés mais que l'index reste initialisé,
	// ListArticlesFromCache ne doit PAS déclencher une tempête de rebuild vers L3 !
	require.NoError(t, object_cache_service.DeleteArticlesBatchFromCache(ctx, []int64{5001, 5002}))
	emptyList, err := object_cache_service.ListArticlesFromCache(ctx)
	require.NoError(t, err)
	assert.Empty(t, emptyList)
	assert.Equal(t, 1, l3CallCount, "Ne doit pas recharger L3 lorsque l'index est initialisé mais vide")
}
