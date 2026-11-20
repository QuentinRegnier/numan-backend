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
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteArticleService_SoftDeleteAndMediaProtection(t *testing.T) {
	ctx := context.Background()
	fakeRedis := test.SetupTestRedis(t)

	adminID := int64(9001)
	now := domain.NowMillis()
	for _, m := range []media_models.MediaPayload{
		{ID: 3001, OwnerID: adminID, StoragePath: "articles/only-5001.md", Visibility: variables.MediaVisibilityPublic, CreatedAt: now},
		{ID: 3002, OwnerID: adminID, StoragePath: "articles/shared.md", Visibility: variables.MediaVisibilityPublic, CreatedAt: now},
	} {
		require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, m))
	}

	require.NoError(t, object_cache_service.SetArticleInCache(ctx, article_models.ArticlePayload{
		ID:                 5001,
		Title:              "À supprimer",
		MarkdownMediaID:    3001,
		AttachmentMediaIDs: []int64{3002},
		Visibility:         variables.ArticleVisibilityPublished,
		CreatedAt:          now,
		UpdatedAt:          now,
	}))
	require.NoError(t, object_cache_service.SetArticleInCache(ctx, article_models.ArticlePayload{
		ID:              5002,
		Title:           "Conservé",
		MarkdownMediaID: 3002,
		Visibility:      variables.ArticleVisibilityDraft,
		CreatedAt:       now,
		UpdatedAt:       now,
	}))

	// 1. Conflit optimiste -> 409 Conflict
	staleTS := now - 500
	_, errConflict := DeleteArticleService(ctx, adminID, article_models.DeleteArticleInput{
		ArticleID:         5001,
		ExpectedUpdatedAt: &staleTS,
	})
	require.Error(t, errConflict)
	var appErr *numan_error.AppError
	require.ErrorAs(t, errConflict, &appErr)
	assert.Equal(t, http.StatusConflict, appErr.HTTPStatus)

	// 2. expected_updated_at interdit en batch -> 400
	validTS := now
	_, errBatchConc := DeleteArticleService(ctx, adminID, article_models.DeleteArticleInput{
		ArticleIDs:        []int64{5001, 5002},
		ExpectedUpdatedAt: &validTS,
	})
	assert.Error(t, errBatchConc)

	// 3. Restauration L1 si EnqueueDB échoue
	fakeRedis.SetFailCmd("RPUSH", errors.New("queue down"))
	_, errQueue := DeleteArticleService(ctx, adminID, article_models.DeleteArticleInput{ArticleID: 5001})
	assert.Error(t, errQueue)
	fakeRedis.SetFailCmd("RPUSH", nil)

	stillThere, errGet := object_cache_service.GetArticleFromCache(ctx, 5001)
	require.NoError(t, errGet)
	assert.Equal(t, int64(5001), stillThere.ID)

	// 4. Suppression nominale de 5001
	delOut, err := DeleteArticleService(ctx, adminID, article_models.DeleteArticleInput{
		ArticleID:         5001,
		ExpectedUpdatedAt: &validTS,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, delOut.DeletedCount)
	assert.Equal(t, []int64{5001}, delOut.ArticleIDs)
	assert.Equal(t, []int64{3001}, delOut.DeactivatedMediaID)

	// 3001 non partagé est soft-deleted ; 3002 partagé avec 5002 reste actif
	_, err3001 := object_cache_service.GetMediaFromObjectCache(ctx, 3001)
	assert.Error(t, err3001)
	m3002, err3002 := object_cache_service.GetMediaFromObjectCache(ctx, 3002)
	require.NoError(t, err3002)
	assert.Equal(t, variables.MediaVisibilityPublic, m3002.Visibility)

	// L'article 5001 n'est plus visible dans le cache actif
	_, errGone := object_cache_service.GetArticleFromCache(ctx, 5001)
	assert.Error(t, errGone)
}
