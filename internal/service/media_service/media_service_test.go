package media_service_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
)

func TestActivateMediaBatch_OnboardingQuarantineVsEstablished(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	newUser := int64(101)
	establishedUser := int64(202)
	now := time.Now().UnixMilli()

	// 1. Nouvel utilisateur (T = 0.20 < 0.40)
	require.NoError(t, redis.UsersLite.SetObject(ctx, newUser, lite_models.UserLiteRequest{
		ID:         newUser,
		Username:   "new_user",
		TrustScore: 0.20,
	}))

	// 2. Utilisateur établi (T = 0.85 >= 0.40)
	require.NoError(t, redis.UsersLite.SetObject(ctx, establishedUser, lite_models.UserLiteRequest{
		ID:         establishedUser,
		Username:   "established_user",
		TrustScore: 0.85,
	}))

	mediaNewUser := media_models.MediaPayload{
		ID:          1001,
		OwnerID:     newUser,
		StoragePath: "users/101/media/1001.avif",
		Visibility:  variables.MediaVisibilityPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, mediaNewUser))

	mediaEstablished := media_models.MediaPayload{
		ID:          2001,
		OwnerID:     establishedUser,
		StoragePath: "users/202/media/2001.avif",
		Visibility:  variables.MediaVisibilityPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, mediaEstablished))

	// Activation média pour nouvel utilisateur -> doit être placé en Quarantaine (Visibility = 2)
	errNew := media_service.ActivateMediaBatch(ctx, []int64{mediaNewUser.ID}, newUser)
	require.NoError(t, errNew)

	activatedNew, errGetNew := object_cache_service.GetMediaFromObjectCache(ctx, mediaNewUser.ID)
	require.NoError(t, errGetNew)
	assert.Equal(t, variables.MediaVisibilityQuarantine, int(activatedNew.Visibility),
		"Le média d'un nouvel utilisateur (T < 0.40) doit être placé en Quarantaine (Visibility = 2)")

	// Activation média pour utilisateur établi -> doit être activé en Public (Visibility = 1)
	errEst := media_service.ActivateMediaBatch(ctx, []int64{mediaEstablished.ID}, establishedUser)
	require.NoError(t, errEst)

	activatedEst, errGetEst := object_cache_service.GetMediaFromObjectCache(ctx, mediaEstablished.ID)
	require.NoError(t, errGetEst)
	assert.Equal(t, variables.MediaVisibilityPublic, int(activatedEst.Visibility),
		"Le média d'un utilisateur établi (T >= 0.40) doit être activé en Public (Visibility = 1)")

	// Zero-Trust : Tentative d'activation par un utilisateur non-propriétaire -> 403 Forbidden
	errForbidden := media_service.ActivateMediaBatch(ctx, []int64{mediaEstablished.ID}, newUser)
	require.Error(t, errForbidden)
	var appErr *numan_error.AppError
	require.ErrorAs(t, errForbidden, &appErr)
	assert.Equal(t, http.StatusForbidden, appErr.HTTPStatus)
}

func TestGenerateMediaViewCascade_AccessControl(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	now := time.Now().UnixMilli()

	// 1. Média en quarantaine (Visibility = 2)
	quarantineMedia := media_models.MediaPayload{
		ID:          3001,
		OwnerID:     500,
		StoragePath: "users/500/media/quarantine.avif",
		Visibility:  variables.MediaVisibilityQuarantine,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, quarantineMedia))

	// 2. Média rejeté par la modération (Visibility = -2)
	rejectedMedia := media_models.MediaPayload{
		ID:          3002,
		OwnerID:     500,
		StoragePath: "users/500/media/rejected.avif",
		Visibility:  variables.MediaVisibilityRejected,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, rejectedMedia))

	// 3. Média public (Visibility = 1)
	publicMedia := media_models.MediaPayload{
		ID:          3003,
		OwnerID:     500,
		StoragePath: "users/500/media/public.avif",
		Visibility:  variables.MediaVisibilityPublic,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, publicMedia))

	// Test Quarantined Media -> 403 CodeMediaUnderReview
	_, errQ := media_service.GenerateMediaViewCascade(ctx, quarantineMedia.ID, 500, 10, 999)
	require.Error(t, errQ)
	var appErrQ *numan_error.AppError
	require.ErrorAs(t, errQ, &appErrQ)
	assert.Equal(t, http.StatusForbidden, appErrQ.HTTPStatus)
	assert.Equal(t, numan_error.CodeMediaUnderReview, appErrQ.Code)

	// Test Rejected Media -> 403 CodeMediaRejected
	_, errR := media_service.GenerateMediaViewCascade(ctx, rejectedMedia.ID, 500, 10, 999)
	require.Error(t, errR)
	var appErrR *numan_error.AppError
	require.ErrorAs(t, errR, &appErrR)
	assert.Equal(t, http.StatusForbidden, appErrR.HTTPStatus)
	assert.Equal(t, numan_error.CodeMediaRejected, appErrR.Code)

	// Test Public Media -> 200 OK avec signed URL
	view, errP := media_service.GenerateMediaViewCascade(ctx, publicMedia.ID, 500, 10, 999)
	require.NoError(t, errP)
	assert.Equal(t, publicMedia.ID, view.MediaID)
	assert.NotEmpty(t, view.URL)
}

func TestGetSignedURLForClient_QuarantinedMediaDenial(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	now := time.Now().UnixMilli()

	quarantinedID := int64(4001)
	require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, media_models.MediaPayload{
		ID:          quarantinedID,
		OwnerID:     600,
		StoragePath: "users/600/media/q.avif",
		Visibility:  variables.MediaVisibilityQuarantine,
		CreatedAt:   now,
		UpdatedAt:   now,
	}))

	// Tentative de signature d'URL pour le média en quarantaine -> refusé avec 403 CodeMediaUnderReview
	_, errSign := media_service.GetSignedURLForClient(ctx, 999, media_models.SignMediaInput{
		MediaID: quarantinedID,
		PostID:  1234,
	})
	require.Error(t, errSign)
	var appErr *numan_error.AppError
	require.ErrorAs(t, errSign, &appErr)
	assert.Equal(t, http.StatusForbidden, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeMediaUnderReview, appErr.Code)
}

func TestDecodeMediaVisibility_BackwardCompatibility(t *testing.T) {
	// JSON
	vTrue, err := media_service.DecodeMediaVisibilityJSON([]byte("true"))
	require.NoError(t, err)
	assert.Equal(t, variables.MediaVisibilityPublic, vTrue)

	vFalse, err := media_service.DecodeMediaVisibilityJSON([]byte("false"))
	require.NoError(t, err)
	assert.Equal(t, variables.MediaVisibilityPending, vFalse)

	vInt, err := media_service.DecodeMediaVisibilityJSON([]byte("2"))
	require.NoError(t, err)
	assert.Equal(t, variables.MediaVisibilityQuarantine, vInt)

	_, errInvalid := media_service.DecodeMediaVisibilityJSON([]byte("invalid"))
	require.Error(t, errInvalid)

	// BSON
	bTrue := bson.RawValue{Type: bson.TypeBoolean, Value: []byte{0x01}}
	vBTrue, err := media_service.DecodeMediaVisibilityBSON(bTrue)
	require.NoError(t, err)
	assert.Equal(t, variables.MediaVisibilityPublic, vBTrue)

	bFalse := bson.RawValue{Type: bson.TypeBoolean, Value: []byte{0x00}}
	vBFalse, err := media_service.DecodeMediaVisibilityBSON(bFalse)
	require.NoError(t, err)
	assert.Equal(t, variables.MediaVisibilityPending, vBFalse)
}

func TestActivateMediaBatchWithMaxIAScore(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	ownerID := int64(8001)
	m1 := media_models.MediaPayload{ID: 101, OwnerID: ownerID, Visibility: variables.MediaVisibilityPending, IAScore: 0.12}
	m2 := media_models.MediaPayload{ID: 102, OwnerID: ownerID, Visibility: variables.MediaVisibilityPending, IAScore: 0.78}
	require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, m1))
	require.NoError(t, object_cache_service.SetMediaInObjectCache(ctx, m2))

	maxScore, err := media_service.ActivateMediaBatchWithMaxIAScore(ctx, []int64{101, 102}, ownerID)
	require.NoError(t, err)
	assert.Equal(t, 0.78, maxScore)
}
