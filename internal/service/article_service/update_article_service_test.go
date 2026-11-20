package article_service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateArticleService_OptimisticLockAndSharedMediaProtection(t *testing.T) {
	ctx := context.Background()
	fakeRedis := test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	t.Setenv("PUBLIC_MEDIA_BASE_URL", "https://cdn.numan.app/public")

	adminID := int64(9001)
	now := domain.NowMillis()
	for _, m := range []media_models.MediaPayload{
		{ID: 3001, OwnerID: adminID, StoragePath: "articles/doc-v1.md", Visibility: variables.MediaVisibilityPublic, CreatedAt: now},
		{ID: 3002, OwnerID: adminID, StoragePath: "articles/doc-v2.md", Visibility: variables.MediaVisibilityPending, CreatedAt: now},
		{ID: 4001, OwnerID: adminID, StoragePath: "articles/anim-1.gif", Visibility: variables.MediaVisibilityPublic, CreatedAt: now},
		{ID: 4002, OwnerID: adminID, StoragePath: "articles/shared.webp", Visibility: variables.MediaVisibilityPublic, CreatedAt: now},
	} {
		require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, m))
	}

	require.NoError(t, object_cache_service.SetArticleInCache(ctx, article_models.ArticlePayload{
		ID:                 5001,
		Title:              "Guide de Lyon",
		MarkdownMediaID:    3001,
		AttachmentMediaIDs: []int64{4001, 4002},
		Visibility:         variables.ArticleVisibilityPublished,
		CreatedAt:          now,
		UpdatedAt:          now,
	}))
	// Deuxième article actif qui partage 4002
	require.NoError(t, object_cache_service.SetArticleInCache(ctx, article_models.ArticlePayload{
		ID:                 5002,
		Title:              "Brouillon Partagé",
		MarkdownMediaID:    3001,
		AttachmentMediaIDs: []int64{4002},
		Visibility:         variables.ArticleVisibilityDraft,
		CreatedAt:          now,
		UpdatedAt:          now,
	}))

	// 1. Conflit de concurrence optimiste -> 409 Conflict
	staleTS := now - 999
	conflictTitle := "Conflit"
	_, errConflict := UpdateArticleService(ctx, adminID, article_models.UpdateArticleInput{
		ArticleID:         5001,
		Title:             &conflictTitle,
		ExpectedUpdatedAt: &staleTS,
	})
	require.Error(t, errConflict)
	var appErr *numan_error.AppError
	require.ErrorAs(t, errConflict, &appErr)
	assert.Equal(t, http.StatusConflict, appErr.HTTPStatus)

	// 2. Remplacement 3001 -> 3002 et retrait de 4001 & 4002
	newMD := int64(3002)
	emptyAtt := []int64{}
	expectedTS := now
	updatedTitle := "Guide de Lyon V2"
	updOut, err := UpdateArticleService(ctx, adminID, article_models.UpdateArticleInput{
		ArticleID:          5001,
		Title:              &updatedTitle,
		MarkdownMediaID:    &newMD,
		AttachmentMediaIDs: &emptyAtt,
		ExpectedUpdatedAt:  &expectedTS,
	})
	require.NoError(t, err)
	assert.Equal(t, "Guide de Lyon V2", updOut.Article.Title)
	assert.Equal(t, "https://cdn.numan.app/public/articles/doc-v2.md", updOut.Article.MarkdownURL)
	assert.Empty(t, updOut.Article.Attachments)
	assert.Greater(t, updOut.Article.UpdatedAt, now)

	// 4001 uniquement utilisé par 5001 -> soft-deleted en L1
	_, errM4001 := object_cache_service.GetMediaFromObjectCache(ctx, 4001)
	assert.Error(t, errM4001)

	// 3001 et 4002 restent utilisés par l'article 5002 -> doivent rester actifs !
	m3001, errM3001 := object_cache_service.GetMediaFromObjectCache(ctx, 3001)
	require.NoError(t, errM3001)
	assert.Equal(t, variables.MediaVisibilityPublic, m3001.Visibility)

	m4002, errM4002 := object_cache_service.GetMediaFromObjectCache(ctx, 4002)
	require.NoError(t, errM4002)
	assert.Equal(t, variables.MediaVisibilityPublic, m4002.Visibility)

	// 3. Restauration L1 si EnqueueDB échoue lors d'une mise à jour
	fakeRedis.SetFailCmd("RPUSH", errors.New("queue down"))
	rollbackTitle := "Ne doit pas rester"
	_, errFail := UpdateArticleService(ctx, adminID, article_models.UpdateArticleInput{
		ArticleID: 5001,
		Title:     &rollbackTitle,
	})
	assert.Error(t, errFail)
	fakeRedis.SetFailCmd("RPUSH", nil)

	stored, errStored := object_cache_service.GetArticleFromCache(ctx, 5001)
	require.NoError(t, errStored)
	assert.Equal(t, "Guide de Lyon V2", stored.Title)
}
