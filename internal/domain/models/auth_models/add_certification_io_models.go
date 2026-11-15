package auth_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// AddCertificationInput valide les données JSON envoyées par l'utilisateur (POST).
// On exige que le prestataire et l'ID de certification soient présents.
type AddCertificationInput struct {
	Prestataire     string `json:"prestataire" binding:"required,min=2,max=50"`
	CertificationID string `json:"certification_id" binding:"required,min=3,max=100"`
}

// Validate vérifie les longueurs et le format sûr des identifiants de certification.
func (p *AddCertificationInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload est nul.", nil)
	}
	trimmedProvider := strings.TrimSpace(p.Prestataire)
	if trimmedProvider == "" || utf8.RuneCountInString(trimmedProvider) < 2 || utf8.RuneCountInString(trimmedProvider) > 50 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nom du prestataire doit contenir entre 2 et 50 caractères.", nil)
	}
	trimmedCert := strings.TrimSpace(p.CertificationID)
	if len(trimmedCert) < 3 || len(trimmedCert) > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de certification doit contenir entre 3 et 100 caractères.", nil)
	}
	for i := 0; i < len(trimmedCert); i++ {
		c := trimmedCert[i]
		isValid := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '-' || c == '_'
		if !isValid {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de certification contient des caractères non autorisés.", nil)
		}
	}
	return nil
}

// AddCertificationOutput est la réponse renvoyée au client après l'attribution du badge.
type AddCertificationOutput struct {
	Message string     `json:"message"`
	Badges  UserBadges `json:"badges"` // On renvoie la nouvelle map de badges pour mettre à jour l'UI locale
}
