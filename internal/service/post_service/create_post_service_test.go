package post_service_test

import (
	"context"
	"database/sql/driver"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/moderation_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/post_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/QuentinRegnier/numan-backend/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreatePost_Wiring_QuarantineOnHighRiskAI(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	authorID := int64(8001)
	mediaID := int64(9001)

	// Auteur établi avec bon score de confiance (0.85)
	authorLite := lite_models.UserLiteRequest{
		ID:         authorID,
		Username:   "established_author",
		TrustScore: 0.85,
		Grade:      0,
	}
	require.NoError(t, redis.UsersLite.SetObject(ctx, authorID, authorLite))

	// Média contenant une image à risque élevé IA (ia_score = 0.90 -> R_auto >= 0.80)
	mediaPayload := media_models.MediaPayload{
		ID:          mediaID,
		OwnerID:     authorID,
		StoragePath: "users/8001/media/risk.avif",
		Visibility:  variables.MediaVisibilityPending,
		IAScore:     0.90,
		CreatedAt:   time.Now().UnixMilli(),
		UpdatedAt:   time.Now().UnixMilli(),
	}
	require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, mediaPayload))

	// Création du post demandant Visibility 0 (Public)
	input := post_models.CreatePostInput{
		Content:    "Post avec média à risque IA élevé",
		MediaIDs:   []int64{mediaID},
		Visibility: variables.PostVisibilityAll, // 0
		IAScores:   []float64{0.90},
	}

	postID, errCreate := post_service.CreatePost(ctx, authorID, input)
	require.NoError(t, errCreate)
	require.True(t, postID > 0)

	// Vérification de la mise en quarantaine forcée (Visibility = 3)
	createdPost, errGet := object_cache_service.GetPostFromObjectCache(ctx, postID)
	require.NoError(t, errGet)
	assert.Equal(t, variables.PostVisibilityQuarantine, createdPost.Visibility, "Le post avec R_auto >= 0.80 doit être forcé en Quarantaine (Visibility = 3)")
}

func TestCreatePost_Wiring_AutoQuarantineNewUser(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	newUserID := int64(8002)
	mediaID := int64(9002)

	// Nouvel utilisateur : T_auteur = 0.20 (< T_seuil = 0.40)
	newUserLite := lite_models.UserLiteRequest{
		ID:         newUserID,
		Username:   "new_user_onboarding",
		TrustScore: variables.TrustScoreInitial, // 0.20
		Grade:      0,
	}
	require.NoError(t, redis.UsersLite.SetObject(ctx, newUserID, newUserLite))

	// Média sain (ia_score = 0.05)
	mediaPayload := media_models.MediaPayload{
		ID:          mediaID,
		OwnerID:     newUserID,
		StoragePath: "users/8002/media/clean.avif",
		Visibility:  variables.MediaVisibilityPending,
		IAScore:     0.05,
		CreatedAt:   time.Now().UnixMilli(),
		UpdatedAt:   time.Now().UnixMilli(),
	}
	require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, mediaPayload))

	input := post_models.CreatePostInput{
		Content:    "Premier post d'un nouvel utilisateur",
		MediaIDs:   []int64{mediaID},
		Visibility: variables.PostVisibilityAll, // 0
	}

	postID, errCreate := post_service.CreatePost(ctx, newUserID, input)
	require.NoError(t, errCreate)
	require.True(t, postID > 0)

	// Vérification de la mise en quarantaine automatique du nouvel utilisateur (T < 0.40)
	createdPost, errGet := object_cache_service.GetPostFromObjectCache(ctx, postID)
	require.NoError(t, errGet)
	assert.Equal(t, variables.PostVisibilityQuarantine, createdPost.Visibility, "Les posts d'un nouvel utilisateur (T < 0.40) doivent être auto-mis en quarantaine (Visibility = 3)")
}

func TestCreatePost_Wiring_CleanPostEstablishedUser(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	establishedUserID := int64(8003)
	mediaID := int64(9003)

	// Utilisateur établi : T_auteur = 0.85 (> 0.40)
	userLite := lite_models.UserLiteRequest{
		ID:         establishedUserID,
		Username:   "established_trusted",
		TrustScore: 0.85,
		Grade:      0,
	}
	require.NoError(t, redis.UsersLite.SetObject(ctx, establishedUserID, userLite))

	mediaPayload := media_models.MediaPayload{
		ID:          mediaID,
		OwnerID:     establishedUserID,
		StoragePath: "users/8003/media/normal.avif",
		Visibility:  variables.MediaVisibilityPending,
		IAScore:     0.05,
		CreatedAt:   time.Now().UnixMilli(),
		UpdatedAt:   time.Now().UnixMilli(),
	}
	require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, mediaPayload))

	input := post_models.CreatePostInput{
		Content:    "Post normal d'un utilisateur de confiance",
		MediaIDs:   []int64{mediaID},
		Visibility: variables.PostVisibilityAll, // 0
	}

	postID, errCreate := post_service.CreatePost(ctx, establishedUserID, input)
	require.NoError(t, errCreate)
	require.True(t, postID > 0)

	createdPost, errGet := object_cache_service.GetPostFromObjectCache(ctx, postID)
	require.NoError(t, errGet)
	assert.Equal(t, variables.PostVisibilityAll, createdPost.Visibility, "Un utilisateur de confiance avec média sain doit voir son post publié normalement (Visibility = 0)")
}

func TestCreatePost_Wiring_ModeratorValidationGrantsBonusAndExitsQuarantine(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	userID := int64(8004)
	modID := int64(9004)
	mediaID1 := int64(9005)
	mediaID2 := int64(9006)

	// Modérateur Grade 3
	modLite := lite_models.UserLiteRequest{
		ID:    modID,
		Grade: variables.UserGradeModerator,
	}
	require.NoError(t, redis.UsersLite.SetObject(ctx, modID, modLite))

	// Utilisateur à 0.35 (sous le seuil 0.40)
	userLite := lite_models.UserLiteRequest{
		ID:         userID,
		Username:   "growing_user",
		TrustScore: 0.35,
		Grade:      0,
	}
	require.NoError(t, redis.UsersLite.SetObject(ctx, userID, userLite))

	// Média 1 en quarantaine
	now := time.Now().UnixMilli()
	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		if strings.Contains(query, "content.get_media") {
			return test.NewMockRows(
				[]string{"id", "owner_id", "storage_path", "visibility", "created_at", "updated_at"},
				[][]driver.Value{{mediaID1, userID, "uploads/pic1.avif", int64(2), now, now}},
			), nil
		}
		if strings.Contains(query, "func_apply_trust_score_delta") {
			return test.NewMockRows(
				[]string{"new_score", "is_banned"},
				[][]driver.Value{{0.40, false}},
			), nil
		}
		return test.NewMockRows([]string{}, nil), nil
	}, nil)

	// 1. Le modérateur valide le média/post en quarantaine
	reviewOut, errReview := moderation_service.ReviewQuarantineMedia(ctx, modID, moderation_models.ReviewQuarantineInput{
		MediaID:  mediaID1,
		Decision: "approve",
		Reason:   "Média conforme validé par modérateur",
	})
	require.NoError(t, errReview)
	assert.Equal(t, variables.TrustEventQuarantineApproved, reviewOut.TrustEvent)

	// 2. Traitement de l'événement par le TrustScoreWorker
	worker.DrainAndProcessTrustEvents(ctx)

	// 3. Vérification que le Trust Score de l'utilisateur a augmenté de +0.05 (0.35 -> 0.40 >= T_seuil)
	updatedUser, errUser := cache_service.GetUserLite(ctx, userID)
	require.NoError(t, errUser)
	assert.InDelta(t, 0.40, updatedUser.TrustScore, 1e-4, "La validation de quarantaine doit accorder +0.05")

	// 4. L'utilisateur crée un nouveau post : avec T = 0.40 >= T_seuil, il SORT de la quarantaine
	mediaPayload2 := media_models.MediaPayload{
		ID:          mediaID2,
		OwnerID:     userID,
		StoragePath: "users/8004/media/post2.avif",
		Visibility:  variables.MediaVisibilityPending,
		IAScore:     0.05,
		CreatedAt:   time.Now().UnixMilli(),
		UpdatedAt:   time.Now().UnixMilli(),
	}
	require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, mediaPayload2))

	newPostID, errPost2 := post_service.CreatePost(ctx, userID, post_models.CreatePostInput{
		Content:    "Deuxième post après validation et gain de confiance",
		MediaIDs:   []int64{mediaID2},
		Visibility: variables.PostVisibilityAll,
	})
	require.NoError(t, errPost2)

	post2, errGet2 := object_cache_service.GetPostFromObjectCache(ctx, newPostID)
	require.NoError(t, errGet2)
	assert.Equal(t, variables.PostVisibilityAll, post2.Visibility, "Une fois le seuil de 0.40 atteint, l'utilisateur sort de la quarantaine automatique (Visibility = 0)")
}

func TestOnboarding_FourValidationsStepToExit(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	onboardingUserID := int64(8888)
	modID := int64(9999)

	// Modérateur
	modLite := lite_models.UserLiteRequest{
		ID:    modID,
		Grade: variables.UserGradeModerator,
	}
	require.NoError(t, redis.UsersLite.SetObject(ctx, modID, modLite))

	// Nouvel utilisateur commençant à T = 0.20
	currentTrust := variables.TrustScoreInitial // 0.20
	newUserLite := lite_models.UserLiteRequest{
		ID:         onboardingUserID,
		Username:   "onboarding_user",
		TrustScore: currentTrust,
		Grade:      0,
	}
	require.NoError(t, redis.UsersLite.SetObject(ctx, onboardingUserID, newUserLite))

	now := time.Now().UnixMilli()
	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		if strings.Contains(query, "content.get_media") {
			var requestedMediaID int64 = 10000
			if len(args) > 0 {
				if idVal, ok := args[0].Value.(int64); ok {
					requestedMediaID = idVal
				}
			}
			return test.NewMockRows(
				[]string{"id", "owner_id", "storage_path", "visibility", "created_at", "updated_at"},
				[][]driver.Value{{requestedMediaID, onboardingUserID, "uploads/pic.avif", int64(2), now, now}},
			), nil
		}
		if strings.Contains(query, "func_apply_trust_score_delta") {
			currentTrust = math.Round((currentTrust+0.05)*10000) / 10000
			return test.NewMockRows(
				[]string{"new_score", "is_banned"},
				[][]driver.Value{{currentTrust, false}},
			), nil
		}
		return test.NewMockRows([]string{}, nil), nil
	}, nil)

	expectedScores := []float64{0.25, 0.30, 0.35, 0.40}

	// 4 itérations de : Création d'un post -> Quarantaine forcée -> Validation modérateur -> +0.05
	for step := 0; step < 4; step++ {
		mediaID := int64(10001 + step)
		mediaPayload := media_models.MediaPayload{
			ID:          mediaID,
			OwnerID:     onboardingUserID,
			StoragePath: fmt.Sprintf("users/8888/media/step%d.avif", step+1),
			Visibility:  variables.MediaVisibilityPending,
			IAScore:     0.05,
			CreatedAt:   time.Now().UnixMilli(),
			UpdatedAt:   time.Now().UnixMilli(),
		}
		require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, mediaPayload))

		// Création du post
		postID, errCreate := post_service.CreatePost(ctx, onboardingUserID, post_models.CreatePostInput{
			Content:    fmt.Sprintf("Post onboarding étape %d", step+1),
			MediaIDs:   []int64{mediaID},
			Visibility: variables.PostVisibilityAll,
		})
		require.NoError(t, errCreate)
		require.True(t, postID > 0)

		// Assertion : le post est en quarantaine (Visibility = 3) car T < 0.40
		quarantinedPost, errGet := object_cache_service.GetPostFromObjectCache(ctx, postID)
		require.NoError(t, errGet)
		assert.Equal(t, variables.PostVisibilityQuarantine, quarantinedPost.Visibility,
			fmt.Sprintf("Étape %d : le post doit être en quarantaine (T=%f < 0.40)", step+1, currentTrust))

		// Le modérateur valide le média/post
		_, errReview := moderation_service.ReviewQuarantineMedia(ctx, modID, moderation_models.ReviewQuarantineInput{
			MediaID:  mediaID,
			Decision: "approve",
			Reason:   "Média sain validé par modérateur",
		})
		require.NoError(t, errReview)

		// Traitement asynchrone des événements de réputation
		worker.DrainAndProcessTrustEvents(ctx)

		// Vérification du nouveau Trust Score
		userLiteAfter, errU := cache_service.GetUserLite(ctx, onboardingUserID)
		require.NoError(t, errU)
		assert.InDelta(t, expectedScores[step], userLiteAfter.TrustScore, 1e-4,
			fmt.Sprintf("Étape %d : le Trust Score doit être exactement à %f", step+1, expectedScores[step]))
	}

	// 5ème post : l'utilisateur a maintenant T = 0.40 >= T_seuil (0.40).
	// Le post doit être immédiatement publié en Public (Visibility = 0) SANS passer par la quarantaine.
	mediaID5 := int64(10005)
	mediaPayload5 := media_models.MediaPayload{
		ID:          mediaID5,
		OwnerID:     onboardingUserID,
		StoragePath: "users/8888/media/step5_public.avif",
		Visibility:  variables.MediaVisibilityPending,
		IAScore:     0.05,
		CreatedAt:   time.Now().UnixMilli(),
		UpdatedAt:   time.Now().UnixMilli(),
	}
	require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, mediaPayload5))

	postID5, errCreate5 := post_service.CreatePost(ctx, onboardingUserID, post_models.CreatePostInput{
		Content:    "Cinquième post après sortie de quarantaine onboarding !",
		MediaIDs:   []int64{mediaID5},
		Visibility: variables.PostVisibilityAll,
	})
	require.NoError(t, errCreate5)

	post5, errGet5 := object_cache_service.GetPostFromObjectCache(ctx, postID5)
	require.NoError(t, errGet5)
	assert.Equal(t, variables.PostVisibilityAll, post5.Visibility,
		"Le 5ème post doit être immédiatement Public (Visibility = 0) car l'utilisateur a atteint le seuil T=0.40 et est sorti de quarantaine !")
}
