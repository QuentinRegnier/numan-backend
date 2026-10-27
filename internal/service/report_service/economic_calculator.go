package report_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/report_models"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # UTILITAIRE : CALCULATEUR D'IMPORTANCE ÉCONOMIQUE
// ############################################################################

// calculateEconomicImportance évalue la valeur "financière/rétention" d'une cible signalée en O(1).
// Plus le score est élevé, plus le signalement doit remonter en haut de la file des modérateurs.
func calculateEconomicImportance(ctx context.Context, input report_models.CreateReportInput) float64 {
	var totalEconomicValue = 0.0

	for _, targetID := range input.TargetIDs {
		switch input.TargetType {

		case variables.ReportTargetUser:
			// 1. Valeur de l'audience
			followerCount := cache_service.GetFollowerCount(ctx, targetID)
			totalEconomicValue += float64(followerCount) * variables.ReportScoreFollowerMultiplier

			// 2. Valeur du statut (Les VIP coûtent cher s'ils partent ou dérapent)
			if targetUserLite, errCache := cache_service.GetUserLite(ctx, targetID); errCache == nil {
				switch targetUserLite.Grade {
				case 1: // Certifié
					totalEconomicValue += variables.ReportScoreCertifiedBonus
				case 2: // Partenaire (Monétisé)
					totalEconomicValue += variables.ReportScorePartnerBonus
				case 3, 4: // Staff / Admin
					totalEconomicValue += variables.ReportScoreStaffBonus
				}
			}

		case variables.ReportTargetPost:
			// Valeur de rétention publicitaire (Object Cache LFU pour les compteurs chauds)
			if postPayload, errCache := object_cache_service.GetPostFromObjectCache(ctx, targetID); errCache == nil {

				viewsValue := float64(postPayload.ViewCount) * variables.ReportScoreViewMultiplier
				likesValue := float64(postPayload.LikeCount) * variables.ReportScoreLikeMultiplier
				commentsValue := float64(postPayload.CommentCount) * variables.ReportScoreCommentMultiplier

				// Le Dwell Sum est stocké en millisecondes, on le convertit en secondes
				dwellSeconds := postPayload.TelemetryDwellSum / 1000.0
				dwellValue := dwellSeconds * variables.ReportScoreDwellMultiplier

				totalEconomicValue += viewsValue + likesValue + commentsValue + dwellValue
			}

		case variables.ReportTargetComment:
			// Valeur d'engagement d'un commentaire
			if commentPayload, errCache := object_cache_service.GetCommentFromObjectCache(ctx, targetID); errCache == nil {
				totalEconomicValue += float64(commentPayload.LikeCount) * variables.ReportScoreCommentLikeMultiplier
			}
		}
	}

	return totalEconomicValue
}

// resolveTargetOwnerID détermine l'identifiant du propriétaire de l'entité signalée (Utilisateur B).
func resolveTargetOwnerID(ctx context.Context, targetType int, targetID int64) int64 {
	switch targetType {
	case variables.ReportTargetUser:
		return targetID
	case variables.ReportTargetPost:
		if p, err := object_cache_service.GetPostFromObjectCache(ctx, targetID); err == nil && p.ID != 0 {
			return p.UserID
		}
	case variables.ReportTargetComment:
		if c, err := object_cache_service.GetCommentFromObjectCache(ctx, targetID); err == nil && c.ID != 0 {
			return c.UserID
		}
	case variables.ReportTargetMessage:
		if m, err := object_cache_service.GetMessageFromObjectCache(ctx, targetID); err == nil && m.ID != 0 {
			return m.SenderID
		}
	}
	return 0
}

// resolveTargetIAScore récupère en O(1) RAM le score IA (P_ia) associé à un Post ou à un Message contenant une image.
func resolveTargetIAScore(ctx context.Context, targetType int, targetID int64) float64 {
	switch targetType {
	case variables.ReportTargetPost:
		if p, err := object_cache_service.GetPostFromObjectCache(ctx, targetID); err == nil && p.ID != 0 {
			if p.IAScoreMax > 0 {
				return p.IAScoreMax
			}
			maxScore := 0.0
			for _, mID := range p.MediaIDs {
				if med, errMed := object_cache_service.GetMediaFromObjectCache(ctx, mID); errMed == nil && med.IAScore > maxScore {
					maxScore = med.IAScore
				}
			}
			return maxScore
		}
	case variables.ReportTargetMessage:
		if m, err := object_cache_service.GetMessageFromObjectCache(ctx, targetID); err == nil && m.ID != 0 {
			if m.Attachments != nil {
				if rawScore, ok := m.Attachments["ia_score"].(float64); ok && rawScore > 0 {
					return rawScore
				}
				var mediaID int64
				switch v := m.Attachments["media_id"].(type) {
				case float64:
					mediaID = int64(v)
				case int64:
					mediaID = v
				}
				if mediaID > 0 {
					if med, errMed := object_cache_service.GetMediaFromObjectCache(ctx, mediaID); errMed == nil {
						return med.IAScore
					}
				}
			}
		}
	}
	return 0.0
}
