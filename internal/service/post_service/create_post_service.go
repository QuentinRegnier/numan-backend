package post_service

import (
	"context"
	"fmt"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/algorithm_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : CRÉATION D'UNE PUBLICATION (POST)
// ############################################################################

// CreatePost orchestre la publication d'un post en vérifiant les droits
// d'identification, en activant les médias et en calculant le vecteur sémantique.
func CreatePost(ctx context.Context, callerID int64, input post_models.CreatePostInput) (int64, error) {
	// ── ÉTAPE 0 : VÉRIFICATION DE LA CONFORMITÉ DU PAYLOAD  ────────────

	if len(input.MediaIDs) == 0 {
		return -1, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Au moins un média est requis.", nil)
	}

	if err_hashtags := pkg.ListLimitVerif(pkg.SliceUniqueStr(input.Hashtags), variables.MaxHashtagsPerPost); err_hashtags != nil {
		return -1, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nombre de hashtags dépasse la limite autorisée.", err_hashtags)
	}

	if err_identifiers := pkg.ListLimitVerif(pkg.SliceUniqueInt64(input.Identifiers), variables.MaxIdentifiersPerPost); err_identifiers != nil {
		return -1, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nombre d'identifiants dépasse la limite autorisée.", err_identifiers)
	}

	if err_media := pkg.ListLimitVerif(pkg.SliceUniqueInt64(input.MediaIDs), variables.MaxMediaPerPost); err_media != nil {
		return -1, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nombre de médias dépasse la limite autorisée.", err_media)
	}

	if input.Visibility < variables.PostVisibilityAll || input.Visibility > variables.PostVisibilityFriend {
		return -1, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Niveau de visibilité non supporté.", nil)
	}

	// ── ÉTAPE 1 : BOUCLIER DE CONFIDENTIALITÉ (TAGS ET MENTIONS) ────────────

	mentionedUserIDs := pkg.ExtractMentions(input.Content)

	verifyPrivacyPermissions := func(targetUserIDs []int64, permissionType string) error {
		for _, targetID := range targetUserIDs {
			if targetID == callerID {
				continue
			}

			targetUserLite, errLite := cache_service.GetUserLite(ctx, targetID)
			if errLite != nil || targetUserLite.ID == 0 {
				continue // Utilisateur introuvable ignoré silencieusement
			}

			relationState := cache_service.RelationValue(ctx, targetID, callerID)
			if relationState == variables.RelationStateBlocked {
				return numan_error.NewForbidden(numan_error.CodeForbidden, "Action impossible : vous ne pouvez pas identifier un utilisateur qui vous a bloqué.", nil)
			}

			activePermission := targetUserLite.AllowTagging
			if permissionType == "mention" {
				activePermission = targetUserLite.AllowMentions
			}

			isActionAllowed := false
			switch activePermission {
			case variables.TagPermissionEveryone:
				isActionAllowed = true
			case variables.TagPermissionFollowers:
				isActionAllowed = relationState >= 1
			case variables.TagPermissionFriends:
				isActionAllowed = relationState == 2
			case variables.TagPermissionNobody:
				isActionAllowed = false
			}

			if !isActionAllowed {
				return numan_error.NewForbidden(numan_error.CodeForbidden, "Un des utilisateurs identifiés restreint les identifications ou mentions.", nil)
			}
		}
		return nil
	}

	if errTags := verifyPrivacyPermissions(input.Identifiers, "tag"); errTags != nil {
		return -1, errTags
	}

	if errMentions := verifyPrivacyPermissions(mentionedUserIDs, "mention"); errMentions != nil {
		return -1, errMentions
	}

	currentTime := time.Now().UTC()
	newPostID := pkg.GenerateID()

	// ── ÉTAPE 2 : DÉLÉGATION - ACTIVATION DES MÉDIAS FANTÔMES ───────────────

	if errMedia := media_service.ActivateMediaBatch(ctx, input.MediaIDs, callerID); errMedia != nil {
		return -1, errMedia // ActivateMediaBatch gère déjà ses propres numan_error
	}

	// ── ÉTAPE 3 : ÉVALUATION DYNAMIQUE DE LA PRIORITÉ ───────────────────────

	authorPriorityLevel := 0
	if callerUserLite, errLite := cache_service.GetUserLite(ctx, callerID); errLite == nil {
		gradeToPriorityMap := map[int]int{
			0: 0, 1: 1, 2: 2, 3: 3, 4: 4,
		}
		if priorityValue, exists := gradeToPriorityMap[callerUserLite.Grade]; exists {
			authorPriorityLevel = priorityValue
		}
	}

	// ── ÉTAPE 4 : OPTIMISATION ALGORITHMIQUE (AUTO-TAG AUTEUR) ──────────────

	authorHashtag := fmt.Sprintf("%s%d", variables.AuthorTagPrefix, callerID)
	isAuthorAlreadyTagged := false

	for _, hashtag := range input.Hashtags {
		if hashtag == authorHashtag {
			isAuthorAlreadyTagged = true
			break
		}
	}

	finalHashtagsList := input.Hashtags
	if !isAuthorAlreadyTagged {
		finalHashtagsList = append(finalHashtagsList, authorHashtag)
	}

	// ── ÉTAPE 5 : ASSEMBLAGE DE L'OBJET POST ────────────────────────────────

	postPayload := post_models.PostPayload{
		ID:                newPostID,
		UserID:            callerID,
		Content:           pkg.CleanStr(input.Content),
		Hashtags:          finalHashtagsList,
		IndirectHashtags:  nil,
		Identifiers:       pkg.SliceUniqueInt64(input.Identifiers),
		MediaIDs:          pkg.SliceUniqueInt64(input.MediaIDs),
		Visibility:        input.Visibility,
		PriorityLevel:     authorPriorityLevel,
		Location:          pkg.CleanStr(input.Location),
		CreatedAt:         domain.TimeToMillis(currentTime),
		UpdatedAt:         domain.TimeToMillis(currentTime),
		LikeCount:         0,
		CommentCount:      0,
		ViewCount:         0,
		ReportCount:       0,
		HasMedia:          len(input.MediaIDs) > 0,
		VectorVersion:     1,
		TelemetryDwellSum: 0.0,
		TelemetryDwellSq:  0.0,
		TelemetryClicks:   0,
	}

	// ── ÉTAPE 6 : VECTORISATION SÉMANTIQUE SYNCHRONE (O(1)) ─────────────────

	postPayload.Vector = algorithm_service.ComputeContentVectorFull(ctx, postPayload, nil)

	// ── ÉTAPE 7 : CACHE REDIS L1 ET INDEXATION TIMELINE ─────────────────────

	if errCache := object_cache_service.SetPostInObjectCache(ctx, postPayload); errCache != nil {
		numan_log.Warn(context.Background()).Err(errCache).Int64("post_id", newPostID).Msg("Échec de la mise en cache de la publication")
	}

	_ = cache_service.AddPostToUserProfile(ctx, callerID, newPostID, float64(currentTime.UnixMilli()))

	// ── ÉTAPE 8 : PERSISTANCE ASYNCHRONE (WRITE-BEHIND) ─────────────────────

	errQueue := redis.EnqueueDB(ctx, newPostID, 0, redis.EntityPost, redis.ActionCreate, postPayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(context.Background()).Err(errQueue).Int64("post_id", newPostID).Msg("Échec du Write-Behind lors de la création d'un post")
		return -1, numan_error.NewInternal()
	}

	return newPostID, nil
}
