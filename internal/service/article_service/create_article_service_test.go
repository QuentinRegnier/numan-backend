package article_service

import (
	"context"
	"errors"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateArticleService_SuccessCrossAdminAndActivation(t *testing.T) {
	ctx := context.Background()
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	t.Setenv("PUBLIC_MEDIA_BASE_URL", "https://cdn.numan.app/public")

	adminA := int64(9001)
	adminB := int64(9002)
	now := domain.NowMillis()

	for _, m := range []media_models.MediaPayload{
		{ID: 2001, OwnerID: adminA, StoragePath: "articles/cover-1.avif", Visibility: variables.MediaVisibilityPending, CreatedAt: now},
		{ID: 3001, OwnerID: adminB, StoragePath: "articles/doc-1.md", Visibility: variables.MediaVisibilityPending, CreatedAt: now},
		{ID: 4001, OwnerID: adminB, StoragePath: "articles/anim-1.gif", Visibility: variables.MediaVisibilityPending, CreatedAt: now},
		{ID: 4002, OwnerID: adminA, StoragePath: "articles/shared.webp", Visibility: variables.MediaVisibilityPending, CreatedAt: now},
	} {
		require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, m))
	}

	visPub := variables.ArticleVisibilityPublished
	out, err := CreateArticleService(ctx, adminA, article_models.CreateArticleInput{
		ID:                 5001,
		Title:              "Guide de Lyon",
		Summary:            "Les meilleures adresses",
		CoverMediaID:       2001,
		MarkdownMediaID:    3001,
		AttachmentMediaIDs: []int64{4001, 4002},
		PartnerURL:         "https://lyon-partner.fr/offre",
		Visibility:         &visPub,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(5001), out.Article.ID)
	assert.Equal(t, "https://cdn.numan.app/public/articles/cover-1.avif", out.Article.CoverURL)
	assert.Equal(t, "https://cdn.numan.app/public/articles/doc-1.md", out.Article.MarkdownURL)
	require.Len(t, out.Article.Attachments, 2)
	assert.Equal(t, "https://cdn.numan.app/public/articles/anim-1.gif", out.Article.Attachments[0].URL)
	assert.Equal(t, "https://cdn.numan.app/public/articles/shared.webp", out.Article.Attachments[1].URL)

	// Tous les médias référencés doivent être activés en Public (1)
	for _, mID := range []int64{2001, 3001, 4001, 4002} {
		m, errM := object_cache_service.GetMediaFromObjectCache(ctx, mID)
		require.NoError(t, errM)
		assert.Equal(t, variables.MediaVisibilityPublic, m.Visibility)
	}
}

func TestCreateArticleService_ValidationAndRollbackOnQueueFailure(t *testing.T) {
	ctx := context.Background()
	fakeRedis := test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)

	adminID := int64(9001)

	// Payload invalide rejeté avant toute écriture
	_, errInvalid := CreateArticleService(ctx, adminID, article_models.CreateArticleInput{
		Title: "",
	})
	assert.Error(t, errInvalid)

	// Échec EnqueueDB -> rollback immédiat de l'entrée L1
	require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, media_models.MediaPayload{
		ID:          3999,
		OwnerID:     adminID,
		StoragePath: "articles/doc-rollback.md",
		Visibility:  variables.MediaVisibilityPending,
		CreatedAt:   domain.NowMillis(),
	}))
	fakeRedis.SetFailCmd("RPUSH", errors.New("queue down"))
	_, errFailCreate := CreateArticleService(ctx, adminID, article_models.CreateArticleInput{
		ID:              5999,
		Title:           "Should Rollback",
		MarkdownMediaID: 3999,
	})
	assert.Error(t, errFailCreate)
	fakeRedis.SetFailCmd("RPUSH", nil)

	_, errRolledBack := object_cache_service.GetArticleFromCache(ctx, 5999)
	assert.Error(t, errRolledBack, "L'article 5999 doit avoir été retiré de L1 après l'échec d'EnqueueDB")
}
