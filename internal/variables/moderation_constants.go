package variables

// Constantes des clés de sous-files ZSET dans redis.ModerationQueues
const (
	ModerationQueueActive     = "active"
	ModerationQueuePeerReview = "peer_review"
	ModerationQueueFinalVote  = "final_vote"
	ModerationQueueAdmin      = "admin"

	ModerationExpirationLocks       = "locks"
	ModerationExpirationWaitingInfo = "waiting_info"
)
