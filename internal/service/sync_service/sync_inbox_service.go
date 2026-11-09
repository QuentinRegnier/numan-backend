package sync_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/sync_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/conversation_service"
)

// ############################################################################
// # SERVICE : SYNCHRONISATION LÉGÈRE DE L'INBOX (DELTA SYNC)
// ############################################################################

// SyncInbox vérifie le delta temporel en RAM (O(1)) et ne retourne l'Inbox hydratée
// que si le serveur possède des données plus récentes que le client.
func SyncInbox(ctx context.Context, callerID int64, input sync_models.SyncInboxInput) (sync_models.SyncInboxOutput, error) {

	syncOutput := sync_models.SyncInboxOutput{
		NeedUpdate: false,
	}

	// ── ÉTAPE 1 : LECTURE DU MARQUEUR D'ACTIVITÉ L1 (O(1)) ──────────────────

	serverLatestActivityTimestamp, errRedis := redis.InboxActivity.GetInt64(ctx, callerID)

	if errRedis != nil || serverLatestActivityTimestamp == 0 {
		// Cas d'un cold start complet ou d'une purge volatile Redis :
		// On force la génération d'un timestamp actuel valide.
		serverLatestActivityTimestamp = domain.NowMillis()
		_ = redis.InboxActivity.SetPrimitive(ctx, callerID, serverLatestActivityTimestamp)
	}

	// ── ÉTAPE 2 : ÉVALUATION DU DELTA (RÉSOLUTION DE CONFLIT) ───────────────

	if input.ClientUpdatedAt >= serverLatestActivityTimestamp {
		// LE CLIENT EST À JOUR ! Zéro I/O supplémentaire BDD/Cache nécessaire.
		syncOutput.ServerUpdated = serverLatestActivityTimestamp
		return syncOutput, nil
	}

	// ── ÉTAPE 3 : HYDRATATION EN CAS DE DÉSYNCHRONISATION ───────────────────

	syncOutput.NeedUpdate = true
	syncOutput.ServerUpdated = serverLatestActivityTimestamp

	// Requête par défaut pour la synchronisation à froid : les 50 premières conversations
	inboxRequestInput := conversation_models.GetUserConversationsInput{
		Limit:  50,
		Offset: 0,
		Force:  false,
	}

	inboxPaginatedData, errInbox := conversation_service.GetUserConversationsPaginated(ctx, callerID, inboxRequestInput)
	if errInbox != nil {
		numan_log.Error(ctx).Err(errInbox).Int64("user_id", callerID).Msg("Erreur critique lors de la synchronisation de l'inbox.")
		return syncOutput, numan_error.NewInternal() // Protection des détails d'infrastructure
	}

	syncOutput.Conversations = inboxPaginatedData.Conversations
	return syncOutput, nil
}
