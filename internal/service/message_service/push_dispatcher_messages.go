package message_service

import (
	"context"
	"encoding/json"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// Structure locale privée pour casser la dépendance cyclique vers le package worker
type pushJob struct {
	UserID    int64  `json:"user_id"`
	EventType string `json:"event_type"`
	Payload   any    `json:"payload"`
}

// ############################################################################
// # SERVICE INTERNE : ENTONNOIR DÉCISIONNEL POUR LES NOTIFICATIONS PUSH
// ############################################################################

// shouldSendPush évalue si un utilisateur spécifique doit recevoir une notification Push
// selon une matrice de présence et de confidentialité résolue en O(1) en RAM.
func shouldSendPush(ctx context.Context, conversationID int64, targetUserID int64, isUserMentioned bool, messageType int) bool {

	// ── FILTRE 1 : PRÉSENCE EN LIGNE (O(1)) ─────────────────────────────────
	if cache_service.IsUserOnline(ctx, targetUserID) {
		return false // L'utilisateur a l'application ouverte, les WebSockets prendront le relais.
	}

	// ── FILTRE 2 : PARAMÈTRES GLOBAUX UTILISATEUR L1 (O(1)) ─────────────────
	userSettingsPayload, errSettings := object_cache_service.GetUserSettingsCascade(ctx, targetUserID)
	if errSettings != nil || !userSettingsPayload.Notifications.MasterPushEnabled {
		return false // L'utilisateur a désactivé toutes les notifications.
	}

	// ── FILTRE 3 : PARAMÈTRES LOCAUX DE LA CONVERSATION L1 (O(1)) ───────────
	memberPayload, errMember := object_cache_service.GetMemberFromObjectCache(ctx, conversationID, targetUserID)
	if errMember != nil {
		return false // Impossible de charger le membre, on bloque par sécurité.
	}

	isMutedStatus := memberPayload.Settings.IsMuted

	// Gestion du "Mute Temporaire"
	expirationTimestamp := memberPayload.Settings.MuteExpireAt
	if expirationTimestamp > 0 {
		currentTimestamp := domain.TimeToMillis(domain.NowTime())
		// Auto-détection du format millisecondes vs secondes
		if expirationTimestamp > variables.MaxTimestamp {
			currentTimestamp = domain.NowMillis()
		}
		if expirationTimestamp > currentTimestamp {
			isMutedStatus = variables.SettingsMutedAll // Toujours silencieux
		}
	}

	// Aiguillage métier selon le type de message
	isNotificationEnabledForMentions := userSettingsPayload.Notifications.NotifyMentions
	isNotificationEnabledForBaseAction := userSettingsPayload.Notifications.NotifyMessages

	if messageType == variables.MessageTypeInvite {
		isNotificationEnabledForBaseAction = userSettingsPayload.Notifications.NotifyGroupInvites
	}

	// ── ENTONNOIR FINAL DE DÉCISION ─────────────────────────────────────────
	switch isMutedStatus {
	case variables.SettingsMutedAll: // Sourdine totale (Rien ne passe)
		return false
	case variables.SettingsMutedExceptMention: // Sourdine partielle (Seules les mentions passent)
		return isUserMentioned && isNotificationEnabledForMentions
	case variables.SettingsNoMuted: // Normal (Tout passe si autorisé)
		return (isUserMentioned && isNotificationEnabledForMentions) || (!isUserMentioned && isNotificationEnabledForBaseAction)
	default:
		return false
	}
}

// dispatchPushNotifications itère sur la liste des destinataires et route les tâches vers Firebase Cloud Messaging.
func dispatchPushNotifications(messageView message_models.MessageView, recipientUserIDs []int64, mentionedUserIDs []int64) {

	// Garde-fou : On ne notifie JAMAIS les messages systèmes (Type 8) via le Push OS Messagerie.
	if messageView.MessageType == variables.MessageTypeSystem {
		return
	}

	// L'exécution se fait dans une goroutine pour garantir que la latence HTTP
	// lors de l'appel au `CreateMessage` par l'utilisateur reste sous les 5ms.
	go func() {
		backgroundContext := context.Background()

		for _, recipientID := range recipientUserIDs {
			// On exclut catégoriquement l'expéditeur de la matrice de notifications
			if recipientID == messageView.SenderID {
				continue
			}

			isUserExplicitlyMentioned := pkg.Exists(mentionedUserIDs, recipientID)

			if shouldSendPush(backgroundContext, messageView.ConversationID, recipientID, isUserExplicitlyMentioned, messageView.MessageType) {

				eventType := "message.new"
				if isUserExplicitlyMentioned {
					eventType = "message.mention" // Utile pour personnaliser le son et l'UI dans Flutter/Swift
				}

				pushTask := pushJob{
					UserID:    recipientID,
					EventType: eventType,
					Payload:   messageView,
				}

				// Envoi dans la file FIFO du Worker Firebase via l'abstraction DDD
				if jobBytes, errMarshal := json.Marshal(pushTask); errMarshal == nil {
					_ = redis.WorkerQueue.LPush(backgroundContext, "firebase", jobBytes)
				} else {
					numan_log.Warn(backgroundContext).Err(errMarshal).Int64("user_id", recipientID).Msg("Impossible de sérialiser le Job FCM")
				}
			}
		}
	}()
}
