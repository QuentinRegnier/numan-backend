package worker

import (
	"context"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/lib/pq"
)

// ############################################################################
// # WORKER : HASHTAG CANON (RÉSOLUTION DES FAUTES DE FRAPPE & ALIASING)
// ############################################################################

// startHashtagCanonCron lance un worker qui calcule les similarités (Levenshtein)
// entre les tags communautaires toutes les 24h pour absorber les fautes de frappe.
func startHashtagCanonCron(ctx context.Context) {
	numan_log.Info(ctx).Msg("Démarrage du Canoniseur de Hashtags (Cron 24h)...")

	go func() {
		// En production, utiliser un vrai cron pour viser les heures creuses (ex: 03:00 AM)
		ticker := time.NewTicker(variables.HashtagCanonCronInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				processHashtagCanonicalization(ctx)
			}
		}
	}()
}

// processHashtagCanonicalization est le cœur de l'algorithme de nettoyage.
func processHashtagCanonicalization(ctx context.Context) {
	// ── ÉTAPE 1 : RÉCUPÉRATION DU RÉSERVOIR DE TAGS COMMUNAUTAIRES (L1) ─────
	tags, err := redis.Tags.SMembers(ctx, "active")
	if err != nil || len(tags) == 0 {
		return // Aucun nouveau tag à analyser
	}

	// ── ÉTAPE 2 : PERSISTANCE SQL DÉFINITIVE (L3) ───────────────────────────
	// Immortalisation des tags dans la base de données relationnelle
	persistCommunityTags(ctx, tags)

	// S'il n'y a pas assez de tags pour faire des paires de comparaison, on s'arrête
	if len(tags) < 2 {
		return
	}

	numan_log.Info(ctx).Int("count", len(tags)).Msg("Canonicalisation de tags communautaires en cours...")
	aliasMap := make(map[string]string)

	// ── ÉTAPE 3 : ALGORITHME D'APPARIEMENT O(N²) ────────────────────────────
	// Comparaison par paires. (Gérable jusqu'à ~100k tags car exécuté 1 seule fois par nuit).
	for i := 0; i < len(tags); i++ {
		for j := i + 1; j < len(tags); j++ {
			t1 := tags[i]
			t2 := tags[j]

			// Protection contre les faux positifs sur des mots trop courts
			if len(t1) < variables.HashtagCanonMinLength || len(t2) < variables.HashtagCanonMinLength {
				continue
			}

			// Calcul de la distance d'édition (Levenshtein) normalisée
			distNorm := normalizedLevenshtein(t1, t2)

			// Critère strict : Distance <= 15% ET même racine morphologique (Stemming)
			if distNorm <= variables.HashtagCanonMaxDistance && service.StemHashtag(t1) == service.StemHashtag(t2) {
				canon, typo := t1, t2

				// Règle empirique : Le mot le plus long est souvent le plus correct
				if len(t2) < len(t1) {
					canon, typo = t2, t1
				}
				aliasMap[typo] = canon
			}
		}
	}

	// ── ÉTAPE 4 : MISE À JOUR DU DICTIONNAIRE D'ALIAS (PIPELINE L1) ─────────
	if len(aliasMap) > 0 {
		pipe := redis.HashtagCanon.Pipeline()
		for typo, canon := range aliasMap {
			pipe.HSet(ctx, redis.HashtagCanon.Key("map"), typo, canon)
		}

		_, errExec := pipe.Exec(ctx)
		if errExec == nil {
			numan_log.Info(ctx).Int("alias_count", len(aliasMap)).Msg("Canonicalisation terminée : Dictionnaire de fautes de frappes mis à jour.")
		} else {
			numan_log.Error(ctx).Err(errExec).Msg("Échec critique lors de l'enregistrement des alias dans Redis (L1)")
		}
	}
}

// ============================================================================
// MOTEUR MATHÉMATIQUE DE DISTANCE D'ÉDITION
// ============================================================================

// normalizedLevenshtein calcule la distance de Levenshtein normalisée.
// TDD §3.3 — Formule: d_Lev(h_i, h_j) = Lev(h_i, h_j) / max(|h_i|, |h_j|)
// Retourne une valeur dans [0.0, 1.0]: 0.0 = chaînes identiques, 1.0 = totalement différentes.
func normalizedLevenshtein(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)

	maxLen := la
	if lb > maxLen {
		maxLen = lb
	}

	if maxLen == 0 {
		return 0.0
	}

	return float64(levenshteinRunes(ra, rb)) / float64(maxLen)
}

// levenshteinRunes calcule la distance d'édition de Levenshtein entre deux slices de runes.
// Implémentation ultra-optimisée en espace O(min(|a|,|b|)) via deux rangées glissantes.
func levenshteinRunes(a, b []rune) int {
	la, lb := len(a), len(b)

	// Optimisation: garantir |a| ≤ |b| pour minimiser l'allocation mémoire.
	if la > lb {
		a, b = b, a
		la, lb = lb, la
	}

	// Deux rangées de la matrice DP (espace O(la) au lieu de O(la·lb)).
	prev := make([]int, la+1)
	curr := make([]int, la+1)

	// Initialisation: prev[j] = j (coût de suppression des j premiers chars de a).
	for j := 0; j <= la; j++ {
		prev[j] = j
	}

	for i := 1; i <= lb; i++ {
		curr[0] = i
		for j := 1; j <= la; j++ {
			cost := 1
			if b[i-1] == a[j-1] {
				cost = 0
			}
			// min(insertion, suppression, substitution)
			curr[j] = min3Int(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}

	return prev[la]
}

func min3Int(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// persistCommunityTags sauvegarde les tags communautaires dans PostgreSQL.
// Utilise la fonction SQL content.func_upsert_community_tags pour l'insertion de masse idempotente.
func persistCommunityTags(ctx context.Context, tags []string) {
	if len(tags) == 0 {
		return
	}

	query := "SELECT content.func_upsert_community_tags($1)"

	var inserted int
	err := postgres.PostgresDB.QueryRowContext(ctx, query, pq.Array(tags)).Scan(&inserted)
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("Échec lors de la persistance SQL des tags communautaires (L3)")
	} else {
		numan_log.Info(ctx).Int("count", len(tags)).Msg("Persistance SQL des tags communautaires terminée avec succès.")
	}
}
