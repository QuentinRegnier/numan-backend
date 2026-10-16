package profile_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/profile_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/post_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : RÉCUPÉRATION GLOBALE DU PROFIL
// ############################################################################

// GetProfile est le Hub central qui orchestre la récupération de toutes
// les strates d'un profil (Identité, Posts, Interactions, Paramètres de confidentialité).
func GetProfile(ctx context.Context, callerID int64, input profile_models.GetProfileInput) (profile_models.GetProfileOutput, error) {
	var errOffset, errLimit numan_error.Error
	input.Offset, errOffset, input.Limit, errLimit = pkg.BatchVerif(input.Offset, input.Limit)
	if errOffset != nil || errLimit != nil {
		return profile_models.GetProfileOutput{}, numan_error.Combine(errOffset, errLimit)
	}

	targetID := input.TargetID
	if targetID == 0 {
		targetID = callerID // Si pas de cible, on charge notre propre profil
	}

	profileOutput := profile_models.GetProfileOutput{
		LikedPostIDs: make([]int64, 0),
		SavedPostIDs: make([]int64, 0),
		Posts:        make([]post_models.GetPostOutput, 0),
	}

	isSelfProfile := callerID == targetID

	// ── ÉTAPE 1 : MATRICE DE RELATIONS (O(1) EN RAM) ────────────────────────

	if !isSelfProfile {
		profileOutput.RelationViewerToTarget = cache_service.RelationValue(ctx, targetID, callerID)
		profileOutput.RelationTargetToViewer = cache_service.RelationValue(ctx, callerID, targetID)

		// Bouclier de sécurité : si la cible nous a bloqués (-1), on simule une 404 (Shadow ban)
		if profileOutput.RelationTargetToViewer == variables.RelationStateBlocked {
			return profile_models.GetProfileOutput{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Utilisateur introuvable.", nil)
		}
		// Si NOUS l'avons bloqué (RelationViewerToTarget == -1), on laisse passer la requête
		// pour permettre le déblocage via l'UI de l'application.
	}

	// ── ÉTAPE 2 : VÉRIFICATION DE LA VISIBILITÉ DU PROFIL ───────────────────

	// Lecture L1 -> L2 -> L3 pour récupérer les réglages de confidentialité de la cible
	targetSettingsPayload, errSettings := object_cache_service.GetUserSettingsCascade(ctx, targetID)

	if errSettings == nil && !isSelfProfile {
		canViewProfile := false

		switch targetSettingsPayload.Privacy.ProfileVisibility {
		case variables.ProfileVisibilityPublic:
			canViewProfile = true
		case variables.ProfileVisibilityFollowers:
			canViewProfile = profileOutput.RelationViewerToTarget >= variables.RelationStateFollow
		case variables.ProfileVisibilityFriends:
			canViewProfile = profileOutput.RelationViewerToTarget == variables.RelationStateFriend
		default:
			canViewProfile = true
		}

		if !canViewProfile {
			// Si on rejette, on s'arrête ici : on économise toute la BDD (pas de chargement de posts)
			return profile_models.GetProfileOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Ce profil est privé.", nil)
		}
	}

	// ── ÉTAPE 3 : IDENTITÉ DE L'UTILISATEUR (CASCADE L2 -> L3) ──────────────

	userPayload, errMongo := mongo.MongoLoadUser(ctx, targetID, "", "", "")

	if errMongo != nil || userPayload.ID == 0 {
		var errPg error
		userPayload, errPg = postgres.FuncLoadUser(ctx, targetID, "", "", "")
		if errPg != nil {
			numan_log.Error(ctx).Err(errPg).Int64("user_id", targetID).Msg("Échec de la récupération L3 du profil utilisateur")
			return profile_models.GetProfileOutput{}, numan_error.NewInternal()
		}

		if userPayload.ID == 0 {
			return profile_models.GetProfileOutput{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Utilisateur introuvable.", nil)
		}

		// Promotion L3 -> L2 asynchrone (Auto-Guérison)
		go func(u auth_models.UserPayload) {
			_ = redis.EnqueueDB(context.Background(), u.ID, 0, redis.EntityUser, redis.ActionUpdate, u, redis.TargetMongo)
		}(userPayload)
	}

	// Application : Masquage de la localisation si refusé par l'utilisateur
	censoredLocation := userPayload.Location
	if errSettings == nil && !targetSettingsPayload.Privacy.ShowLocation && !isSelfProfile {
		censoredLocation = ""
	}

	// Application : Masquage du statut en ligne si refusé
	isUserOnline := cache_service.IsUserOnline(ctx, userPayload.ID)
	if errSettings == nil && !targetSettingsPayload.Privacy.ShowOnlineStatus && !isSelfProfile {
		isUserOnline = false
	}

	userBadges := userPayload.Badges
	if userBadges == nil {
		userBadges = auth_models.UserBadges{}
	}

	profileOutput.User = auth_models.UserProfileView{
		ID:        userPayload.ID,
		Username:  userPayload.Username,
		FirstName: userPayload.FirstName,
		LastName:  userPayload.LastName,
		Birthdate: userPayload.Birthdate,
		Sex:       userPayload.Sex,
		Bio:       userPayload.Bio,
		Grade:     userPayload.Grade,
		Location:  censoredLocation,
		School:    userPayload.School,
		Work:      userPayload.Work,
		Badges:    userBadges,
		CreatedAt: userPayload.CreatedAt,
		UpdatedAt: userPayload.UpdatedAt,
		IsOnline:  isUserOnline,
	}

	// ── ÉTAPE 4 : AVATAR (GÉNÉRATION DU LIEN HMAC SIGNÉ) ────────────────────

	if userPayload.ProfilePictureID > 0 {
		if avatarView, errMedia := media_service.GenerateMediaViewCascade(ctx, userPayload.ProfilePictureID, targetID, 0, callerID); errMedia == nil {
			profileOutput.Avatar = avatarView
		}
	}

	// ── ÉTAPE 5 : CONVERSATION DIRECTE (MP) EXISTANTE ───────────────────────

	if !isSelfProfile {
		conversationID, errCache := cache_service.GetDirectConversationCache(ctx, callerID, targetID)
		if errCache == nil && conversationID > 0 {
			profileOutput.DirectConversationID = conversationID
		} else {
			directConversation, errPg := postgres.FuncGetDirectConversation(ctx, callerID, targetID)
			if errPg == nil && directConversation.ID > 0 {
				profileOutput.DirectConversationID = directConversation.ID
			}
		}
	}

	// ── ÉTAPE 6 : CHARGEMENT DU BATCH DE POSTS ──────────────────────────────

	timelineRequestInput := post_models.GetUserPostsInput{
		TargetUserID: targetID,
		Limit:        input.Limit,
		Offset:       input.Offset,
		Force:        false,
	}

	timelinePostsOutput, errTimeline := post_service.GetUserPosts(ctx, callerID, timelineRequestInput)
	if errTimeline != nil {
		return profileOutput, errTimeline
	}

	if len(timelinePostsOutput) > 0 {
		profileOutput.Posts = timelinePostsOutput

		// ── ÉTAPE 7 : HYDRATATION DES INTERACTIONS DU VIEWER ────────────────

		batchPostIDs := make([]int64, 0, len(timelinePostsOutput))
		for _, post := range timelinePostsOutput {
			batchPostIDs = append(batchPostIDs, post.PostID)
		}

		// Hydratation des Likes du caller
		for _, postID := range batchPostIDs {
			likesRecord, _ := postgres.FuncLoadLikes(ctx, 0, postID, callerID, 1, 0)
			if len(likesRecord) > 0 {
				profileOutput.LikedPostIDs = append(profileOutput.LikedPostIDs, postID)
			}
		}

		// Hydratation des sauvegardes (Saved Posts) du caller
		recentSavedPosts, _ := postgres.FuncLoadSavedPostsKeyset(ctx, callerID, nil, nil, 500)
		savedPostsMap := make(map[int64]bool)
		for _, savedRecord := range recentSavedPosts {
			savedPostsMap[savedRecord.PostID] = true
		}

		for _, postID := range batchPostIDs {
			if savedPostsMap[postID] {
				profileOutput.SavedPostIDs = append(profileOutput.SavedPostIDs, postID)
			}
		}
	}

	return profileOutput, nil
}
