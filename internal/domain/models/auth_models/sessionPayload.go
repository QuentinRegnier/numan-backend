package auth_models

import (
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type SessionsPayload struct { // CreateSession
	ID                     int64          `bson:"id" json:"id"`
	UserID                 int64          `bson:"user_id" json:"user_id"`
	MasterToken            string         `bson:"master_token" json:"master_token"`
	FirebaseInstallationID string         `bson:"firebase_installation_id" json:"firebase_installation_id"`
	DeviceInfo             map[string]any `bson:"device_info" json:"device_info"`
	IPHistory              []string       `bson:"ip_history" json:"ip_history"`
	CurrentSecret          string         `bson:"current_secret" json:"current_secret"`
	LastSecret             string         `bson:"last_secret" json:"last_secret"`
	LastJWT                string         `bson:"last_jwt" json:"last_jwt"`
	ToleranceTime          int64          `bson:"tolerance_time" json:"tolerance_time"`
	CreatedAt              int64          `bson:"created_at" json:"created_at"`
	ExpiresAt              int64          `bson:"expires_at" json:"expires_at"`
}

// Validate vérifie les invariants métier d'une session utilisateur avant stockage.
func (p *SessionsPayload) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de session est nul.", nil)
	}
	if p.ID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de session doit être strictement positif.", nil)
	}
	if p.UserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur de session doit être strictement positif.", nil)
	}
	if strings.TrimSpace(p.MasterToken) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le MasterToken de session est obligatoire.", nil)
	}
	if strings.TrimSpace(p.FirebaseInstallationID) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant d'installation Firebase de session est obligatoire.", nil)
	}
	if p.ExpiresAt <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La date d'expiration de session est invalide.", nil)
	}
	return nil
}
