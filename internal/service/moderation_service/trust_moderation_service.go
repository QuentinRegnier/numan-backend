package moderation_service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// PublishTrustScoreEvent publie un événement de réputation dans la file Redis du Worker sans bloquer la requête HTTP.
func PublishTrustScoreEvent(ctx context.Context, userID int64, eventType string, targetID int64, reason string) error {
	if userID <= 0 {
		return nil
	}
	evt := moderation_models.TrustScoreEventPayload{
		EventID:      pkg.GenerateID(),
		UserID:       userID,
		EventType:    eventType,
		TargetID:     targetID,
		Reason:       reason,
		OccurredAtMs: domain.NowMillis(),
	}
	payloadBytes, err := json.Marshal(evt)
	if err != nil {
		return numan_error.NewInternal(err)
	}
	if errQueue := redis.WorkerQueue.LPush(ctx, variables.WorkerQueueTrustScore, payloadBytes); errQueue != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de publier l'événement de réputation dans la file d'attente.",
			errQueue,
		)
	}
	return nil
}

// ApplyPreventiveShadowbanToPost masque préventivement un post (Visibility = 3) et ses médias (Visibility = 2) en L1, L2 et L3.
func ApplyPreventiveShadowbanToPost(ctx context.Context, postID int64) {
	postPayload, err := object_cache_service.GetPostFromObjectCache(ctx, postID)
	if err != nil || postPayload.ID == 0 {
		postsPg, errPg := postgres.FuncLoadPosts(ctx, []int64{postID}, 1, 0)
		if errPg != nil || len(postsPg) == 0 {
			return
		}
		postPayload = postsPg[0]
	}

	nowMs := domain.NowMillis()
	postPayload.Visibility = variables.PostVisibilityQuarantine
	postPayload.UpdatedAt = nowMs

	_ = object_cache_service.SetPostInObjectCache(ctx, postPayload)
	_ = cache_service.RemovePostFromUserProfile(ctx, postPayload.UserID, postPayload.ID)
	_ = redis.EnqueueDB(ctx, postPayload.ID, postPayload.UserID, redis.EntityPost, redis.ActionUpdate, postPayload, redis.TargetAll)

	// Mise en quarantaine préventive des médias rattachés
	for _, mediaID := range postPayload.MediaIDs {
		mediaPayload, errMed := postgres.FuncGetMedia(ctx, mediaID)
		if errMed == nil && mediaPayload.ID != 0 {
			mediaPayload.Visibility = variables.MediaVisibilityQuarantine
			mediaPayload.UpdatedAt = nowMs
			_ = object_cache_service.SetMediaInObjectCache(ctx, mediaPayload)
			_ = redis.EnqueueDB(ctx, mediaPayload.ID, mediaPayload.OwnerID, redis.EntityMedia, redis.ActionUpdate, mediaPayload, redis.TargetAll)
		}
	}

	numan_log.Info(ctx).
		Entity("post", postID).
		Action(numan_log.ActionUpdate).
		Msg("Shadowban préventif appliqué au post et à ses médias suite au calcul d'impact de signalement.")
}

// ReviewQuarantineMedia traite la décision d'un modérateur sur un média/post en quarantaine.
func ReviewQuarantineMedia(ctx context.Context, moderatorID int64, input moderation_models.ReviewQuarantineInput) (moderation_models.ReviewQuarantineOutput, error) {
	// Vérification de sécurité : le modérateur doit avoir Grade >= UserGradeModerator (3)
	modLite, errMod := cache_service.GetUserLite(ctx, moderatorID)
	if errMod != nil || modLite.Grade < variables.UserGradeModerator {
		return moderation_models.ReviewQuarantineOutput{}, numan_error.NewForbidden(
			numan_error.CodeForbidden,
			"Accès réservé aux modérateurs et administrateurs.",
			errMod,
		)
	}

	// Chargement du média (L2 -> L3)
	mediaPayload, errL2 := mongo.MongoGetMediaByID(ctx, input.MediaID)
	if errL2 != nil || mediaPayload.ID == 0 {
		var errPg error
		mediaPayload, errPg = postgres.FuncGetMedia(ctx, input.MediaID)
		if errPg != nil {
			var appErr *numan_error.AppError
			if errors.As(errPg, &appErr) && appErr.HTTPStatus == http.StatusNotFound {
				return moderation_models.ReviewQuarantineOutput{}, numan_error.NewNotFound(
					numan_error.CodeNotFound,
					"Le média ciblé est introuvable.",
					errPg,
				)
			}
			return moderation_models.ReviewQuarantineOutput{}, errPg
		}
		if mediaPayload.ID == 0 {
			return moderation_models.ReviewQuarantineOutput{}, numan_error.NewNotFound(
				numan_error.CodeNotFound,
				"Le média ciblé est introuvable.",
				nil,
			)
		}
	}

	nowMs := domain.NowMillis()
	var newMediaVisibility int
	var trustEvent string

	switch input.Decision {
	case "approve":
		newMediaVisibility = variables.MediaVisibilityPublic
		trustEvent = variables.TrustEventQuarantineApproved
	case "reject_minor":
		newMediaVisibility = variables.MediaVisibilityRejected
		trustEvent = variables.TrustEventMinorInfraction
	case "reject_major":
		newMediaVisibility = variables.MediaVisibilityRejected
		trustEvent = variables.TrustEventMajorInfraction
	default:
		return moderation_models.ReviewQuarantineOutput{}, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Décision de modération invalide.",
			nil,
		)
	}

	mediaPayload.Visibility = newMediaVisibility
	mediaPayload.UpdatedAt = nowMs

	// Mise à jour L1 + Write-Behind L2/L3 du Média
	if newMediaVisibility == variables.MediaVisibilityPublic {
		_ = object_cache_service.SetMediaInObjectCache(ctx, mediaPayload)
	} else {
		_ = object_cache_service.DeleteMediaFromObjectCache(ctx, mediaPayload.ID)
	}
	if errQueue := redis.EnqueueDB(ctx, mediaPayload.ID, mediaPayload.OwnerID, redis.EntityMedia, redis.ActionUpdate, mediaPayload, redis.TargetAll); errQueue != nil {
		return moderation_models.ReviewQuarantineOutput{}, numan_error.NewInternal(errQueue)
	}

	// Si un PostID est associé, débloquer ou supprimer le post correspondant
	if input.PostID > 0 {
		postsPg, errPg := postgres.FuncLoadPosts(ctx, []int64{input.PostID}, 1, 0)
		if errPg == nil && len(postsPg) > 0 {
			post := postsPg[0]
			if input.Decision == "approve" {
				post.Visibility = variables.PostVisibilityAll
				post.UpdatedAt = nowMs
				_ = object_cache_service.SetPostInObjectCache(ctx, post)
				_ = cache_service.AddPostToUserProfile(ctx, post.UserID, post.ID, float64(post.CreatedAt))
				_ = redis.EnqueueDB(ctx, post.ID, post.UserID, redis.EntityPost, redis.ActionUpdate, post, redis.TargetAll)
			} else {
				post.Visibility = variables.PostVisibilityModerated
				post.UpdatedAt = nowMs
				_ = object_cache_service.DeletePostFromObjectCache(ctx, post.ID)
				_ = cache_service.RemovePostFromUserProfile(ctx, post.UserID, post.ID)
				_ = redis.EnqueueDB(ctx, post.ID, post.UserID, redis.EntityPost, redis.ActionDelete, post, redis.TargetAll)
			}
		}
	}

	// Publication asynchrone de l'événement de Trust Score (+0.05, -0.30 ou mise à 0.00)
	if errEvt := PublishTrustScoreEvent(ctx, mediaPayload.OwnerID, trustEvent, input.PostID, pkg.CleanStr(input.Reason)); errEvt != nil {
		numan_log.Error(ctx).Err(errEvt).Int64("owner_id", mediaPayload.OwnerID).Msg("Échec d'émission de l'événement TrustScore après revue de quarantaine")
		return moderation_models.ReviewQuarantineOutput{}, errEvt
	}

	return moderation_models.ReviewQuarantineOutput{
		MediaID:       mediaPayload.ID,
		NewVisibility: newMediaVisibility,
		TrustEvent:    trustEvent,
		UpdatedAt:     nowMs,
	}, nil
}

// GetModerationQueue récupère les médias en quarantaine et les signalements en attente (triés par priorité).
func GetModerationQueue(ctx context.Context, callerID int64, input moderation_models.GetModerationQueueInput) (moderation_models.GetModerationQueueOutput, error) {
	modLite, errMod := cache_service.GetUserLite(ctx, callerID)
	if errMod != nil || modLite.Grade < variables.UserGradeModerator {
		return moderation_models.GetModerationQueueOutput{}, numan_error.NewForbidden(
			numan_error.CodeForbidden,
			"Accès réservé aux modérateurs et administrateurs.",
			errMod,
		)
	}

	limit := input.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	offset := input.Offset
	if offset < 0 {
		offset = 0
	}

	quarantinedMedia, errMedia := postgres.FuncLoadQuarantinedMediaPaginated(ctx, limit, offset)
	if errMedia != nil {
		return moderation_models.GetModerationQueueOutput{}, errMedia
	}

	// Conversion explicite de *int en paramètre optionnel
	pendingReports, errReports := postgres.FuncLoadPendingReportsFilteredPaginated(
		ctx, limit, offset, input.OriginFilter, input.TargetTypeFilter,
	)
	if errReports != nil {
		return moderation_models.GetModerationQueueOutput{}, errReports
	}

	items := make([]moderation_models.QuarantinedMediaItem, 0, len(quarantinedMedia))
	for _, m := range quarantinedMedia {
		ownerScore := variables.TrustScoreInitial
		if ownerLite, errOwner := cache_service.GetUserLite(ctx, m.OwnerID); errOwner == nil && ownerLite.TrustScore > 0 {
			ownerScore = ownerLite.TrustScore
		}
		itemPriority := ComputeGlobalModerationPriority(m.IAScore, ownerScore, 0.0, 0.0) // <-- Retrait de targetType
		items = append(items, moderation_models.QuarantinedMediaItem{
			MediaID:       m.ID,
			OwnerID:       m.OwnerID,
			OwnerScore:    ownerScore,
			IAScore:       m.IAScore,
			PriorityScore: itemPriority,
			SignedURL:     m.StoragePath,
			CreatedAt:     m.CreatedAt,
		})
	}

	stats := moderation_models.ModerationQueueStats{
		TotalQuarantinedMedia: len(items),
		TotalPendingReports:   len(pendingReports),
	}
	for _, rep := range pendingReports {
		switch rep.ReportOrigin {
		case variables.ReportOriginAutoAI:
			stats.AutoAIReportsCount++
		case variables.ReportOriginHybrid:
			stats.HybridReportsCount++
		case variables.ReportOriginSystem:
			stats.SystemReportsCount++
		default:
			stats.UserReportsCount++
		}
	}

	output := moderation_models.GetModerationQueueOutput{
		Stats:            stats,
		QuarantinedMedia: items,
		PendingReports:   pendingReports,
	}

	// Récupération dynamique depuis les ZSETs Redis si une sous-file spécifique est demandée (§C.9–C.12)
	if input.QueueType != "" {
		isAdmin := modLite.Grade >= variables.UserGradeAdmin
		if input.QueueType == variables.ModerationQueueAdmin && !isAdmin {
			return moderation_models.GetModerationQueueOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "File réservée aux administrateurs.", nil)
		}
		if input.QueueType == variables.ModerationQueueFinalVote {
			modStats, _ := cache_service.GetOrInitModeratorStats(ctx, callerID, modLite.Grade)
			if !isAdmin && (modStats == nil || modStats.ModeratorTrustScore <= variables.SuperModeratorTrustThreshold) {
				return moderation_models.GetModerationQueueOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "File réservée aux Super-Modérateurs (fiabilité > 0.90).", nil)
			}
		}

		records, errRecs := cache_service.ListModerationQueueRecords(ctx, input.QueueType, offset, limit)
		if errRecs == nil {
			views := make([]moderation_models.ModerationTicketPublicView, 0, len(records))
			for _, r := range records {
				views = append(views, ToPublicTicketView(r, isAdmin))
			}
			output.QueueTickets = views
		}
	}

	return output, nil
}
