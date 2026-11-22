package community_recommendation_models

import (
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models"
)

// CommunityScoreInput contient les métriques brutes d'une communauté publique
// nécessaires au calcul de son score global S_global.
// HasOwnerTrust indique si OwnerTrust est renseigné (évite tout pointeur *float64).
type CommunityScoreInput struct {
	ConversationID  int64
	MemberCount     int
	MsgCount7d      int64
	UniqueAuthors7d int64
	Growth7d        int64
	HasOwnerTrust   bool
	OwnerTrust      float64
	AdminTrusts     []float64
	MemberTrusts    []float64
	Mode            models.CommunityMode
	IsPartner       bool
	CreatedAt       time.Time
	Now             time.Time
}
