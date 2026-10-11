package auth_models

// AddCertificationInput valide les données JSON envoyées par l'utilisateur (POST).
// On exige que le prestataire et l'ID de certification soient présents.
type AddCertificationInput struct {
	Prestataire     string `json:"prestataire" binding:"required,min=2,max=50"`
	CertificationID string `json:"certification_id" binding:"required,alphanum,min=3,max=100"`
}

// AddCertificationOutput est la réponse renvoyée au client après l'attribution du badge.
type AddCertificationOutput struct {
	Message string   `json:"message"`
	Badges  []string `json:"badges"` // On renvoie la nouvelle liste de badges pour mettre à jour l'UI locale
}
