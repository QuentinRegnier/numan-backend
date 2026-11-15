package map_service

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : MUTATIONS ADMINISTRATEUR DES POINTS D'INTÉRÊT (WRITE-BEHIND)
// ############################################################################

// ImportPOIsService importe un lot de Points d'Intérêt en masse (Admin).
// Met à jour de manière synchrone l'Object Cache Redis L1 et l'index spatial GEO L1,
// puis ordonnance la persistance asynchrone vers PostgreSQL L3 (`TargetPostgres`).
func ImportPOIsService(
	ctx context.Context,
	adminID int64,
	input map_models.ImportPOIsInput,
) (map_models.ImportPOIsOutput, error) {
	if len(input.POIs) == 0 {
		return map_models.ImportPOIsOutput{}, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"La liste des points d'intérêt à importer ne peut pas être vide.",
			nil,
		)
	}
	if len(input.POIs) > variables.MaxPOIImportBatch {
		return map_models.ImportPOIsOutput{}, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("L'import est limité à %d points d'intérêt par requête.", variables.MaxPOIImportBatch),
			nil,
		)
	}

	payloads, ids, err := buildAndValidatePOIPayloads(input.POIs)
	if err != nil {
		return map_models.ImportPOIsOutput{}, err
	}

	// 1. Écriture synchrone L1 (Object Cache + GEO Index)
	if errCache := object_cache_service.SetPOIsBatchInObjectCache(ctx, payloads); errCache != nil {
		return map_models.ImportPOIsOutput{}, errCache
	}

	// 2. Ordonnancement Write-Behind vers PostgreSQL L3 (sans MongoDB L2)
	for _, p := range payloads {
		if errQ := redis.EnqueueDB(ctx, p.ID, p.ID, redis.EntityPOI, redis.ActionCreate, p, redis.TargetPostgres); errQ != nil {
			numan_log.Error(ctx).Err(errQ).Int64("poi_id", p.ID).Msg("Échec EnqueueDB lors de l'import du POI")
			return map_models.ImportPOIsOutput{}, errQ
		}
	}

	numan_log.Info(ctx).
		Int64(numan_log.LogKeyUserID, adminID).
		Action(numan_log.ActionCreate).
		Int("imported_count", len(payloads)).
		Msg("Importation en masse de POIs effectuée avec succès")

	views := BuildPOIViewsBatch(ctx, payloads, adminID)
	return map_models.ImportPOIsOutput{
		ImportedCount: len(payloads),
		POIIDs:        ids,
		POIs:          views,
	}, nil
}

// SetPOIsService crée un ou plusieurs Points d'Intérêt (Admin).
// Met à jour de manière synchrone l'Object Cache Redis L1 et l'index spatial GEO L1,
// puis publie les événements de création vers PostgreSQL L3 (`TargetPostgres`).
func SetPOIsService(
	ctx context.Context,
	adminID int64,
	input map_models.SetPOIInput,
) (map_models.SetPOIOutput, error) {
	items := input.NormalizeItems()
	if len(items) == 0 {
		return map_models.SetPOIOutput{}, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Aucun point d'intérêt valide fourni pour la création.",
			nil,
		)
	}
	if len(items) > variables.MaxPOISetBatch {
		return map_models.SetPOIOutput{}, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("La création simultanée est limitée à %d points d'intérêt.", variables.MaxPOISetBatch),
			nil,
		)
	}

	payloads, ids, err := buildAndValidatePOIPayloads(items)
	if err != nil {
		return map_models.SetPOIOutput{}, err
	}

	// 1. Écriture synchrone L1 (Object Cache + GEO Index)
	if errCache := object_cache_service.SetPOIsBatchInObjectCache(ctx, payloads); errCache != nil {
		return map_models.SetPOIOutput{}, errCache
	}

	// 2. Ordonnancement Write-Behind vers PostgreSQL L3
	for _, p := range payloads {
		if errQ := redis.EnqueueDB(ctx, p.ID, p.ID, redis.EntityPOI, redis.ActionCreate, p, redis.TargetPostgres); errQ != nil {
			numan_log.Error(ctx).Err(errQ).Int64("poi_id", p.ID).Msg("Échec EnqueueDB lors de la création du POI")
			return map_models.SetPOIOutput{}, errQ
		}
	}

	numan_log.Info(ctx).
		Int64(numan_log.LogKeyUserID, adminID).
		Action(numan_log.ActionCreate).
		Int("created_count", len(payloads)).
		Msg("Création de POI(s) effectuée avec succès")

	views := BuildPOIViewsBatch(ctx, payloads, adminID)
	return map_models.SetPOIOutput{
		CreatedCount: len(payloads),
		POIIDs:       ids,
		POIs:         views,
	}, nil
}

// UpdatePOIsService met à jour un ou plusieurs Points d'Intérêt existants (Admin).
// Réhydrate depuis PostgreSQL L3 si un POI est absent de L1, applique les modifications partielles,
// revalide l'entité complète, met à jour l'Object Cache + GEO Index L1, et publie `ActionUpdate` vers PostgreSQL L3.
func UpdatePOIsService(
	ctx context.Context,
	adminID int64,
	input map_models.UpdatePOIInput,
) (map_models.UpdatePOIOutput, error) {
	items := input.NormalizeItems()
	if len(items) == 0 {
		return map_models.UpdatePOIOutput{}, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Aucun point d'intérêt valide fourni pour la mise à jour.",
			nil,
		)
	}
	if len(items) > variables.MaxPOIUpdateBatch {
		return map_models.UpdatePOIOutput{}, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("La mise à jour simultanée est limitée à %d points d'intérêt.", variables.MaxPOIUpdateBatch),
			nil,
		)
	}

	targetIDs := make([]int64, 0, len(items))
	seen := make(map[int64]struct{}, len(items))
	for _, item := range items {
		if item.ID <= 0 {
			return map_models.UpdatePOIOutput{}, numan_error.NewBadRequest(
				numan_error.CodeInvalidPayload,
				"Chaque point d'intérêt à modifier doit posséder un identifiant strictement positif.",
				nil,
			)
		}
		if _, dup := seen[item.ID]; dup {
			return map_models.UpdatePOIOutput{}, numan_error.NewBadRequest(
				numan_error.CodeInvalidPayload,
				"Identifiant de point d'intérêt en doublon dans la requête de mise à jour.",
				nil,
			)
		}
		seen[item.ID] = struct{}{}
		targetIDs = append(targetIDs, item.ID)
	}

	// Récupération de l'état actuel depuis L1 (et fallback L3 pour les misses)
	existingMap, missingIDs, errCache := object_cache_service.GetPOIsBatchFromObjectCache(ctx, targetIDs)
	if errCache != nil {
		return map_models.UpdatePOIOutput{}, errCache
	}
	if len(missingIDs) > 0 {
		l3POIs, errL3 := postgres.FuncGetPOIsByIDs(ctx, missingIDs)
		if errL3 == nil {
			for _, p := range l3POIs {
				existingMap[p.ID] = p
			}
		}
	}

	now := domain.NowMillis()
	updatedPayloads := make([]map_models.POIPayload, 0, len(items))
	updatedIDs := make([]int64, 0, len(items))

	for _, patch := range items {
		current, exists := existingMap[patch.ID]
		if !exists || current.Visibility < variables.POIVisibilityPublic {
			return map_models.UpdatePOIOutput{}, numan_error.NewNotFound(
				numan_error.CodeNotFound,
				fmt.Sprintf("Le point d'intérêt %d est introuvable.", patch.ID),
				nil,
			)
		}

		if patch.Type != nil {
			current.Type = *patch.Type
		}
		if patch.Title != nil {
			current.Title = strings.TrimSpace(*patch.Title)
		}
		if patch.Description != nil {
			current.Description = strings.TrimSpace(*patch.Description)
		}
		if patch.Latitude != nil {
			current.Latitude = *patch.Latitude
		}
		if patch.Longitude != nil {
			current.Longitude = *patch.Longitude
		}
		if patch.Address != nil {
			current.Address = strings.TrimSpace(*patch.Address)
		}
		if patch.Phone != nil {
			current.Phone = strings.TrimSpace(*patch.Phone)
		}
		if patch.Website != nil {
			current.Website = strings.TrimSpace(*patch.Website)
		}
		if patch.Rating != nil {
			current.Rating = *patch.Rating
		}
		if patch.CommentCount != nil {
			current.CommentCount = *patch.CommentCount
		}
		if patch.MediaIDs != nil {
			if len(*patch.MediaIDs) == 0 {
				current.MediaIDs = []int64{}
			} else {
				current.MediaIDs = append([]int64(nil), (*patch.MediaIDs)...)
			}
		}
		if patch.GoogleMapsLink != nil {
			current.GoogleMapsLink = strings.TrimSpace(*patch.GoogleMapsLink)
		}
		if patch.AppleMapsLink != nil {
			current.AppleMapsLink = strings.TrimSpace(*patch.AppleMapsLink)
		}
		current.UpdatedAt = now

		if errVal := current.Validate(); errVal != nil {
			return map_models.UpdatePOIOutput{}, errVal
		}

		updatedPayloads = append(updatedPayloads, current)
		updatedIDs = append(updatedIDs, current.ID)
	}

	// 1. Mise à jour synchrone L1 (Object Cache + GEOADD qui déplace automatiquement les coordonnées)
	if errSet := object_cache_service.SetPOIsBatchInObjectCache(ctx, updatedPayloads); errSet != nil {
		return map_models.UpdatePOIOutput{}, errSet
	}

	// 2. Ordonnancement Write-Behind vers PostgreSQL L3
	for _, p := range updatedPayloads {
		if errQ := redis.EnqueueDB(ctx, p.ID, p.ID, redis.EntityPOI, redis.ActionUpdate, p, redis.TargetPostgres); errQ != nil {
			numan_log.Error(ctx).Err(errQ).Int64("poi_id", p.ID).Msg("Échec EnqueueDB lors de la mise à jour du POI")
			return map_models.UpdatePOIOutput{}, errQ
		}
	}

	numan_log.Info(ctx).
		Int64(numan_log.LogKeyUserID, adminID).
		Action(numan_log.ActionUpdate).
		Int("updated_count", len(updatedPayloads)).
		Msg("Mise à jour de POI(s) effectuée avec succès")

	views := BuildPOIViewsBatch(ctx, updatedPayloads, adminID)
	return map_models.UpdatePOIOutput{
		UpdatedCount: len(updatedPayloads),
		POIIDs:       updatedIDs,
		POIs:         views,
	}, nil
}

// DeletePOIsService effectue une suppression logique (Soft Delete) d'un ou plusieurs Points d'Intérêt (`[]POI_IDs`) (Admin).
// Purge de manière synchrone l'Object Cache Redis L1, l'index spatial GEO L1 (`ZREM`) et le cache des avis associés,
// puis ordonnance la suppression logique asynchrone en PostgreSQL L3 (`TargetPostgres`, `visibility = -1`) pour conservation légale.
func DeletePOIsService(
	ctx context.Context,
	adminID int64,
	input map_models.DeletePOIInput,
) (map_models.DeletePOIOutput, error) {
	if len(input.POIIDs) == 0 {
		return map_models.DeletePOIOutput{}, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"La liste des identifiants de points d'intérêt à supprimer ne peut pas être vide.",
			nil,
		)
	}
	if len(input.POIIDs) > variables.MaxPOIDeleteBatch {
		return map_models.DeletePOIOutput{}, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("La suppression simultanée est limitée à %d points d'intérêt.", variables.MaxPOIDeleteBatch),
			nil,
		)
	}

	uniqueIDs := make([]int64, 0, len(input.POIIDs))
	seen := make(map[int64]struct{}, len(input.POIIDs))
	for _, id := range input.POIIDs {
		if id <= 0 {
			return map_models.DeletePOIOutput{}, numan_error.NewBadRequest(
				numan_error.CodeInvalidPayload,
				"Tous les identifiants de points d'intérêt doivent être strictement positifs.",
				nil,
			)
		}
		if _, exists := seen[id]; !exists {
			seen[id] = struct{}{}
			uniqueIDs = append(uniqueIDs, id)
		}
	}

	// 1. Purge synchrone L1 (Object Cache + GEO ZREM + ZSET des avis associés)
	if errDel := object_cache_service.DeletePOIsFromObjectCache(ctx, uniqueIDs); errDel != nil {
		return map_models.DeletePOIOutput{}, errDel
	}
	for _, id := range uniqueIDs {
		object_cache_service.PurgePOIReviewsFromL1(ctx, id)
	}

	// 2. Ordonnancement Write-Behind Soft Delete vers PostgreSQL L3 (conservation légale des données et avis)
	now := domain.NowMillis()
	for _, id := range uniqueIDs {
		delPayload := map[string]any{
			"id":         id,
			"visibility": variables.POIVisibilitySoftDeleted,
			"updated_at": now,
		}
		if errQ := redis.EnqueueDB(ctx, id, id, redis.EntityPOI, redis.ActionDelete, delPayload, redis.TargetPostgres); errQ != nil {
			numan_log.Error(ctx).Err(errQ).Int64("poi_id", id).Msg("Échec EnqueueDB lors du Soft Delete du POI")
			return map_models.DeletePOIOutput{}, errQ
		}
	}

	numan_log.Info(ctx).
		Int64(numan_log.LogKeyUserID, adminID).
		Action(numan_log.ActionSoftDelete).
		Int("deleted_count", len(uniqueIDs)).
		Msg("Suppression logique (Soft Delete) de POI(s) effectuée avec succès")

	return map_models.DeletePOIOutput{
		DeletedCount: len(uniqueIDs),
		POIIDs:       uniqueIDs,
	}, nil
}

func buildAndValidatePOIPayloads(items []map_models.CreatePOIItemInput) ([]map_models.POIPayload, []int64, error) {
	now := domain.NowMillis()
	payloads := make([]map_models.POIPayload, 0, len(items))
	ids := make([]int64, 0, len(items))
	seenIDs := make(map[int64]struct{}, len(items))

	for _, item := range items {
		poiID := item.ID
		if poiID < 0 {
			return nil, nil, numan_error.NewBadRequest(
				numan_error.CodeInvalidPayload,
				"L'identifiant d'un point d'intérêt ne peut pas être négatif.",
				nil,
			)
		}
		if poiID == 0 {
			poiID = pkg.GenerateID()
		}
		if _, dup := seenIDs[poiID]; dup {
			return nil, nil, numan_error.NewBadRequest(
				numan_error.CodeInvalidPayload,
				fmt.Sprintf("L'identifiant de point d'intérêt %d est présent en doublon dans le lot.", poiID),
				nil,
			)
		}
		seenIDs[poiID] = struct{}{}

		mediaIDs := item.MediaIDs
		if mediaIDs == nil {
			mediaIDs = []int64{}
		} else {
			mediaIDs = append([]int64(nil), item.MediaIDs...)
		}

		p := map_models.POIPayload{
			ID:             poiID,
			Type:           item.Type,
			Title:          strings.TrimSpace(item.Title),
			Description:    strings.TrimSpace(item.Description),
			Latitude:       item.Latitude,
			Longitude:      item.Longitude,
			Address:        strings.TrimSpace(item.Address),
			Phone:          strings.TrimSpace(item.Phone),
			Website:        strings.TrimSpace(item.Website),
			Rating:         item.Rating,
			CommentCount:   item.CommentCount,
			MediaIDs:       mediaIDs,
			GoogleMapsLink: strings.TrimSpace(item.GoogleMapsLink),
			AppleMapsLink:  strings.TrimSpace(item.AppleMapsLink),
			Visibility:     variables.POIVisibilityPublic,
			CreatedAt:      now,
			UpdatedAt:      now,
		}

		if err := p.Validate(); err != nil {
			return nil, nil, err
		}

		payloads = append(payloads, p)
		ids = append(ids, p.ID)
	}

	return payloads, ids, nil
}
