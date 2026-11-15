package conversation_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/vmihailenco/msgpack/v5"
)

// ############################################################################
// # SERVICE : SUGGESTION DE CONTACTS (AUTOCOMPLÉTION & LISTES)
// ############################################################################

// SuggestContacts orchestre la suggestion de membres selon l'intention demandée.
func SuggestContacts(ctx context.Context, callerID int64, input conversation_models.SuggestInput) (conversation_models.SuggestOutput, error) {
	// 1. Validation du payload et de la pagination
	if callerID <= 0 {
		return conversation_models.SuggestOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur est invalide.", nil)
	}
	if err := input.Validate(); err != nil {
		return conversation_models.SuggestOutput{}, err
	}
	if input.Limit == 0 {
		input.Limit = 20
	}

	// 2. Déduction de l'intention métier
	if input.Intent == "" {
		if input.ConversationID > 0 {
			input.Intent = "group"
		} else {
			input.Intent = "dm"
		}
	}

	// 3. Détermination des exclusions (utilisateurs déjà présents / MP existants)
	excludedUserIDsMap, errExclusion := buildExcludedUsersMap(ctx, callerID, input.Intent, input.ConversationID)
	if errExclusion != nil {
		return conversation_models.SuggestOutput{}, errExclusion
	}

	// 4. Récupération et filtrage itératif
	var validatedUsersList []auth_models.UserLiteView
	currentOffset := input.Offset
	maxFetchIterations := 3 // Prévention de boucle infinie

	for int64(len(validatedUsersList)) < input.Limit && maxFetchIterations > 0 {
		fetchBatchLimit := input.Limit * 2
		potentialCandidates := fetchCandidates(ctx, callerID, input.Query, currentOffset, fetchBatchLimit)

		if len(potentialCandidates) == 0 {
			break
		}

		for _, targetUserLite := range potentialCandidates {
			if int64(len(validatedUsersList)) >= input.Limit {
				break
			}

			if targetUserLite.ID == callerID || excludedUserIDsMap[targetUserLite.ID] {
				continue
			}

			// Matrice de Confidentialité
			relationState := cache_service.RelationValue(ctx, callerID, targetUserLite.ID)
			if !isCommunicationAllowed(input.Intent, relationState, targetUserLite) {
				continue
			}

			// Hydratation finale
			isUserOnline := cache_service.IsUserOnline(ctx, targetUserLite.ID)
			if !targetUserLite.ShowOnlineStatus {
				isUserOnline = false
			}

			var userAvatarView media_models.MediaView
			if targetUserLite.ProfilePictureID > 0 {
				if mediaView, errMedia := media_service.GenerateMediaViewCascade(ctx, targetUserLite.ProfilePictureID, targetUserLite.ID, 0, callerID); errMedia == nil {
					userAvatarView = mediaView
				}
			}

			validatedUsersList = append(validatedUsersList, auth_models.UserLiteView{
				User:     targetUserLite,
				Avatar:   userAvatarView,
				IsOnline: isUserOnline,
			})
		}

		if input.Query != "" {
			break // Les recherches textuelles Redis retournent déjà le meilleur match global.
		}

		currentOffset += fetchBatchLimit
		maxFetchIterations--
	}

	if validatedUsersList == nil {
		validatedUsersList = make([]auth_models.UserLiteView, 0)
	}

	return conversation_models.SuggestOutput{Users: validatedUsersList}, nil
}

// ── UTILITAIRES PRIVÉS DU SERVICE ───────────────────────────────────────────

// buildExcludedUsersMap construit la carte des identifiants à exclure de la suggestion.
func buildExcludedUsersMap(ctx context.Context, callerID int64, intentAction string, conversationID int64) (map[int64]bool, error) {
	excludedMap := make(map[int64]bool)

	if conversationID > 0 && intentAction == "group" {
		callerMemberPayload, errSecurity := security_service.LeftMember(ctx, conversationID, callerID)
		if errSecurity != nil || callerMemberPayload.Role < variables.MemberRoleNormal {
			return nil, numan_error.NewForbidden(numan_error.CodeForbidden, "Accès refusé : vous ne faites pas partie de ce groupe.", errSecurity)
		}

		participantsStringList, _ := redis.ConvParticipants.SMembers(ctx, conversationID)
		for _, parsedID := range pkg.ParseInt64List(participantsStringList) {
			excludedMap[parsedID] = true
		}

	} else if intentAction == "dm" {
		inboxConversationIDStrings, _ := redis.UserInbox.ZRevRange(ctx, callerID, 0, -1)
		inboxConversationIDs := pkg.ParseInt64List(inboxConversationIDStrings)

		conversationMetasBatch, _ := redis.ConvMeta.GetMany(ctx, inboxConversationIDs)
		for convID, rawData := range conversationMetasBatch.Found {
			var liteConversation lite_models.ConvLiteRequest
			if errUnpack := msgpack.Unmarshal(rawData, &liteConversation); errUnpack == nil && liteConversation.Type == variables.ConversationTypeDirect {

				directParticipantsStringList, _ := redis.ConvParticipants.SMembers(ctx, convID)
				for _, pStr := range directParticipantsStringList {
					if targetID := pkg.ParseInt64(pStr); targetID != 0 && targetID != callerID {
						excludedMap[targetID] = true
					}
				}
			}
		}
	}

	return excludedMap, nil
}
