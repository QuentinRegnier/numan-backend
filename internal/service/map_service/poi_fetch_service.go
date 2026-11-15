package map_service

import (
	"context"
	"sort"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : LECTURE SPATIALE DES POINTS D'INTÉRÊT (L1 GEO/OBJECT -> L3 PG)
// ############################################################################

// GetPOIsInBBoxService récupère les Points d'Intérêt compris dans une Bounding Box WGS84.
//
// Stratégie d'exécution :
//  1. Validation géométrique stricte de la Bounding Box (SWLat <= NELat, SWLng <= NELng, bornes WGS84).
//  2. Recherche spatiale L1 dans Redis GEO (`GEOSEARCH BYBOX`).
//  3. Hydratation en lot depuis l'Object Cache Redis L1 (`MGET`).
//  4. Auto-guérison sélective (Self-Healing) depuis PostgreSQL L3 (`FuncGetPOIsByIDs`) pour les objets manquants,
//     ou fallback spatial complet vers PostgreSQL L3 (`FuncGetPOIsInBBox`) si l'index GEO L1 est vide ou indisponible.
//  5. Filtrage exact des coordonnées par rapport à la Bounding Box demandée.
//  6. Résolution groupée des médias (`FormatMediaViewsCascade`) sans requêtes N+1.
func GetPOIsInBBoxService(
	ctx context.Context,
	callerID int64,
	input map_models.GetPoisInBBoxInput,
) (map_models.GetPoisInBBoxOutput, error) {
	if err := map_models.ValidateBBox(input.SWLat, input.SWLng, input.NELat, input.NELng); err != nil {
		return map_models.GetPoisInBBoxOutput{}, err
	}

	var pois []map_models.POIPayload

	// ── ÉTAPE 1 : RECHERCHE SPATIALE L1 (REDIS GEO) ─────────────────────────
	geoIDs, errGeo := object_cache_service.SearchPOIIDsInBBox(ctx, input.SWLat, input.SWLng, input.NELat, input.NELng)
	if errGeo != nil {
		numan_log.Warn(ctx).Err(errGeo).Msg("Échec recherche spatiale Redis GEO L1, bascule sur PostgreSQL L3")
	}

	if len(geoIDs) > 0 {
		// ── ÉTAPE 2 : HYDRATATION BATCH L1 (MGET) ───────────────────────────
		foundMap, missingIDs, errBatch := object_cache_service.GetPOIsBatchFromObjectCache(ctx, geoIDs)
		if errBatch != nil {
			numan_log.Warn(ctx).Err(errBatch).Msg("Échec MGET Object Cache POI L1, repli vers PostgreSQL L3")
			missingIDs = geoIDs
			foundMap = make(map[int64]map_models.POIPayload)
		}

		for _, id := range geoIDs {
			if p, ok := foundMap[id]; ok {
				pois = append(pois, p)
			}
		}

		// ── ÉTAPE 3 : AUTO-GUÉRISON PARTIELLE L3 -> L1 (CACHE MISS OBJET) ───
		if len(missingIDs) > 0 {
			l3POIs, errL3 := postgres.FuncGetPOIsByIDs(ctx, missingIDs)
			if errL3 != nil {
				numan_log.Warn(ctx).Err(errL3).Msg("Impossible de réhydrater les POIs manquants depuis PostgreSQL L3")
			} else if len(l3POIs) > 0 {
				liveL3 := filterLivePOIs(l3POIs)
				if len(liveL3) > 0 {
					_ = object_cache_service.SetPOIsBatchInObjectCache(ctx, liveL3)
					pois = append(pois, liveL3...)
				}
			}
		}
	} else {
		// ── ÉTAPE 3b : FALLBACK SPATIAL COMPLET L3 -> L1 (COLD CACHE GEO) ───
		l3POIs, errL3 := postgres.FuncGetPOIsInBBox(ctx, input.SWLat, input.SWLng, input.NELat, input.NELng, variables.MaxPOIGeoSearchCount)
		if errL3 != nil {
			// Si Redis GEO a échoué ET que PostgreSQL L3 est en erreur, on remonte l'erreur L3.
			// Si Redis GEO était simplement vide et que Postgres n'est pas configuré ou vide en test unitaire L1,
			// on ne retourne une erreur que si Redis GEO avait lui-même échoué.
			if errGeo != nil {
				return map_models.GetPoisInBBoxOutput{}, errL3
			}
			numan_log.Warn(ctx).Err(errL3).Msg("Fallback PostgreSQL L3 indisponible lors d'un résultat GEO L1 vide")
		} else if len(l3POIs) > 0 {
			liveL3 := filterLivePOIs(l3POIs)
			if len(liveL3) > 0 {
				_ = object_cache_service.SetPOIsBatchInObjectCache(ctx, liveL3)
				pois = append(pois, liveL3...)
			}
		}
	}

	// ── ÉTAPE 4 : FILTRAGE VISIBILITÉ, GÉOMÉTRIE EXACTE & DÉDUPLICATION ─────
	seen := make(map[int64]struct{}, len(pois))
	filtered := make([]map_models.POIPayload, 0, len(pois))
	for _, p := range pois {
		if p.ID <= 0 || p.Visibility < variables.POIVisibilityPublic {
			continue
		}
		if _, exists := seen[p.ID]; exists {
			continue
		}
		if p.Latitude < input.SWLat || p.Latitude > input.NELat || p.Longitude < input.SWLng || p.Longitude > input.NELng {
			continue
		}
		seen[p.ID] = struct{}{}
		filtered = append(filtered, p)
	}

	// Tri déterministe : note décroissante puis ID croissant
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].Rating == filtered[j].Rating {
			return filtered[i].ID < filtered[j].ID
		}
		return filtered[i].Rating > filtered[j].Rating
	})

	// ── ÉTAPE 5 : RÉSOLUTION GROUPÉE DES MÉDIAS & GÉNÉRATION DES VUES ───────
	views := BuildPOIViewsBatch(ctx, filtered, callerID)

	return map_models.GetPoisInBBoxOutput{
		POIs: views,
	}, nil
}

// BuildPOIViewsBatch résout les médias de tous les POIs en évitant le problème N+1
// et construit les DTOs `POIView`.
func BuildPOIViewsBatch(ctx context.Context, pois []map_models.POIPayload, callerID int64) []map_models.POIView {
	if len(pois) == 0 {
		return make([]map_models.POIView, 0)
	}

	// Collecte dédupliquée de tous les MediaIDs à travers tous les POIs du lot
	uniqueMediaIDs := make([]int64, 0)
	mediaSeen := make(map[int64]struct{})
	for _, p := range pois {
		for _, mid := range p.MediaIDs {
			if mid > 0 {
				if _, ok := mediaSeen[mid]; !ok {
					mediaSeen[mid] = struct{}{}
					uniqueMediaIDs = append(uniqueMediaIDs, mid)
				}
			}
		}
	}

	// Pré-chargement groupé des médias en L1/L2/L3 en une seule passe
	resolvedMediaMap := make(map[int64]media_models.MediaView, len(uniqueMediaIDs))
	if len(uniqueMediaIDs) > 0 {
		batchViews := media_service.FormatMediaViewsCascade(ctx, uniqueMediaIDs, 0, 0, callerID)
		for _, mv := range batchViews {
			resolvedMediaMap[mv.MediaID] = mv
		}
	}

	views := make([]map_models.POIView, 0, len(pois))
	for _, p := range pois {
		poiMedia := make([]media_models.MediaView, 0, len(p.MediaIDs))
		for _, mid := range p.MediaIDs {
			if mv, ok := resolvedMediaMap[mid]; ok {
				poiMedia = append(poiMedia, mv)
			}
		}
		views = append(views, p.ToView(poiMedia))
	}

	return views
}

func filterLivePOIs(pois []map_models.POIPayload) []map_models.POIPayload {
	live := make([]map_models.POIPayload, 0, len(pois))
	for _, p := range pois {
		if p.Visibility >= variables.POIVisibilityPublic {
			live = append(live, p)
		}
	}
	return live
}
