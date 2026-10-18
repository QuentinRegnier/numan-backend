package two_factor_service

import (
	"context"
	"fmt"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/cuckoo"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/security"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	postgresgo "github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/email_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// loadUserByIDCascade charge un utilisateur par son ID (L2 -> L3) avec auto-guérison.
func loadUserByIDCascade(ctx context.Context, userID int64) (auth_models.UserPayload, error) {
	userPayload, errMongo := mongo.MongoLoadUser(ctx, userID, "", "", "")
	if errMongo == nil && userPayload.ID != 0 {
		return userPayload, nil
	}

	userPayload, errPg := postgresgo.FuncLoadUser(ctx, userID, "", "", "")
	if errPg != nil {
		return auth_models.UserPayload{}, numan_error.NewInternal(errPg)
	}
	if userPayload.ID == 0 {
		return auth_models.UserPayload{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Utilisateur introuvable.", nil)
	}

	_ = redis.EnqueueDB(ctx, userPayload.ID, 0, redis.EntityUser, redis.ActionUpdate, userPayload, redis.TargetMongo)
	return userPayload, nil
}

// finalizeLoginSession crée ou met à jour la session appareil après validation 2FA au Login.
func finalizeLoginSession(
	ctx context.Context,
	userPayload auth_models.UserPayload,
	input auth_models.LoginInput,
	ipAddresses []string,
) (two_factor_models.VerifyTwoFactorOutput, error) {
	currentTime := time.Now().UTC()
	isNewSession := false
	deviceFirebaseID := input.FirebaseInstallationID

	sessionPayload, err := cache_service.LoadSessionFromCache(ctx, userPayload.ID, deviceFirebaseID, "")
	if err != nil {
		return two_factor_models.VerifyTwoFactorOutput{}, numan_error.NewInternal()
	}

	if sessionPayload.ID == 0 {
		sessionPayload, _ = mongo.MongoLoadSession(ctx, userPayload.ID, deviceFirebaseID, "", "")
		if sessionPayload.ID == 0 {
			sessionPayload, err = postgresgo.FuncLoadSession(ctx, -1, userPayload.ID, deviceFirebaseID, "")
			if err != nil {
				return two_factor_models.VerifyTwoFactorOutput{}, numan_error.NewInternal()
			}
			if sessionPayload.ID != 0 {
				go func(s auth_models.SessionsPayload) {
					_ = redis.EnqueueDB(context.Background(), s.ID, s.UserID, redis.EntitySession, redis.ActionUpdate, s, redis.TargetMongo)
				}(sessionPayload)
			}
		}
		if sessionPayload.ID != 0 {
			_ = cache_service.SetSessionInCache(ctx, sessionPayload)
		}
	}

	if sessionPayload.ID != 0 {
		sessionPayload.DeviceInfo = input.DeviceInfo
		if len(ipAddresses) > 0 && !pkg.Exists(sessionPayload.IPHistory, ipAddresses[0]) {
			sessionPayload.IPHistory = append(sessionPayload.IPHistory, ipAddresses[0])
		}
	} else {
		isNewSession = true
		sessionPayload.ID = pkg.GenerateID()
		sessionPayload.UserID = userPayload.ID
		sessionPayload.CreatedAt = domain.TimeToMillis(currentTime)
		sessionPayload.FirebaseInstallationID = deviceFirebaseID
		sessionPayload.DeviceInfo = input.DeviceInfo
		if len(ipAddresses) > 0 {
			sessionPayload.IPHistory = []string{ipAddresses[0]}
		} else {
			return two_factor_models.VerifyTwoFactorOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'adresse IP est requise.", nil)
		}
	}

	var errToken error
	sessionPayload.ExpiresAt = domain.TimeToMillis(currentTime.Add(time.Duration(variables.MasterTokenExpirationSeconds) * time.Second))
	sessionPayload.MasterToken, errToken = pkg.GenerateToken(userPayload.ID, deviceFirebaseID, variables.MasterTokenExpirationSeconds)
	if errToken != nil {
		return two_factor_models.VerifyTwoFactorOutput{}, numan_error.NewInternal()
	}

	sessionPayload.CurrentSecret = security.DeriveNextSecret(sessionPayload.FirebaseInstallationID, sessionPayload.MasterToken, sessionPayload.MasterToken, sessionPayload.FirebaseInstallationID)
	sessionPayload.LastSecret = sessionPayload.FirebaseInstallationID
	sessionPayload.LastJWT = ""
	sessionPayload.ToleranceTime = domain.TimeToMillis(time.Time{})

	newJWT, errJwt := pkg.GenerateToken(userPayload.ID, sessionPayload.FirebaseInstallationID, variables.JWTExpirationSeconds)
	if errJwt != nil {
		return two_factor_models.VerifyTwoFactorOutput{}, numan_error.NewInternal()
	}

	_ = cache_service.SetSessionInCache(ctx, sessionPayload)

	timelineKey := fmt.Sprintf("profile:posts:zset:%d", userPayload.ID)
	if timelineExists, errTimeline := redis.Exists(ctx, timelineKey); errTimeline != nil || !timelineExists {
		_ = cache_service.MarkUserTimelineEmpty(ctx, userPayload.ID)
	}

	var liteUser lite_models.UserLiteRequest
	if errSpeed := redis.UsersLite.GetObject(ctx, userPayload.ID, &liteUser); errSpeed != nil {
		if settingsPayload, errSet := object_cache_service.GetUserSettingsCascade(ctx, userPayload.ID); errSet == nil {
			_ = cache_service.AddUserToSpeedCache(ctx, userPayload, settingsPayload)
		}
	}

	dbAction := redis.ActionUpdate
	if isNewSession {
		dbAction = redis.ActionCreate
	}
	_ = redis.EnqueueDB(ctx, sessionPayload.ID, userPayload.ID, redis.EntitySession, dbAction, sessionPayload, redis.TargetAll)

	return two_factor_models.VerifyTwoFactorOutput{
		Flow:        "login",
		UserID:      userPayload.ID,
		MasterToken: sessionPayload.MasterToken,
		JWT:         newJWT,
		ExpiresAt:   sessionPayload.ExpiresAt,
		Message:     "Connexion validée avec succès.",
	}, nil
}

// finalizeSignupAfterTwoFactor crée effectivement l'utilisateur, sa session et ses settings après validation de sa preuve 2FA.
func finalizeSignupAfterTwoFactor(
	ctx context.Context,
	preAuth auth_models.PreAuthSession,
	verifiedMethod int,
	passkeyToSave *two_factor_models.PasskeyPayload,
) (two_factor_models.VerifyTwoFactorOutput, error) {
	input := preAuth.SignUpData
	newUserID := preAuth.UserID
	currentTime := time.Now().UTC()
	newSessionID := pkg.GenerateID()

	parsedBirthdate, errParse := time.Parse("02012006", input.Birthdate)
	if errParse != nil {
		return two_factor_models.VerifyTwoFactorOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format de date de naissance invalide.", errParse)
	}

	if input.ProfilePictureID > 0 {
		if errMedia := media_service.ActivateMediaBatch(ctx, []int64{input.ProfilePictureID}, newUserID); errMedia != nil {
			return two_factor_models.VerifyTwoFactorOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Impossible de valider la photo de profil.", errMedia)
		}
	}

	ipAddr := ""
	if len(preAuth.IPAddresses) > 0 {
		ipAddr = preAuth.IPAddresses[0]
	}

	userPayload := auth_models.UserPayload{
		ID:               newUserID,
		Username:         input.Username,
		Email:            input.Email,
		EmailVerified:    verifiedMethod == variables.TwoFactorMethodEmail,
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
		TwoFactorEnabled: true,
		TwoFactorMethod:  methodToBit(verifiedMethod),
		TOTPSecret:       preAuth.TempTOTPSecret,
		CreatedAt:        domain.TimeToMillis(currentTime),
		UpdatedAt:        domain.TimeToMillis(currentTime),
	}

	sessionPayload := auth_models.SessionsPayload{
		ID:                     newSessionID,
		UserID:                 newUserID,
		FirebaseInstallationID: input.FirebaseInstallationID,
		DeviceInfo:             input.DeviceInfo,
		IPHistory:              []string{ipAddr},
		CreatedAt:              domain.TimeToMillis(currentTime),
		ExpiresAt:              domain.TimeToMillis(currentTime.Add(time.Duration(variables.MasterTokenExpirationSeconds) * time.Second)),
		ToleranceTime:          domain.TimeToMillis(currentTime.Add(time.Duration(variables.ToleranceTimeSeconds) * time.Second)),
	}

	var errToken error
	sessionPayload.MasterToken, errToken = pkg.GenerateToken(userPayload.ID, sessionPayload.FirebaseInstallationID, variables.MasterTokenExpirationSeconds)
	if errToken != nil {
		return two_factor_models.VerifyTwoFactorOutput{}, numan_error.NewInternal()
	}

	sessionPayload.CurrentSecret = security.DeriveNextSecret(sessionPayload.FirebaseInstallationID, sessionPayload.MasterToken, sessionPayload.MasterToken, sessionPayload.FirebaseInstallationID)
	sessionPayload.LastSecret = sessionPayload.FirebaseInstallationID

	newJWT, errJwt := pkg.GenerateToken(userPayload.ID, sessionPayload.FirebaseInstallationID, variables.JWTExpirationSeconds)
	if errJwt != nil {
		return two_factor_models.VerifyTwoFactorOutput{}, numan_error.NewInternal()
	}

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
		TelemetryVector:    nil,
		TelemetryTags:      nil,
		TelemetryTimestamp: 0,
		CreatedAt:          domain.TimeToMillis(currentTime),
		UpdatedAt:          domain.TimeToMillis(currentTime),
	}

	_ = cache_service.MarkUserTimelineEmpty(ctx, userPayload.ID)
	_ = cache_service.SetSessionInCache(ctx, sessionPayload)
	_ = cache_service.AddUserToSpeedCache(ctx, userPayload, userSettingsPayload)
	_ = object_cache_service.SetUserSettings(ctx, userSettingsPayload)

	_ = redis.EnqueueDB(ctx, newUserID, 0, redis.EntityUser, redis.ActionCreate, userPayload, redis.TargetAll)
	_ = redis.EnqueueDB(ctx, newSessionID, newUserID, redis.EntitySession, redis.ActionCreate, sessionPayload, redis.TargetAll)
	_ = redis.EnqueueDB(ctx, settingsID, newUserID, redis.EntityUserSettings, redis.ActionCreate, userSettingsPayload, redis.TargetAll)

	if passkeyToSave != nil {
		_ = object_cache_service.SetPasskeyInObjectCache(ctx, *passkeyToSave)
		_ = redis.EnqueueDB(ctx, passkeyToSave.ID, newUserID, redis.EntityPasskey, redis.ActionCreate, *passkeyToSave, redis.TargetAll)
	}

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

	var avatarView media_models.MediaView
	if userPayload.ProfilePictureID > 0 {
		if view, errMedia := media_service.GenerateMediaViewCascade(ctx, userPayload.ProfilePictureID, newUserID, 0, newUserID); errMedia == nil {
			avatarView = view
		}
	}

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

	return two_factor_models.VerifyTwoFactorOutput{
		Flow:        "signup",
		UserID:      newUserID,
		MasterToken: sessionPayload.MasterToken,
		JWT:         newJWT,
		ExpiresAt:   sessionPayload.ExpiresAt,
		Avatar:      avatarView,
		Message:     "Inscription et double authentification validées avec succès.",
	}, nil
}

// MethodToBit convertit l'identifiant de méthode (1, 2, 3) en bitmask (1, 2, 4).
func methodToBit(method int) int {
	switch method {
	case variables.TwoFactorMethodEmail:
		return variables.TwoFactorBitEmail
	case variables.TwoFactorMethodTOTP:
		return variables.TwoFactorBitTOTP
	case variables.TwoFactorMethodPasskey:
		return variables.TwoFactorBitPasskey
	default:
		return 0
	}
}
