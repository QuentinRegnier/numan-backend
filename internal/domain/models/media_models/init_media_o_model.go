package media_models

type UploadMediaOutput struct {
	MediaID            int64   `json:"media_id"`
	Status             int     `json:"status"`              // 0 = pending standard, 2 = quarantaine (vérification requise)
	IAScore            float64 `json:"ia_score"`            // Score IA NSFWJS enregistré [0.0, 1.0]
	RequiresModeration bool    `json:"requires_moderation"` // true si le TrustScore de l'utilisateur est < 0.40
	Message            string  `json:"message,omitempty"`   // Message informatif pour l'utilisateur en quarantaine
}
