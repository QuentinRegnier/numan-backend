package report_models

import "github.com/QuentinRegnier/numan-backend/internal/variables"

// ReportPayload est le paquet envoyé au Worker pour la BDD et restitué dans la file de modération.
type ReportPayload struct {
	ID                     int64   `json:"id" bson:"id"`
	ReporterID             int64   `json:"reporter_id" bson:"reporter_id"`                           // 0 si généré par le robot IA
	ReportOrigin           int     `json:"report_origin" bson:"report_origin"`                       // 0: User, 1: Auto_AI (Robot), 2: Hybrid
	ReportOriginLabel      string  `json:"report_origin_label" bson:"report_origin_label,omitempty"` // "user" | "auto_ai" | "hybrid"
	TargetType             int     `json:"target_type" bson:"target_type"`                           // 0: Post, 1: Comment, 2: Conv, 3: Message, 5: User
	TargetIDs              []int64 `json:"target_ids" bson:"target_ids"`
	TargetOwnerID          int64   `json:"target_owner_id" bson:"target_owner_id"`
	AuthorTrustScore       float64 `json:"author_trust_score" bson:"author_trust_score"`
	IAScoreMax             float64 `json:"ia_score_max" bson:"ia_score_max"`
	AccumulatedReportScore float64 `json:"accumulated_report_score" bson:"accumulated_report_score"`
	ReportCount            int     `json:"report_count" bson:"report_count"`
	Category               int     `json:"category" bson:"category"`
	Reason                 string  `json:"reason" bson:"reason"`
	Rationale              string  `json:"rationale" bson:"rationale"`
	State                  int     `json:"state" bson:"state"`
	Importance             float64 `json:"importance" bson:"importance"` // Priorité globale P_file
	CreatedAt              int64   `json:"created_at" bson:"created_at"`
	UpdatedAt              int64   `json:"updated_at" bson:"updated_at"`
}

// ResolveOriginLabel retourne le libellé lisible correspondant au code d'origine du signalement.
func ResolveOriginLabel(origin int) string {
	switch origin {
	case variables.ReportOriginAutoAI:
		return variables.ReportOriginLabelAutoAI
	case variables.ReportOriginHybrid:
		return variables.ReportOriginLabelHybrid
	default:
		return variables.ReportOriginLabelUser
	}
}
