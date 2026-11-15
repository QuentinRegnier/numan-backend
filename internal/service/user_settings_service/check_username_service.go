package user_settings_service

import (
	"context"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
)

// ############################################################################
// # SERVICE : VÉRIFICATION DE DISPONIBILITÉ DU NOM D'UTILISATEUR
// ############################################################################

// CheckUsernameAvailability vérifie si un nom d'utilisateur est disponible.
// Il délègue la vérification à la cascade haute performance (Cuckoo -> L1 -> L2 -> L3).
func CheckUsernameAvailability(ctx context.Context, username string) bool {
	cleanUsername := strings.TrimSpace(username)
	checkInput := user_settings_models.CheckUsernameInput{Username: cleanUsername}
	if err := checkInput.Validate(); err != nil {
		return false
	}

	// service.IsUnique renvoie 1 si unique (libre), et 0 si trouvé (pris)
	return service.IsUnique(ctx, redis.EntityUser, "username", cleanUsername) == 1
}
