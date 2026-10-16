package cache_service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	goredis "github.com/go-redis/redis/v8"
)

const (
	variablesOverridesKey = "overrides"
	variablesVersionKey   = "version"
)

// VariablesSyncEvent est l'événement léger diffusé sur le flux Pub/Sub inter-serveurs.
type VariablesSyncEvent struct {
	Version   int64 `json:"version"`
	UpdatedAt int64 `json:"updated_at"`
}

// BindVariablesCacheHooks enregistre les callbacks de rafraîchissement du cache Redis.
func BindVariablesCacheHooks() {
	variables.SetStandardTTLHook(redis.RefreshStandardTTL)
}

// IsVariablesCacheReady vérifie si le client Redis sous-jacent est prêt.
func IsVariablesCacheReady() bool {
	return redis.IsReady()
}

// LoadDynamicVariablesFromCache récupère de manière atomique l'état de référence des variables personnalisées et leur version depuis Redis.
func LoadDynamicVariablesFromCache(ctx context.Context) (map[string]string, int64, error) {
	return redis.DynamicVariables.LoadVariablesStateAtomic(ctx, variablesOverridesKey, variablesVersionKey)
}

// SaveDynamicVariablesPatchInCache persiste de manière atomique un lot de variables modifiées dans Redis et retourne la nouvelle version.
func SaveDynamicVariablesPatchInCache(ctx context.Context, toSet map[string]string, toDelete []string) (int64, error) {
	return redis.DynamicVariables.ApplyVariablesPatchAtomic(ctx, variablesOverridesKey, variablesVersionKey, toSet, toDelete)
}

// GetDynamicVariablesRemoteVersion lit en O(1) le numéro de version global des variables dans Redis.
func GetDynamicVariablesRemoteVersion(ctx context.Context) (int64, error) {
	ver, err := redis.DynamicVariables.GetInt64(ctx, variablesVersionKey)
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return 0, nil
		}
		return 0, err
	}
	return ver, nil
}

// PublishDynamicVariablesSync notifie toutes les instances Go d'une mise à jour de configuration via le pattern Claim-Check Flux.
func PublishDynamicVariablesSync(ctx context.Context, event VariablesSyncEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return redis.DynamicVariablesSync.PushFlux(ctx, payload)
}

// SubscribeDynamicVariablesSync ouvre un canal d'écoute sur le flux de synchronisation des variables dynamiques.
func SubscribeDynamicVariablesSync(ctx context.Context) (<-chan []byte, context.CancelFunc) {
	return redis.DynamicVariablesSync.SubscribeFlux(ctx)
}

// ApplyAndBroadcastDynamicVariablesPatch exécute la persistance atomique dans Redis (SaveDynamicVariablesPatchInCache)
// puis diffuse immédiatement l'événement de synchronisation inter-serveurs (PublishDynamicVariablesSync).
func ApplyAndBroadcastDynamicVariablesPatch(ctx context.Context, preparedPatch *variables.PreparedPatch) (int64, error) {
	if !IsVariablesCacheReady() {
		return 0, numan_error.NewInternal(errors.New("connexion Redis indisponible pour la mise à jour des variables"))
	}

	newVersion, errSave := SaveDynamicVariablesPatchInCache(ctx, preparedPatch.ToSet, preparedPatch.ToDelete)
	if errSave != nil {
		return 0, errSave
	}

	syncEvt := VariablesSyncEvent{
		Version:   newVersion,
		UpdatedAt: domain.NowMillis(),
	}
	if errPub := PublishDynamicVariablesSync(ctx, syncEvt); errPub != nil {
		numan_log.Warn(ctx).Err(errPub).Int64("version", newVersion).Msg("Variables dynamiques : échec de la publication Pub/Sub (rattrapage automatique par la boucle de réconciliation)")
	}

	return newVersion, nil
}
