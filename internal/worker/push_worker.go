package worker

import (
	"context"
	"encoding/json"
	"fmt"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

var fcmClient *messaging.Client

// pushJob représente la structure d'une tâche de notification en attente dans Redis.
type pushJob struct {
	UserID    int64  `json:"user_id"`
	EventType string `json:"event_type"`
	Payload   any    `json:"payload"`
}

// ############################################################################
// # WORKER : FIREBASE CLOUD MESSAGING (PUSH NOTIFICATIONS)
// ############################################################################

// startPushNotificationWorker initialise la connexion à l'API Google Firebase
// et lance la boucle d'écoute sur la file Redis (Consumer).
func startPushNotificationWorker(ctx context.Context) {
	numan_log.Info(ctx).Msg("Démarrage du Worker Firebase Cloud Messaging...")

	// Initialisation de l'application Firebase
	app, err := firebase.NewApp(ctx, nil)
	if err != nil {
		numan_log.Fatal(ctx).Err(err).Msg("Erreur critique : Impossible d'initialiser Firebase")
		return
	}

	// Création du client Messaging
	fcmClient, err = app.Messaging(ctx)
	if err != nil {
		numan_log.Fatal(ctx).Err(err).Msg("Erreur critique : Impossible d'initialiser le client FCM")
		return
	}

	// Lancement de la Goroutine de consommation (Blocking Pop)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return // Arrêt gracieux
			default:
				// BLPop bloque l'exécution jusqu'à ce qu'un élément soit disponible ou que le timeout expire
				rawJob, errPop := repoRedis.WorkerQueue.BLPop(ctx, variables.WorkerQueueFirebase, variables.PushWorkerBLPopTimeout)
				if errPop != nil {
					continue // File vide ou timeout atteint, on reboucle
				}

				var job pushJob
				if errUnmarshal := json.Unmarshal([]byte(rawJob), &job); errUnmarshal == nil {
					processFirebaseJob(ctx, job)
				} else {
					numan_log.Warn(ctx).Err(errUnmarshal).Msg("Push Worker : Impossible de désérialiser le job FCM")
				}
			}
		}
	}()
}

// processFirebaseJob formate et expédie la notification aux serveurs d'Apple/Google.
func processFirebaseJob(ctx context.Context, job pushJob) {
	// ── ÉTAPE 1 : RÉCUPÉRATION DES IDENTIFIANTS D'APPAREILS (FIDS) ──────────
	// Cascade L1 -> L2 -> L3 pour récupérer tous les appareils actifs de l'utilisateur.
	fids, errCache := cache_service.GetFirebaseInstallationIDsCascade(ctx, job.UserID)
	if errCache != nil || len(fids) == 0 {
		return // L'utilisateur n'a aucun appareil connecté, annulation silencieuse.
	}

	// ── ÉTAPE 2 : FORMATAGE DU PAYLOAD ET DES MÉTADONNÉES ───────────────────
	payloadBytes, _ := json.Marshal(job.Payload)
	payloadStr := string(payloadBytes)

	title := variables.PushWorkerDefaultTitle
	body := variables.PushWorkerDefaultBody
	soundIOS := variables.PushWorkerDefaultSound
	channelAndroid := variables.PushWorkerDefaultChannel

	var payloadMap map[string]interface{}
	_ = json.Unmarshal(payloadBytes, &payloadMap)

	// Aiguillage selon le type d'événement pour personnaliser l'affichage OS
	switch job.EventType {
	case "message.new":
		if sender, ok := payloadMap["sender_username"].(string); ok && sender != "" {
			title = fmt.Sprintf("Nouveau message de %s", sender)
		} else {
			title = "Nouveau message"
		}
		if content, ok := payloadMap["content"].(string); ok && content != "" {
			body = content
		}

	case "message.mention":
		if sender, ok := payloadMap["sender_username"].(string); ok && sender != "" {
			title = fmt.Sprintf("@%s vous a mentionné(e)", sender)
		} else {
			title = "Vous avez été mentionné(e)"
		}
		if content, ok := payloadMap["content"].(string); ok && content != "" {
			body = content
		}
		// Personnalisation des alertes OS pour les mentions
		soundIOS = "mention.wav"
		channelAndroid = "mentions_channel"
	}

	// ── ÉTAPE 3 : CONSTRUCTION DU MESSAGE MULTICAST (FCM BATCH) ─────────────
	message := &messaging.MulticastMessage{
		Fids: fids, // Ciblage de multiples appareils en une seule requête réseau
		Data: map[string]string{
			"event_type": job.EventType,
			"payload":    payloadStr, // Data silencieuse traitée par l'application
		},
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
		Android: &messaging.AndroidConfig{
			CollapseKey: job.EventType,
			Priority:    "high",
			Notification: &messaging.AndroidNotification{
				ChannelID: channelAndroid,
			},
		},
		APNS: &messaging.APNSConfig{
			Headers: map[string]string{
				"apns-collapse-id": job.EventType,
			},
			Payload: &messaging.APNSPayload{
				Aps: &messaging.Aps{
					MutableContent: true, // Autorise l'extension iOS à modifier la notif
					Sound:          soundIOS,
				},
			},
		},
		Webpush: &messaging.WebpushConfig{
			Headers: map[string]string{
				"Topic": job.EventType,
			},
		},
	}

	// ── ÉTAPE 4 : EXPÉDITION PHYSIQUE VIA L'API FIREBASE ────────────────────
	br, errSend := fcmClient.SendEachForMulticast(ctx, message)
	if errSend != nil {
		numan_log.Error(ctx).Err(errSend).Int64("user_id", job.UserID).Msg("Échec critique lors de l'envoi Firebase")
	} else if br.FailureCount > 0 {
		numan_log.Warn(ctx).
			Int("failures", br.FailureCount).
			Int("total", len(fids)).
			Int64("user_id", job.UserID).
			Msg("Firebase a rejeté un ou plusieurs tokens (Possiblement expirés)")
	}
}
