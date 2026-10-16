package variables_service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

const reconciliationInterval = 30 * time.Second

// InitDynamicVariables initialise le système de variables dynamiques au démarrage du serveur.
func InitDynamicVariables(ctx context.Context) {
	cache_service.BindVariablesCacheHooks()

	if !cache_service.IsVariablesCacheReady() {
		numan_log.Warn(ctx).Msg("Variables dynamiques : Redis non initialisé, conservation des valeurs par défaut du code.")
		return
	}

	if err := SyncVariablesFromRedis(ctx); err != nil {
		numan_log.Warn(ctx).Err(err).Msg("Variables dynamiques : échec du chargement initial Redis, repli sur les valeurs par défaut")
	} else {
		numan_log.Info(ctx).Int64("version", variables.GetVersion()).Msg("Variables dynamiques : état initial synchronisé depuis Redis")
	}

	go startVariablesPubSubListener(ctx)
	go startVariablesReconciliationLoop(ctx)
}

// SyncVariablesFromRedis charge de manière atomique le Hash d'overrides et la version depuis le Cache Service
// et met à jour toutes les variables en mémoire locale.
func SyncVariablesFromRedis(ctx context.Context) error {
	readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	overrides, remoteVersion, err := cache_service.LoadDynamicVariablesFromCache(readCtx)
	if err != nil {
		return err
	}

	return variables.ApplyRemoteState(overrides, remoteVersion)
}

// EnsureVariablesFreshness vérifie en O(1) si la version Redis a évolué par rapport à la mémoire locale
// et déclenche une synchronisation uniquement si nécessaire.
func EnsureVariablesFreshness(ctx context.Context) {
	if !cache_service.IsVariablesCacheReady() {
		return
	}

	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	remoteVersion, err := cache_service.GetDynamicVariablesRemoteVersion(checkCtx)
	if err != nil {
		numan_log.Warn(ctx).Err(err).Msg("Variables dynamiques : impossible de vérifier la version distante Redis, utilisation du cache mémoire local")
		return
	}

	if remoteVersion != variables.GetVersion() {
		if err := SyncVariablesFromRedis(checkCtx); err != nil {
			numan_log.Warn(ctx).Err(err).Msg("Variables dynamiques : échec de la réconciliation depuis Redis")
		}
	}
}

func startVariablesPubSubListener(ctx context.Context) {
	msgChan, cancel := cache_service.SubscribeDynamicVariablesSync(ctx)
	defer cancel()

	numan_log.Info(ctx).Msg("Variables dynamiques : écoute du flux de synchronisation inter-serveurs activée.")

	for {
		select {
		case <-ctx.Done():
			return
		case payload, ok := <-msgChan:
			if !ok {
				return
			}
			var evt cache_service.VariablesSyncEvent
			if err := json.Unmarshal(payload, &evt); err != nil {
				numan_log.Warn(ctx).Err(err).Msg("Variables dynamiques : événement Pub/Sub mal formé")
				continue
			}

			if evt.Version <= variables.GetVersion() && evt.Version != 0 {
				continue
			}

			if err := SyncVariablesFromRedis(ctx); err != nil {
				numan_log.Error(ctx).Err(err).Int64("target_version", evt.Version).Msg("Variables dynamiques : échec de la synchronisation suite à notification Pub/Sub")
			} else {
				numan_log.Info(ctx).Int64("version", variables.GetVersion()).Msg("Variables dynamiques : configuration locale mise à jour via Pub/Sub")
			}
		}
	}
}

func startVariablesReconciliationLoop(ctx context.Context) {
	ticker := time.NewTicker(reconciliationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			EnsureVariablesFreshness(ctx)
		}
	}
}
