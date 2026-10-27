package media_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : GÉNÉRATION D'URL SIGNÉE POUR LE CLIENT
// ############################################################################

// GetSignedURLForClient vérifie les droits d'accès en temps réel (Zero-Trust)
// et génère l'URL HMAC pour le client mobile ou web.
func GetSignedURLForClient(ctx context.Context, readerID int64, input media_models.SignMediaInput) (media_models.SignMediaOutput, error) {

	// ── ÉTAPE 0 : ANTI-AMBIGUÏTÉ DU CONTEXTE ────────────────────────────────
	if input.PostID > 0 && input.ConversationID > 0 {
		return media_models.SignMediaOutput{}, numan_error.NewBadRequest(
			"AMBIGUOUS_CONTEXT",
			"Impossible de spécifier simultanément un post_id et un conversation_id.",
			nil,
		)
	}

	// ── ÉTAPE 1 : RÉCUPÉRATION DU MÉDIA (CASCADE L1 -> L2 -> L3) ────────────
	mediaPayload, errCascade := getMediaCascade(ctx, input.MediaID)
	if errCascade != nil {
		return media_models.SignMediaOutput{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Média introuvable ou supprimé.", errCascade)
	}

	// BOUCLIER QUARANTAINE : Même avec une autorisation sur le Post/Conversation,
	// un média en quarantaine (Visibility = 2) ne peut jamais être chargé par les clients.
	if mediaPayload.Visibility == variables.MediaVisibilityQuarantine {
		return media_models.SignMediaOutput{}, numan_error.NewForbidden(
			numan_error.CodeMediaUnderReview,
			"Cette image est actuellement en cours de vérification par un modérateur et ne peut pas être chargée.",
			nil,
		)
	}
	if mediaPayload.Visibility == variables.MediaVisibilityRejected {
		return media_models.SignMediaOutput{}, numan_error.NewForbidden(
			numan_error.CodeMediaRejected,
			"Cette image a été retirée suite à une décision de modération.",
			nil,
		)
	}
	if mediaPayload.Visibility != variables.MediaVisibilityPublic {
		return media_models.SignMediaOutput{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Média introuvable ou supprimé.", nil)
	}

	// ── ÉTAPE 2 : MATRICE DE SÉCURITÉ CONTEXTUELLE ──────────────────────────
	var validatedContextID int64

	if input.PostID > 0 {
		// A. Cas d'une image attachée à une Publication (Post)
		postPayload, errSecurity := security_service.LeftPost(ctx, input.PostID, readerID)
		if errSecurity != nil {
			return media_models.SignMediaOutput{}, errSecurity // Propage l'AppError (Forbidden ou NotFound)
		}

		// Anti-Usurpation : Vérifier que le média demandé appartient réellement à ce post validé
		if !pkg.Exists(postPayload.MediaIDs, input.MediaID) {
			return media_models.SignMediaOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Ce média n'appartient pas à cette publication.", nil)
		}
		validatedContextID = input.PostID

	} else if input.ConversationID > 0 {
		// B. Cas d'une image échangée dans une Messagerie Privée (Conversation)
		memberPayload, errSecurity := security_service.LeftMember(ctx, input.ConversationID, readerID)
		if errSecurity != nil || memberPayload.Role < 0 {
			return media_models.SignMediaOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Vous n'avez pas accès à cette conversation.", errSecurity)
		}
		validatedContextID = input.ConversationID

	} else {
		// C. Cas d'un Avatar public (Ni Post, Ni Conversation)
		// Les avatars de profil sont publics, la vérification est allégée (contexte 0)
		validatedContextID = 0
	}

	// ── ÉTAPE 3 : GÉNÉRATION DU SCEAU CRYPTOGRAPHIQUE ───────────────────────
	signedURL := generateWatermarkedURL(mediaPayload.StoragePath, mediaPayload.OwnerID, validatedContextID, readerID)

	return media_models.SignMediaOutput{
		MediaID: input.MediaID,
		URL:     signedURL,
	}, nil
}
