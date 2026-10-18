package auth_service

import (
	"context"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/cuckoo"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/security"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/email_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : CRÉATION D'UTILISATEUR (SIGN-UP)
// ############################################################################

// CreateUser orchestre l'inscription : règles métier, génération des modèles BDD,
// écriture asynchrone (Write-Behind) et upload de l'avatar.
func CreateUser(ctx context.Context, input auth_models.SignUpInput, ipAddress string) (auth_models.SignUpResponse, error) {

	// ── ÉTAPE 1 : RÈGLES MÉTIER ET VÉRIFICATIONS D'UNICITÉ ─────────────────

	if service.IsUnique(ctx, redis.EntityUser, "username", input.Username) == 0 {
		return auth_models.SignUpResponse{}, numan_error.NewConflict(numan_error.CodeConflict, "Ce nom d'utilisateur est déjà pris.", nil)
	}
	if service.IsUnique(ctx, redis.EntityUser, "email", input.Email) == 0 {
		return auth_models.SignUpResponse{}, numan_error.NewConflict(numan_error.CodeConflict, "Cet email est déjà utilisé.", nil)
	}
	if service.IsUnique(ctx, redis.EntityUser, "phone", input.Phone) == 0 {
		return auth_models.SignUpResponse{}, numan_error.NewConflict(numan_error.CodeConflict, "Ce numéro de téléphone est déjà utilisé.", nil)
	}

	parsedBirthdate, errParse := time.Parse("02012006", input.Birthdate)
	if errParse != nil {
		return auth_models.SignUpResponse{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le format de la date de naissance est invalide.", errParse)
	}

	userAgeInYears := time.Since(parsedBirthdate).Hours() / 24 / 365
	if userAgeInYears < 13 {
		return auth_models.SignUpResponse{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Vous devez avoir au moins 13 ans pour vous inscrire.", nil)
	}
	if userAgeInYears > 120 {
		return auth_models.SignUpResponse{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La date de naissance fournie est incohérente.", nil)
	}

	if input.Gender < 0 || input.Gender > 2 {
		return auth_models.SignUpResponse{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le genre sélectionné n'est pas reconnu.", nil)
	}

	// Interception Double Authentification (2FA) si demandée dès l'inscription
	if input.TwoFactorMethod > 0 {
		return initiateSignupTwoFactorChallenge(ctx, input, ipAddress)
	}

	// ── ÉTAPE 2 : GÉNÉRATION DES MODÈLES DE DONNÉES (La source de vérité) ──

	currentTime := time.Now().UTC()
	newUserID := pkg.GenerateID()
	newSessionID := pkg.GenerateID()

	numan_log.Info(ctx).Int64("user_id", newUserID).Msg("Initialisation d'un nouvel utilisateur...")

	// ACTIVATION DU MÉDIA (Processus Out-of-Band)
	if input.ProfilePictureID > 0 {
		if errMedia := media_service.ActivateMediaBatch(ctx, []int64{input.ProfilePictureID}, newUserID); errMedia != nil {
			return auth_models.SignUpResponse{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Impossible de valider la photo de profil.", errMedia)
		}
	}

	// A. Hydratation du Payload Utilisateur Principal
	userPayload := auth_models.UserPayload{
		ID:               newUserID,
		Username:         input.Username,
		Email:            input.Email,
		EmailVerified:    false,
		Phone:            input.Phone,
		PhoneVerified:    false,
		PasswordHash:     input.PasswordHash,
		FirstName:        pkg.CleanStr(input.FirstName),
		LastName:         pkg.CleanStr(input.LastName),
		Birthdate:        domain.TimeToMillis(parsedBirthdate),
		Sex:              input.Gender,
		Bio:              pkg.CleanStr(input.Bio),
		ProfilePictureID: input.ProfilePictureID,
		Grade:            0,
		Location:         pkg.CleanStr(input.Location),
		School:           pkg.CleanStr(input.School),
		Work:             pkg.CleanStr(input.Work),
		Badges:           auth_models.UserBadges{},
		Desactivated:     false,
		Banned:           false,
		TwoFactorEnabled: false,
		TwoFactorMethod:  0,
		TOTPSecret:       "",
		CreatedAt:        domain.TimeToMillis(currentTime),
		UpdatedAt:        domain.TimeToMillis(currentTime),
	}

	// B. Hydratation du Payload de la Session Appareil
	sessionPayload := auth_models.SessionsPayload{
		ID:                     newSessionID,
		UserID:                 newUserID,
		FirebaseInstallationID: input.FirebaseInstallationID,
		DeviceInfo:             input.DeviceInfo,
		IPHistory:              []string{ipAddress},
		CreatedAt:              domain.TimeToMillis(currentTime),
		ExpiresAt:              domain.TimeToMillis(currentTime.Add(time.Duration(variables.MasterTokenExpirationSeconds) * time.Second)),
		ToleranceTime:          domain.TimeToMillis(currentTime.Add(time.Duration(variables.ToleranceTimeSeconds) * time.Second)),
	}

	// Génération des Tokens Sécurisés
	var errToken error
	sessionPayload.MasterToken, errToken = pkg.GenerateToken(userPayload.ID, sessionPayload.FirebaseInstallationID, variables.MasterTokenExpirationSeconds)
	if errToken != nil {
		return auth_models.SignUpResponse{}, numan_error.NewInternal()
	}

	sessionPayload.CurrentSecret = security.DeriveNextSecret(sessionPayload.FirebaseInstallationID, sessionPayload.MasterToken, sessionPayload.MasterToken, sessionPayload.FirebaseInstallationID)
	sessionPayload.LastSecret = sessionPayload.FirebaseInstallationID

	newJWT, errJwt := pkg.GenerateToken(userPayload.ID, sessionPayload.FirebaseInstallationID, variables.JWTExpirationSeconds)
	if errJwt != nil {
		return auth_models.SignUpResponse{}, numan_error.NewInternal()
	}

	// C. Hydratation des Paramètres Utilisateurs (Settings)
	if input.Privacy.PostVisibilityDefault < variables.PostVisibilityAll || input.Privacy.PostVisibilityDefault > variables.PostVisibilityFriend {
		input.Privacy.PostVisibilityDefault = variables.PostVisibilityAll
	}
	settingsID := pkg.GenerateID()
	userSettingsPayload := user_settings_models.UserSettingsPayload{
		ID:                 settingsID,
		UserID:             newUserID,
		Privacy:            input.Privacy,
		Notifications:      input.Notifications,
		DisplayAndContent:  input.DisplayAndContent,
		TelemetryVector:    nil, // Profil vierge à l'inscription
		TelemetryTags:      nil,
		TelemetryTimestamp: 0,
		CreatedAt:          domain.TimeToMillis(currentTime),
		UpdatedAt:          domain.TimeToMillis(currentTime),
	}

	// ── ÉTAPE 3 : MISE EN CACHE IMMÉDIATE (L1 RAM - Évite les latences) ────

	if errCache := cache_service.MarkUserTimelineEmpty(ctx, userPayload.ID); errCache != nil {
		numan_log.Warn(ctx).Err(errCache).Int64("user_id", userPayload.ID).Msg("Échec initialisation Timeline ZSET")
	}

	if errCache := cache_service.SetSessionInCache(ctx, sessionPayload); errCache != nil {
		numan_log.Warn(ctx).Err(errCache).Int64("session_id", sessionPayload.ID).Msg("Échec mise en cache L1 de la Session")
	}

	if errCache := cache_service.AddUserToSpeedCache(ctx, userPayload, userSettingsPayload); errCache != nil {
		numan_log.Warn(ctx).Err(errCache).Int64("user_id", userPayload.ID).Msg("Échec mise en Speed Cache L1 de l'Utilisateur")
	}

	if errCache := object_cache_service.SetUserSettings(ctx, userSettingsPayload); errCache != nil {
		numan_log.Warn(ctx).Err(errCache).Int64("user_id", userPayload.ID).Msg("Échec mise en cache L1 des UserSettings")
	}

	// ── ÉTAPE 4 : PERSISTANCE ASYNCHRONE (Write-Behind vers L2/L3) ─────────

	if errQueue := redis.EnqueueDB(ctx, newUserID, 0, redis.EntityUser, redis.ActionCreate, userPayload, redis.TargetAll); errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("user_id", newUserID).Msg("CRITICAL: Impossible d'enqueue le User vers la BDD")
	}

	if errQueue := redis.EnqueueDB(ctx, newSessionID, newUserID, redis.EntitySession, redis.ActionCreate, sessionPayload, redis.TargetAll); errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("session_id", newSessionID).Msg("CRITICAL: Impossible d'enqueue la Session vers la BDD")
	}

	if errQueue := redis.EnqueueDB(ctx, settingsID, newUserID, redis.EntityUserSettings, redis.ActionCreate, userSettingsPayload, redis.TargetAll); errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("settings_id", settingsID).Msg("CRITICAL: Impossible d'enqueue les UserSettings vers la BDD")
	}

	// ── ÉTAPE 5 : CUCKOO FILTERS (Prévention O(1) de l'unicité en RAM) ─────

	if cuckoo.GlobalCuckoo != nil {
		cuckoo.GlobalCuckoo.Insert([]byte("username:" + userPayload.Username))
		cuckoo.GlobalCuckoo.Insert([]byte("email:" + userPayload.Email))
		cuckoo.GlobalCuckoo.Insert([]byte("phone:" + userPayload.Phone))
	}

	go func() {
		cuckoo.BroadcastCuckooUpdate(cuckoo.ActionAdd, "username", userPayload.Username)
		cuckoo.BroadcastCuckooUpdate(cuckoo.ActionAdd, "email", userPayload.Email)
		cuckoo.BroadcastCuckooUpdate(cuckoo.ActionAdd, "phone", userPayload.Phone)
	}()

	// ── ÉTAPE 6 : RÉPONSE DÉFINITIVE PRÊTE À ÊTRE SÉRIALISÉE ───────────────

	var avatarView media_models.MediaView
	if userPayload.ProfilePictureID > 0 {
		if view, errMedia := media_service.GenerateMediaViewCascade(ctx, userPayload.ProfilePictureID, newUserID, 0, newUserID); errMedia == nil {
			avatarView = view
		}
	}

	// EXPÉDITION ASYNCHRONE DE L'EMAIL DE BIENVENUE
	// Exécuté dans une goroutine dédiée avec context.Background() pour ne pas impacter le temps de réponse HTTP (< 5ms)
	go func(targetID int64, email, username string) {
		bgCtx := context.Background()
		_ = email_service.DispatchEmail(
			bgCtx,
			targetID,
			email,
			"welcome",
			"Bienvenue sur Numan !",
			map[string]any{
				"Username": username,
			},
		)
	}(userPayload.ID, userPayload.Email, userPayload.Username)

	return auth_models.SignUpResponse{
		RequiresTwoFactor: false,
		UserID:            newUserID,
		MasterToken:       sessionPayload.MasterToken,
		JWT:               newJWT,
		ExpiresAt:         sessionPayload.ExpiresAt,
		Message:           "Inscription validée avec succès.",
		Avatar:            avatarView,
	}, nil
}
