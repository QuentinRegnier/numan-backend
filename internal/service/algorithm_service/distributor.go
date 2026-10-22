package algorithm_service

import (
	"context"
	"math/rand"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// RefreshOptions encapsule les données de contexte envoyées par le client mobile/web.
type RefreshOptions struct {
	UserID        int64
	LastSeenIndex int
	FetchCount    int // La quantité de posts que le client demande (ex: 50 par page)
	Quotas        Quotas
	PersonalOpts  PersonalizedFeedOptions
	IsForce       bool // True si c'est un Pull-To-Refresh manuel
}

// ############################################################################
// # LE DISTRIBUTEUR (Gestion des Tampons Tournants et du Scroll)
// ############################################################################

// HandlePullToRefresh gère la consommation du flux, la rotation des paniers et l'amnésie algorithmique.
func HandlePullToRefresh(ctx context.Context, opts RefreshOptions) ([]int64, error) {

	// 1. Chargement ou Initialisation de l'état en RAM
	feedState, err := GetUserFeedState(ctx, opts.UserID)
	if err != nil {
		// Nouvel utilisateur ou Cache expiré : Création d'un état vierge
		feedState = FeedState{
			ActiveFeed: "A",
			Feeds: map[string]feedData{
				"A": {Seed: rand.Int63()},
				"B": {Seed: rand.Int63()},
				"C": {Seed: rand.Int63()},
			},
		}
	}

	// ─────────────────────────────────────────────────────────────────────────────
	// CAS A : GESTION EXPLICITE DU PULL-TO-REFRESH (/force)
	// ─────────────────────────────────────────────────────────────────────────────
	if opts.IsForce {

		// 1. Sauvegarde systématique dans l'historique des posts réellement consommés avant le reset
		activeFeedData := feedState.Feeds[feedState.ActiveFeed]
		if opts.LastSeenIndex > 0 && len(activeFeedData.PostIDs) > 0 {

			endIndexForSeen := opts.LastSeenIndex
			if endIndexForSeen > len(activeFeedData.PostIDs) {
				endIndexForSeen = len(activeFeedData.PostIDs)
			}

			consumedPostIDs := activeFeedData.PostIDs[0:endIndexForSeen]
			for _, postID := range consumedPostIDs {
				service.MarkAsSeen(ctx, opts.UserID, postID)
			}
		}

		// 2. Réinitialisation complète du Cuckoo Filter (Amnésie algorithmique RAM)
		service.ResetCuckooFilter(ctx, opts.UserID)
		opts.LastSeenIndex = 0

		// 3. Application des délais anti-spam algorithmiques
		if time.Since(feedState.GeneratedAt) >= variables.FeedReloadDelay {

			// Le délai est écoulé : Reset intégral et régénération complète
			feedState.GeneratedAt = time.Now()
			feedState.ActiveFeed = "A"
			feedState.Feeds["A"] = feedData{Seed: rand.Int63(), PostIDs: nil, Fused: false}
			feedState.Feeds["B"] = feedData{Seed: rand.Int63(), PostIDs: nil, Fused: false}
			feedState.Feeds["C"] = feedData{Seed: rand.Int63(), PostIDs: nil, Fused: false}

			seedsArray := [3]int64{feedState.Feeds["A"].Seed, feedState.Feeds["B"].Seed, feedState.Feeds["C"].Seed}
			magasinierBaskets, err := collectCandidates(ctx, opts.UserID, seedsArray, opts.Quotas)
			if err != nil {
				numan_log.Error(ctx).Err(err).Msg("distributor: Échec de la collecte des candidats")
				return nil, err
			}

			// Extraction du panier A
			var initialCandidateIDs []int64
			for _, item := range magasinierBaskets.FeedA.Items {
				initialCandidateIDs = append(initialCandidateIDs, item.PostID)
			}

			opts.PersonalOpts.CandidateIDs = initialCandidateIDs
			opts.PersonalOpts.Seed = feedState.Feeds["A"].Seed
			opts.PersonalOpts.StartIndex = 0

			freshFeedIDs, err := buildPersonalizedFeed(ctx, opts.PersonalOpts)
			if err != nil {
				return nil, err
			}

			// Injection dans l'état
			updatedFeedData := feedState.Feeds["A"]
			updatedFeedData.PostIDs = freshFeedIDs
			feedState.Feeds["A"] = updatedFeedData

		} else {
			// Protection Anti-Spam : On effectue une Rotation Circulaire des paniers existants
			switch feedState.ActiveFeed {
			case "A":
				feedState.ActiveFeed = "B"
			case "B":
				feedState.ActiveFeed = "C"
			case "C":
				feedState.ActiveFeed = "A"
			}
		}

		err = saveUserFeedState(ctx, opts.UserID, feedState)
		if err != nil {
			return nil, err
		}
		opts.LastSeenIndex = 0 // On force à 0, et on laisse couler vers le bloc de Consommation Normal
	}

	// ─────────────────────────────────────────────────────────────────────────────
	// CAS B : CONSOMMATION DU SCROLL (last_seen_index > 0)
	// ─────────────────────────────────────────────────────────────────────────────
	activeFeedData := feedState.Feeds[feedState.ActiveFeed]
	totalPostsInActiveFeed := len(activeFeedData.PostIDs)
	remainingPosts := totalPostsInActiveFeed - opts.LastSeenIndex

	// SCENARIO 1 : Le buffer actuel contient assez de posts pour satisfaire la demande client
	if remainingPosts >= opts.FetchCount {
		endIndex := opts.LastSeenIndex + opts.FetchCount
		return activeFeedData.PostIDs[opts.LastSeenIndex:endIndex], nil
	}

	// SCENARIO 2 & 3 : Le buffer est presque vide, il faut l'étendre
	var extendedCandidateIDs []int64

	// Mémorisation des posts actuels pour éviter les doublons lors de la greffe
	alreadyInActiveFeedMap := make(map[int64]struct{}, totalPostsInActiveFeed)
	for _, id := range activeFeedData.PostIDs {
		alreadyInActiveFeedMap[id] = struct{}{}
	}

	if !activeFeedData.Fused {
		// SCENARIO 2 (FUSION) : On recycle le travail en attente des paniers voisins (B et C)
		for feedName, feedData := range feedState.Feeds {
			if feedName != feedState.ActiveFeed {
				for _, candidateID := range feedData.PostIDs {
					if _, alreadyExists := alreadyInActiveFeedMap[candidateID]; !alreadyExists {
						extendedCandidateIDs = append(extendedCandidateIDs, candidateID)
						alreadyInActiveFeedMap[candidateID] = struct{}{}
					}
				}
			}
		}
		activeFeedData.Fused = true

	} else {
		// SCENARIO 3 (EXTENSION INFINIE) : Les paniers voisins sont vides, on doit ré-interroger Redis
		extendedBasket, err := collectSingleBasket(ctx, opts.UserID, activeFeedData.Seed, opts.Quotas)
		if err != nil {
			return nil, err
		}

		for _, item := range extendedBasket.Items {
			if _, alreadyExists := alreadyInActiveFeedMap[item.PostID]; !alreadyExists {
				extendedCandidateIDs = append(extendedCandidateIDs, item.PostID)
				alreadyInActiveFeedMap[item.PostID] = struct{}{}
			}
		}
	}

	// Passage à la Caissière pour filtrer et scrorer cette nouvelle extension
	opts.PersonalOpts.CandidateIDs = extendedCandidateIDs
	opts.PersonalOpts.Seed = activeFeedData.Seed
	opts.PersonalOpts.StartIndex = totalPostsInActiveFeed // Maintient la continuité de la Vague de Sérendipité

	freshlyScoredFeedIDs, err := buildPersonalizedFeed(ctx, opts.PersonalOpts)
	if err != nil {
		return nil, err
	}

	// On greffe les nouveaux posts au flux actif existant
	activeFeedData.PostIDs = append(activeFeedData.PostIDs, freshlyScoredFeedIDs...)
	feedState.Feeds[feedState.ActiveFeed] = activeFeedData
	err = saveUserFeedState(ctx, opts.UserID, feedState)
	if err != nil {
		return nil, err
	}

	newTotalPostsInActiveFeed := len(activeFeedData.PostIDs)

	// Sécurité absolue (ex: base de données vide au lancement du projet)
	if newTotalPostsInActiveFeed == 0 {
		return []int64{}, nil
	}

	// ─────────────────────────────────────────────────────────────────────────────
	// SCENARIO 4 : L'AMNÉSIE (L'utilisateur a consommé 100% du contenu généré)
	// ─────────────────────────────────────────────────────────────────────────────
	if opts.LastSeenIndex >= newTotalPostsInActiveFeed {
		// On le bascule en douceur sur un autre flux (A, B ou C).
		// La nouvelle Seed va re-scorer le contenu et inverser les pics de Dopamine.
		switch feedState.ActiveFeed {
		case "A":
			feedState.ActiveFeed = "B"
		case "B":
			feedState.ActiveFeed = "C"
		case "C":
			feedState.ActiveFeed = "A"
		}
		err = saveUserFeedState(ctx, opts.UserID, feedState)
		if err != nil {
			return nil, err
		}

		// On pioche instantanément le haut du nouveau panier
		rotatedFeedIDs := feedState.Feeds[feedState.ActiveFeed].PostIDs
		rotatedEndIndex := opts.FetchCount
		if rotatedEndIndex > len(rotatedFeedIDs) {
			rotatedEndIndex = len(rotatedFeedIDs)
		}
		return rotatedFeedIDs[0:rotatedEndIndex], nil
	}

	// Renvoi classique du bloc paginé
	finalEndIndex := opts.LastSeenIndex + opts.FetchCount
	if finalEndIndex > newTotalPostsInActiveFeed {
		finalEndIndex = newTotalPostsInActiveFeed // Prévention Out-Of-Bounds
	}

	return activeFeedData.PostIDs[opts.LastSeenIndex:finalEndIndex], nil
}
