package variables

import "time"

// ############################################################################
// # CONSTANTES PROTOCOLAIRES & FILES REDIS DU DOMAINE NOTIFICATIONS (IMMUABLES)
// ############################################################################

const (
	// Types d'événements de notification supportés par le système Push FCM et les WebSockets
	EventPostLiked        = "post_liked"
	EventCommentLiked     = "comment_liked"
	EventCommentAdded     = "comment_added"
	EventRelationFollowed = "relation_followed"
	EventFriendshipEst    = "friendship_established"
	EventGroupInvited     = "group_invited"

	// Noms des files d'attente asynchrones Redis pour les Push Firebase et la Réputation
	WorkerQueueFirebase   = "firebase"
	PushWorkerQueueName   = "worker:queue:firebase"
	WorkerQueueTrustScore = "trust_score"
)

// ############################################################################
// # PARAMÈTRES DYNAMIQUES DU WORKER DE PUSH NOTIFICATIONS
// ############################################################################

var (
	MaxZsetNotification      = 100
	PushWorkerBLPopTimeout   = 2 * time.Second
	PushWorkerDefaultTitle   = "numan"
	PushWorkerDefaultBody    = "Nouvelle notification"
	PushWorkerDefaultSound   = "default"
	PushWorkerDefaultChannel = "default_channel"
)
