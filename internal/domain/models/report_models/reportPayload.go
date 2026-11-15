package report_models

import (
	"math"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

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

// IsValidReportTargetType indique si targetType fait partie des cibles supportées.
func IsValidReportTargetType(targetType int) bool {
	switch targetType {
	case variables.ReportTargetPost,
		variables.ReportTargetComment,
		variables.ReportTargetConversation,
		variables.ReportTargetMessage,
		variables.ReportTargetUser:
		return true
	default:
		return false
	}
}

// IsValidReportCategory indique si category fait partie des catégories de signalement connues.
func IsValidReportCategory(category int) bool {
	switch category {
	case variables.ReportCatSpam,
		variables.ReportCatHarassmentMoral,
		variables.ReportCatHarassmentSexual,
		variables.ReportCatHateSpeech,
		variables.ReportCatIdentityTheft,
		variables.ReportCatUnderage,
		variables.ReportCatNonConsensual,
		variables.ReportCatIllegalContent,
		variables.ReportCatSelfHarm,
		variables.ReportCatAIPornography,
		variables.ReportCatOther:
		return true
	default:
		return false
	}
}

// IsValidReportOrigin indique si origin correspond à une origine de signalement reconnue.
func IsValidReportOrigin(origin int) bool {
	switch origin {
	case variables.ReportOriginUser,
		variables.ReportOriginAutoAI,
		variables.ReportOriginHybrid,
		variables.ReportOriginSystem:
		return true
	default:
		return false
	}
}

// IsValidReportState indique si state correspond à un état de cycle de vie de signalement reconnu.
func IsValidReportState(state int) bool {
	switch state {
	case variables.ReportStatePending,
		variables.ReportStateInProgress,
		variables.ReportStateWaitingForInfo,
		variables.ReportStatePendingPeerReview,
		variables.ReportStateEscalated,
		variables.ReportStatePendingFinalVote,
		variables.ReportStateClosed:
		return true
	default:
		return false
	}
}

// Validate vérifie les invariants du payload de signalement avant mise en cache ou écriture asynchrone.
func (p *ReportPayload) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de signalement est nul.", nil)
	}
	if p.ID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du signalement doit être strictement positif.", nil)
	}
	if !IsValidReportOrigin(p.ReportOrigin) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Origine de signalement invalide.", nil)
	}
	if p.ReporterID < 0 || (p.ReportOrigin == variables.ReportOriginUser && p.ReporterID <= 0) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Identifiant du signaleur invalide.", nil)
	}
	if !IsValidReportTargetType(p.TargetType) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Type de cible du signalement invalide.", nil)
	}
	if len(p.TargetIDs) == 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le signalement doit contenir au moins un identifiant cible.", nil)
	}
	for _, id := range p.TargetIDs {
		if id <= 0 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Chaque identifiant cible doit être strictement positif.", nil)
		}
	}
	if p.TargetOwnerID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du propriétaire cible ne peut pas être négatif.", nil)
	}
	if !IsValidReportCategory(p.Category) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Catégorie de signalement invalide.", nil)
	}
	if !IsValidReportState(p.State) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "État du signalement invalide.", nil)
	}
	if p.ReportCount < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le compteur de signalements ne peut pas être négatif.", nil)
	}
	if math.IsNaN(p.AuthorTrustScore) || math.IsInf(p.AuthorTrustScore, 0) || p.AuthorTrustScore < 0 || p.AuthorTrustScore > 1.0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le TrustScore de l'auteur doit être compris entre 0 et 1.", nil)
	}
	if math.IsNaN(p.IAScoreMax) || math.IsInf(p.IAScoreMax, 0) || p.IAScoreMax < 0 || p.IAScoreMax > 1.0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le score IA maximal doit être compris entre 0 et 1.", nil)
	}
	if math.IsNaN(p.AccumulatedReportScore) || math.IsInf(p.AccumulatedReportScore, 0) || p.AccumulatedReportScore < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le score cumulé des signalements est invalide.", nil)
	}
	if math.IsNaN(p.Importance) || math.IsInf(p.Importance, -1) || p.Importance < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La priorité d'importance du signalement est invalide.", nil)
	}
	if utf8.RuneCountInString(p.Reason) > 1000 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le motif du signalement ne peut pas dépasser 1000 caractères.", nil)
	}
	if utf8.RuneCountInString(p.Rationale) > 2000 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La justification modérateur ne peut pas dépasser 2000 caractères.", nil)
	}
	if p.CreatedAt < 0 || p.UpdatedAt < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les horodatages du signalement ne peuvent pas être négatifs.", nil)
	}
	return nil
}

// ResolveOriginLabel retourne le libellé lisible correspondant au code d'origine du signalement.
func ResolveOriginLabel(origin int) string {
	switch origin {
	case variables.ReportOriginAutoAI:
		return variables.ReportOriginLabelAutoAI
	case variables.ReportOriginHybrid:
		return variables.ReportOriginLabelHybrid
	case variables.ReportOriginSystem:
		return variables.ReportOriginLabelSystem
	default:
		return variables.ReportOriginLabelUser
	}
}
