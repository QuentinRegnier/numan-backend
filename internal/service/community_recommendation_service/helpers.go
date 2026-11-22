package community_recommendation_service

import (
	"context"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// resolveTargetCommunityLang résout la langue cible selon l'ordre :
// 1. Paramètre explicite normalisé
// 2. Préférence DisplayAndContent.Language de UserSettings
// 3. Langue par défaut de la plateforme ("fr")
func resolveTargetCommunityLang(ctx context.Context, callerID int64, explicitLang string) string {
	if strings.TrimSpace(explicitLang) != "" {
		if norm, ok := variables.NormalizeCommunityLang(explicitLang); ok {
			return norm
		}
	}
	if settings, err := object_cache_service.GetUserSettingsCascade(ctx, callerID); err == nil {
		langID := settings.DisplayAndContent.Language
		if variables.IsSupportedLanguage(langID) {
			if norm, ok := variables.NormalizeCommunityLang(variables.LangToISO(langID)); ok {
				return norm
			}
		}
	}
	return variables.DefaultLanguageISO
}

// loadConvMetasWithFallback lit les ConvMeta en batch L1 et auto-guérit les cache misses depuis L1 Object / L3 Postgres.
func loadConvMetasWithFallback(ctx context.Context, ids []int64) map[int64]lite_models.ConvLiteRequest {
	metas, missing, err := cache_service.GetConvMetaBatch(ctx, ids)
	if err != nil {
		numan_log.Warn(ctx).Err(err).Msg("Échec MGET ConvMeta pour hydratation recommandations communautés")
		metas = make(map[int64]lite_models.ConvLiteRequest)
		missing = ids
	}

	for _, id := range missing {
		if convObj, errObj := object_cache_service.GetConversationFromObjectCache(ctx, id); errObj == nil && convObj.ID != 0 {
			if convObj.State == variables.ConversationStateActive && convObj.Type == variables.ConversationTypeCommunityPub {
				lite := service.ToConvLiteRequest(convObj)
				metas[id] = lite
				_ = redis.ConvMeta.SetObject(ctx, id, lite)
			}
			continue
		}

		if pgConv, errPg := postgres.FuncGetConversation(ctx, id); errPg == nil && pgConv.ID != 0 {
			if pgConv.State == variables.ConversationStateActive && pgConv.Type == variables.ConversationTypeCommunityPub {
				lite := service.ToConvLiteRequest(pgConv)
				metas[id] = lite
				_ = object_cache_service.SetConversationInObjectCache(ctx, pgConv)
				_ = redis.ConvMeta.SetObject(ctx, id, lite)
			}
		}
	}
	return metas
}

// syncCommunityCachesAfterAdminMutation maintient la cohérence entre L3 (déjà muté),
// L1 Object Cache, L1 ConvMeta et L2 MongoDB (via Write-Behind TargetMongo).
func syncCommunityCachesAfterAdminMutation(
	ctx context.Context,
	convID int64,
	mutatePayload func(conversation_models.ConversationPayload) conversation_models.ConversationPayload,
	mutateLite func(lite_models.ConvLiteRequest) lite_models.ConvLiteRequest,
) {
	if convObj, err := object_cache_service.GetConversationFromObjectCache(ctx, convID); err == nil && convObj.ID != 0 {
		convObj = mutatePayload(convObj)
		convObj.UpdatedAt = domain.NowMillis()
		_ = object_cache_service.SetConversationInObjectCache(ctx, convObj)
		_ = redis.ConvMeta.SetObject(ctx, convID, service.ToConvLiteRequest(convObj))
		_ = redis.EnqueueDB(ctx, convObj.ID, convObj.ID, redis.EntityConversation, redis.ActionUpdate, convObj, redis.TargetMongo)
		return
	}

	// Si absent de l'Object Cache L1, mettre à jour ConvMeta en place si présent
	if redis.IsReady() && redis.ConvMeta != nil {
		var meta lite_models.ConvLiteRequest
		if err := redis.ConvMeta.GetObject(ctx, convID, &meta); err == nil && meta.ID != 0 {
			meta = mutateLite(meta)
			_ = redis.ConvMeta.SetObject(ctx, convID, meta)
		}
	}

	// Tenter une réhydratation L3 -> L1/L2 pour garantir la convergence Mongo
	if pgConv, errPg := postgres.FuncGetConversation(ctx, convID); errPg == nil && pgConv.ID != 0 {
		pgConv = mutatePayload(pgConv)
		_ = object_cache_service.SetConversationInObjectCache(ctx, pgConv)
		_ = redis.ConvMeta.SetObject(ctx, convID, service.ToConvLiteRequest(pgConv))
		_ = redis.EnqueueDB(ctx, pgConv.ID, pgConv.ID, redis.EntityConversation, redis.ActionUpdate, pgConv, redis.TargetMongo)
	}
}
