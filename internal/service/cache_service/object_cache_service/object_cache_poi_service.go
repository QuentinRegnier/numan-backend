package object_cache_service

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	goredis "github.com/redis/go-redis/v9"
	"github.com/vmihailenco/msgpack/v5"
)

// ############################################################################
// # SERVICE : OBJECT & GEO CACHE (POINTS OF INTEREST - TTL 0)
// ############################################################################

// GlobalPOIGeoIndexKey désigne la clé de partition globale de l'index spatial Redis GEO pour les POIs.
const GlobalPOIGeoIndexKey = "global"

// GetPOIFromObjectCache récupère un POI unique depuis le cache objet Redis L1.
func GetPOIFromObjectCache(ctx context.Context, poiID int64) (map_models.POIPayload, error) {
	var poi map_models.POIPayload
	errRedis := redis.POIs.GetObject(ctx, poiID, &poi)
	if errRedis != nil {
		if errors.Is(errRedis, goredis.Nil) {
			return map_models.POIPayload{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Point d'intérêt introuvable dans le cache.", errRedis)
		}
		return map_models.POIPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de récupérer le point d'intérêt depuis l'Object Cache.",
			errRedis,
		)
	}
	return poi, nil
}

// GetPOIsBatchFromObjectCache récupère un lot de POIs en un seul aller-retour MGET Redis L1.
// Retourne la map des POIs trouvés et la slice des identifiants manquants (cache misses).
func GetPOIsBatchFromObjectCache(ctx context.Context, poiIDs []int64) (map[int64]map_models.POIPayload, []int64, error) {
	found := make(map[int64]map_models.POIPayload, len(poiIDs))
	if len(poiIDs) == 0 {
		return found, nil, nil
	}

	res, err := redis.POIs.GetMany(ctx, poiIDs)
	if err != nil {
		return nil, poiIDs, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de récupérer le lot de points d'intérêt depuis l'Object Cache.",
			err,
		)
	}

	var missingIDs []int64
	for _, id := range poiIDs {
		rawBytes, ok := res.Found[id]
		if !ok || len(rawBytes) == 0 {
			missingIDs = append(missingIDs, id)
			continue
		}
		var poi map_models.POIPayload
		if errUnmarshal := msgpack.Unmarshal(rawBytes, &poi); errUnmarshal != nil {
			missingIDs = append(missingIDs, id)
			continue
		}
		found[id] = poi
	}

	return found, missingIDs, nil
}

// SetPOIInObjectCache indexe un POI dans l'Object Cache L1 (TTL 0) et dans l'index spatial GEO L1.
func SetPOIInObjectCache(ctx context.Context, poi map_models.POIPayload) error {
	if err := redis.POIs.SetObject(ctx, poi.ID, poi); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de sauvegarder le point d'intérêt dans l'Object Cache.",
			err,
		)
	}
	if err := redis.POIsGeo.GeoAdd(ctx, GlobalPOIGeoIndexKey, poi.Longitude, poi.Latitude, poi.ID); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible d'indexer les coordonnées du point d'intérêt dans le cache GEO.",
			err,
		)
	}
	return nil
}

// SetPOIsBatchInObjectCache enregistre un lot de POIs dans l'Object Cache L1 et met à jour l'index spatial GEO L1 en batch.
func SetPOIsBatchInObjectCache(ctx context.Context, pois []map_models.POIPayload) error {
	if len(pois) == 0 {
		return nil
	}

	objMap := make(map[int64]any, len(pois))
	geoItems := make([]redis.GeoLocationItem, 0, len(pois))
	for _, p := range pois {
		objMap[p.ID] = p
		geoItems = append(geoItems, redis.GeoLocationItem{
			MemberID:  p.ID,
			Longitude: p.Longitude,
			Latitude:  p.Latitude,
		})
	}

	if err := redis.POIs.SetMany(ctx, objMap); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de sauvegarder le lot de points d'intérêt dans l'Object Cache.",
			err,
		)
	}

	if err := redis.POIsGeo.GeoAddBatch(ctx, GlobalPOIGeoIndexKey, geoItems); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible d'indexer le lot de points d'intérêt dans le cache GEO.",
			err,
		)
	}

	return nil
}

// DeletePOIsFromObjectCache supprime de manière synchrone un ou plusieurs POIs de l'Object Cache L1 et de l'index spatial GEO L1.
func DeletePOIsFromObjectCache(ctx context.Context, poiIDs []int64) error {
	if len(poiIDs) == 0 {
		return nil
	}

	if err := redis.POIs.DeleteMany(ctx, poiIDs); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de supprimer les points d'intérêt de l'Object Cache.",
			err,
		)
	}

	members := make([]any, 0, len(poiIDs))
	for _, id := range poiIDs {
		members = append(members, id)
	}
	if err := redis.POIsGeo.GeoRem(ctx, GlobalPOIGeoIndexKey, members...); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de retirer les points d'intérêt de l'index GEO.",
			err,
		)
	}

	return nil
}

// SearchPOIIDsInBBox interroge l'index spatial Redis GEO L1 (`GEOSEARCH BYBOX`) pour extraire les identifiants des POIs dans la Bounding Box.
func SearchPOIIDsInBBox(ctx context.Context, swLat, swLng, neLat, neLng float64) ([]int64, error) {
	ids, err := redis.POIsGeo.GeoSearchBBox(ctx, GlobalPOIGeoIndexKey, swLat, swLng, neLat, neLng, variables.MaxPOIGeoSearchCount)
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return []int64{}, nil
		}
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible d'exécuter la recherche spatiale dans le cache GEO.",
			err,
		)
	}
	return ids, nil
}
