package user_settings_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/cuckoo"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/realtime_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : MISE À JOUR DU PROFIL
// ############################################################################

// UpdateProfile modifie l'identité, vérifie l'unicité des champs sensibles,
// gère le remplacement de l'avatar et envoie les mutations au Write-Behind.
func UpdateProfile(ctx context.Context, userID int64, input user_settings_models.UpdateProfileInput) (user_settings_models.UpdateProfileOutput, error) {
	if userID <= 0 {
		return user_settings_models.UpdateProfileOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur est invalide.", nil)
	}
	if err := input.Validate(); err != nil {
		return user_settings_models.UpdateProfileOutput{}, err
	}

	// ── ÉTAPE 1 : RÉCUPÉRATION DU PROFIL ACTUEL (CASCADE L2 -> L3) ──────────
	// Nécessaire pour préserver les champs critiques (Grade, Banni, etc.) non soumis au PUT.

	userPayload, errMongo := mongo.MongoLoadUser(ctx, userID, "", "", "")
	if errMongo != nil || userPayload.ID == 0 {
		var errPg error
		userPayload, errPg = postgres.FuncLoadUser(ctx, userID, "", "", "")
		if errPg != nil {
			return user_settings_models.UpdateProfileOutput{}, numan_error.NewInternal()
		}
		if userPayload.ID == 0 {
			return user_settings_models.UpdateProfileOutput{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Utilisateur introuvable.", nil)
		}
	}

	// Variables tampons pour mettre à jour le Cuckoo Filter si besoin
	var oldUsername, oldEmail, oldPhone string
	var newUsername, newEmail, newPhone string

	// ── ÉTAPE 2 : VÉRIFICATIONS D'UNICITÉ (CUCKOO -> L1 -> L2 -> L3) ────────

	if input.Username != userPayload.Username {
		if service.IsUnique(ctx, redis.EntityUser, "username", input.Username) == 0 {
			return user_settings_models.UpdateProfileOutput{}, numan_error.NewConflict(numan_error.CodeConflict, "Ce nom d'utilisateur est déjà pris.", nil)
		}
		oldUsername = userPayload.Username
		newUsername = input.Username
		userPayload.Username = input.Username
	}

	if input.Email != userPayload.Email {
		if service.IsUnique(ctx, redis.EntityUser, "email", input.Email) == 0 {
			return user_settings_models.UpdateProfileOutput{}, numan_error.NewConflict(numan_error.CodeConflict, "Cet email est déjà utilisé par un autre compte.", nil)
		}
		oldEmail = userPayload.Email
		newEmail = input.Email
		userPayload.Email = input.Email
		userPayload.EmailVerified = false // Nécessite une nouvelle vérification par email
	}

	if input.Phone != userPayload.Phone {
		if input.Phone != "" && service.IsUnique(ctx, redis.EntityUser, "phone", input.Phone) == 0 {
			return user_settings_models.UpdateProfileOutput{}, numan_error.NewConflict(numan_error.CodeConflict, "Ce numéro de téléphone est déjà associé à un autre compte.", nil)
		}
		oldPhone = userPayload.Phone
		newPhone = input.Phone
		userPayload.Phone = input.Phone
		userPayload.PhoneVerified = false // Nécessite une nouvelle vérification par SMS
	}

	// ── ÉTAPE 3 : REMPLACEMENT INTÉGRAL DES CHAMPS LIBRES ───────────────────

	userPayload.FirstName = pkg.CleanStr(input.FirstName)
	userPayload.LastName = pkg.CleanStr(input.LastName)
	userPayload.Bio = pkg.CleanStr(input.Bio)
	userPayload.Location = pkg.CleanStr(input.Location)
	userPayload.School = pkg.CleanStr(input.School)
	userPayload.Work = pkg.CleanStr(input.Work)

	// ── ÉTAPE 4 : GESTION DU MÉDIA DE PROFIL (AVATAR) ───────────────────────

	if input.ProfilePictureID != userPayload.ProfilePictureID {

		// A. Tuer l'ancien fantôme de MinIO/Base (S'il en avait un)
		if userPayload.ProfilePictureID > 0 {
			_ = media_service.DeactivateMediaBatch(ctx, []int64{userPayload.ProfilePictureID}, userID)
		}

		// B. Activer la nouvelle image (Processus Out-Of-Band)
		if input.ProfilePictureID > 0 {
			if errAct := media_service.ActivateMediaBatch(ctx, []int64{input.ProfilePictureID}, userID); errAct != nil {
				// L'avatar envoyé n'est pas valide ou n'appartient pas à cet utilisateur.
				return user_settings_models.UpdateProfileOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Impossible de valider la nouvelle photo de profil.", errAct)
			}
		}
		userPayload.ProfilePictureID = input.ProfilePictureID
	}

	userPayload.UpdatedAt = domain.NowMillis()

	if err := userPayload.Validate(); err != nil {
		return user_settings_models.UpdateProfileOutput{}, err
	}

	// ── ÉTAPE 5 : RAFRAÎCHISSEMENT IMMÉDIAT DU CACHE RAM (L1) ───────────────

	// Récupération asynchrone/rapide des settings pour construire l'objet Lite du profil public
	settingsPayload, _ := object_cache_service.GetUserSettingsCascade(ctx, userID)
	if errCache := cache_service.AddUserToSpeedCache(ctx, userPayload, settingsPayload); errCache != nil {
		numan_log.Warn(ctx).Err(errCache).Msg("Impossible de mettre à jour l'utilisateur dans le Speed Cache L1")
	}

	// ── ÉTAPE 6 : MISE À JOUR DU CUCKOO FILTER (Asynchrone) ─────────────────

	if newUsername != "" {
		cuckoo.BroadcastCuckooUpdate(cuckoo.ActionAdd, "username", newUsername)
		cuckoo.BroadcastCuckooUpdate(cuckoo.ActionDel, "username", oldUsername)
	}
	if newEmail != "" {
		cuckoo.BroadcastCuckooUpdate(cuckoo.ActionAdd, "email", newEmail)
		cuckoo.BroadcastCuckooUpdate(cuckoo.ActionDel, "email", oldEmail)
	}
	if newPhone != "" {
		cuckoo.BroadcastCuckooUpdate(cuckoo.ActionAdd, "phone", newPhone)
		cuckoo.BroadcastCuckooUpdate(cuckoo.ActionDel, "phone", oldPhone)
	}

	// ── ÉTAPE 7 : ENVOI NOTIFICATION TEMPS RÉEL (WebSockets) ────────────────

	_ = realtime_service.DistributeToUsers(ctx, variables.NotificationProfileUpdated, userPayload, []int64{userID})

	// ── ÉTAPE 8 : PERSISTANCE ASYNCHRONE (Write-Behind) ─────────────────────

	errQueue := redis.EnqueueDB(ctx, userPayload.ID, 0, redis.EntityUser, redis.ActionUpdate, userPayload, redis.TargetAll)
	if errQueue != nil {
		// Log en erreur car le worker n'a pas reçu l'ordre, mais on ne fait pas crasher la requête HTTP.
		numan_log.Error(ctx).Err(errQueue).Int64("user_id", userPayload.ID).Msg("Échec du Write-Behind lors de l'Update Profile")
	}

	return user_settings_models.UpdateProfileOutput{
		ProfileUpdatedAt: userPayload.UpdatedAt,
	}, nil
}
