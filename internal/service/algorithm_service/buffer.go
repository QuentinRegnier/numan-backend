package algorithm_service

import (
	"context"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

// ############################################################################
// # GESTION DE L'ÉTAT DU FEED (BUFFER)
// ############################################################################

// feedData contient la graine et la liste des IDs ordonnés pour une lettre donnée (A, B ou C).
type feedData struct {
	Seed    int64   `json:"seed" msgpack:"seed"`
	PostIDs []int64 `json:"post_ids" msgpack:"post_ids"` // Liste finale ordonnée par la caissière
	Fused   bool    `json:"fused" msgpack:"fused"`       // Indique si le panier a déjà fusionné avec ses voisins
}

// feedState est la structure interne stockée physiquement dans Redis.
// Elle remplace l'ancienne pagination rigide par un modèle de tampons tournants.
type FeedState struct {
	GeneratedAt time.Time           `json:"generated_at" msgpack:"generated_at"`
	ActiveFeed  string              `json:"active_feed" msgpack:"active_feed"` // Pointeur actuel : "A", "B" ou "C"
	Feeds       map[string]feedData `json:"feeds" msgpack:"feeds"`
}

// GetUserFeedState récupère l'arborescence complète depuis Redis via le wrapper LFU.
func GetUserFeedState(ctx context.Context, userID int64) (FeedState, error) {
	var state FeedState
	err := redis.FeedsObject.GetObject(ctx, userID, &state)
	if err != nil {
		// On sécurise l'erreur : si ça ne vient pas de Redis, c'est que la clé n'existe pas.
		return state, err
	}
	return state, nil
}

// saveUserFeedState écrase ou met à jour l'état complet dans le Speed Cache Redis.
func saveUserFeedState(ctx context.Context, userID int64, state FeedState) error {
	err := redis.FeedsObject.SetObject(ctx, userID, state)
	if err != nil {
		return err
	}
	return nil
}

// DeleteUserFeedState supprime brutalement l'état en RAM.
// Indispensable lors d'un pull-to-refresh destructif (/force) ou d'un blocage d'utilisateur.
func DeleteUserFeedState(ctx context.Context, userID int64) error {
	err := redis.FeedsObject.DeleteObject(ctx, userID)
	if err != nil {
		return err
	}
	return nil
}
