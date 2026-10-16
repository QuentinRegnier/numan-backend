package post_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/comment_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/comment_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : RÉCUPÉRATION ET FORMATAGE D'UN LOT DE POSTS
// ############################################################################

// GetPosts orchestre la récupération d'une liste de posts, applique le filtrage de
// visibilité et hydrate massivement les relations (Commentaires, Médias, Auteur).
func GetPosts(ctx context.Context, callerID int64, input post_models.GetPostInput) ([]post_models.GetPostOutput, error) {
	// 1. Sécurisation de la taille du lot (Correction de l'appel au package security)
	if err := pkg.ListLimitVerifDefault(input.PostIDs); err != nil {
		return nil, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La liste des publications demandées est invalide.", nil)
	}
	uniquePostIDs := pkg.SliceUniqueInt64(input.PostIDs)

	// 2. Fin du "nil" silencieux : Si la liste est vide, on lève une vraie erreur métier[cite: 9]
	if len(uniquePostIDs) == 0 {
		return nil, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La liste des publications demandées est vide.", nil)
	}

	hydratedPostsResults := make([]post_models.GetPostOutput, 0, len(uniquePostIDs))

	// Utilisation de l'assistant de cascade (Helpers) pour le chargement brut des payloads
	resolvedPostsMap := fetchPostsCascade(ctx, uniquePostIDs)

	for _, requestedPostID := range uniquePostIDs {
		postPayload, isFound := resolvedPostsMap[requestedPostID]

		if !isFound {
			hydratedPostsResults = append(hydratedPostsResults, post_models.GetPostOutput{
				PostID: requestedPostID,
				Error:  "Cette publication est introuvable ou a été supprimée.",
			})
			continue
		}

		// L'auteur a toujours accès inconditionnel à son propre post
		isCallerAuthor := postPayload.UserID == callerID

		// ── ÉTAPE 1 : MATRICE DE VISIBILITÉ ET RELATIONNELLE ────────────────

		// State: 0 = Rien, 1 = Abonné, 2 = Ami, -1 = Banni
		relationState := 0
		if !isCallerAuthor {
			relationState = cache_service.RelationValue(ctx, postPayload.UserID, callerID)
		}

		// Règle Z : Bannissement Croisé
		if relationState == variables.RelationStateBlocked {
			hydratedPostsResults = append(hydratedPostsResults, post_models.GetPostOutput{
				PostID: requestedPostID,
				Error:  "Cette publication est introuvable ou a été supprimée.", // Mode furtif
			})
			continue
		}

		// Règle A : Soft Delete
		if postPayload.Visibility == variables.PostVisibilityDeleted {
			hydratedPostsResults = append(hydratedPostsResults, post_models.GetPostOutput{
				PostID: requestedPostID,
				Error:  "Cette publication est introuvable ou a été supprimée.",
			})
			continue
		}

		// Règle B : Réservé aux Abonnés
		if postPayload.Visibility == variables.PostVisibilitySubcriber && !isCallerAuthor {
			if relationState < variables.RelationStateFollow {
				hydratedPostsResults = append(hydratedPostsResults, post_models.GetPostOutput{
					PostID: requestedPostID,
					Error:  "🔒 Ce post est strictement réservé aux abonnés de l'auteur.",
				})
				continue
			}
		}

		// Règle C : Réservé aux Amis
		if postPayload.Visibility == variables.PostVisibilityFriend && !isCallerAuthor {
			if relationState != variables.RelationStateFriend {
				hydratedPostsResults = append(hydratedPostsResults, post_models.GetPostOutput{
					PostID: requestedPostID,
					Error:  "🤝 Ce post est confidentiel et réservé au cercle d'amis de l'auteur.",
				})
				continue
			}
		}

		// Règle D : Sécurité - Visibilité non supportée (ancien mode privé ou invalide)
		if (postPayload.Visibility < variables.PostVisibilityAll || postPayload.Visibility > variables.PostVisibilityFriend) && !isCallerAuthor {
			hydratedPostsResults = append(hydratedPostsResults, post_models.GetPostOutput{
				PostID: requestedPostID,
				Error:  "Cette publication est introuvable ou n'est plus accessible.",
			})
			continue
		}

		// ── ÉTAPE 2 : HYDRATATION DU DTO (AUTEUR, MÉDIAS, COMMENTAIRES) ─────

		var resolvedAuthorUsername string
		var resolvedAuthorAvatar media_models.MediaView

		if authorUserLite, errLite := cache_service.GetUserLite(ctx, postPayload.UserID); errLite == nil {
			resolvedAuthorUsername = authorUserLite.Username

			if authorUserLite.ProfilePictureID > 0 {
				if avatarView, errMedia := media_service.GenerateMediaViewCascade(ctx, authorUserLite.ProfilePictureID, postPayload.UserID, postPayload.ID, callerID); errMedia == nil {
					resolvedAuthorAvatar = avatarView
				}
			}
		}

		// Signature HMAC des médias via le domaine dédié
		signedMediaViews := media_service.FormatMediaViewsCascade(ctx, postPayload.MediaIDs, postPayload.UserID, postPayload.ID, callerID)

		// Chargement direct des Top Commentaires
		commentRequestInput := comment_models.GetCommentsInput{
			PostID: requestedPostID,
			Limit:  int64(variables.MaxZsetPostComment), // Aligné sur le Cap L1 ZSET
			Offset: 0,
		}

		topCommentsList, errComments := comment_service.GetComments(ctx, callerID, commentRequestInput)
		if errComments != nil {
			// Propagation native de l'erreur au lieu de l'absorber
			return nil, errComments
		}

		// ── ÉTAPE 3 : ASSEMBLAGE FINAL DE LA VUE ────────────────────────────

		hydratedPostsResults = append(hydratedPostsResults, post_models.GetPostOutput{
			PostID:         requestedPostID,
			Data:           postPayload,
			AuthorUsername: resolvedAuthorUsername,
			AuthorAvatar:   resolvedAuthorAvatar,
			Media:          signedMediaViews,
			Comments:       topCommentsList,
		})
	}

	return hydratedPostsResults, nil
}
