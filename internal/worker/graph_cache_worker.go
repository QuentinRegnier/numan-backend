package worker

import (
	"context"
	"encoding/json"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # WORKER : GRAPH CACHE (ÉMERGENCE COLLECTIVE & SÉMANTIQUE)
// ############################################################################

// handleGraphUpdate intercepte les événements asynchrones de création de posts
// pour tisser le graphe relationnel de tags (Co-occurrences) basé sur la chaîne de Markov.
func handleGraphUpdate(ctx context.Context, events []redis.AsyncEvent) {
	for _, evt := range events {

		// ── ÉTAPE 1 : FILTRAGE DES ÉVÉNEMENTS (UNIQUEMENT LES CRÉATIONS) ────────
		if evt.Type == redis.EntityPost && evt.Action == redis.ActionCreate {

			// Sérialisation inverse du payload générique vers le modèle Post
			jsonBytes, errMarshal := json.Marshal(evt.Payload)
			if errMarshal != nil {
				numan_log.Warn(ctx).Err(errMarshal).Msg("Graph Worker : Impossible de sérialiser le payload du post")
				continue
			}

			var post post_models.PostPayload
			if errUnmarshal := json.Unmarshal(jsonBytes, &post); errUnmarshal != nil {
				numan_log.Warn(ctx).Err(errUnmarshal).Msg("Graph Worker : Impossible de désérialiser le payload du post")
				continue
			}
			if post.Visibility < 0 || post.Visibility > 2 {
				continue // Ignorer les posts en quarantaine (Shadowban) ou supprimés
			}

			// ── ÉTAPE 2 : FUSION DES TAGS DIRECTS ET INDIRECTS ──────────────────
			// On combine les hashtags écrits par l'auteur et ceux ajoutés par la communauté
			// dans les commentaires pour créer un contexte sémantique riche.
			allTags := append(post.Hashtags, post.IndirectHashtags...)

			// Il faut au moins 2 tags pour créer un segment (une arête) dans le graphe
			if len(allTags) > 1 {

				// ── ÉTAPE 3 : FILTRAGE SÉMANTIQUE DU BRUIT ──────────────────────
				semanticTags := filterSemanticTags(allTags)

				// ── ÉTAPE 4 : MISE À JOUR DU GRAPHE DE CO-OCCURRENCES (L1) ──────
				if len(semanticTags) > 1 {
					cache_service.UpdateTagCooccurrences(ctx, semanticTags, post.CreatedAt)
				}
			}
		}
	}
}

// filterSemanticTags nettoie la liste des tags pour ne conserver que la matière
// sémantique utile au graphe de Markov. Elle exclut notamment les auto-tags d'utilisateurs.
func filterSemanticTags(tags []string) []string {
	semantic := make([]string, 0, len(tags))

	for _, t := range tags {
		// Exclusion des tags trop courts ou des tags d'identification d'utilisateurs
		if len(t) > variables.GraphTagMinLength && t[:len(variables.GraphUserTagPrefix)] == variables.GraphUserTagPrefix {
			continue
		}
		semantic = append(semantic, t)
	}

	return semantic
}
