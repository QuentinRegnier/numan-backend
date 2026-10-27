package moderation_models

// ModeratorStatsRecord stocke l'état de gouvernance d'un modérateur ou d'un utilisateur ciblé en cache L1.
type ModeratorStatsRecord struct {
	UserID              int64         `msgpack:"user_id" json:"user_id"`
	ModeratorTrustScore float64       `msgpack:"moderator_trust_score" json:"moderator_trust_score"`
	Grade               int           `msgpack:"grade" json:"grade"`
	BlameCount          int           `msgpack:"blame_count" json:"blame_count"`
	BlameHistory        []BlameRecord `msgpack:"blame_history" json:"blame_history"`
	UpdatedAt           int64         `msgpack:"updated_at" json:"updated_at"`
}
