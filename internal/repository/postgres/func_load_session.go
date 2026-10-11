package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/lib/pq"
)

func FuncLoadSession(ctx context.Context, ID int64, UserId int64, FirebaseInstallationID string, MasterToken string) (auth_models.SessionsPayload, error) {
	// 1. Vérification que les champs sont non nuls
	if ID == -1 && UserId == -1 && FirebaseInstallationID == "" && MasterToken == "" {
		numan_log.Error(ctx).Msg("FuncLoadSession : Champs requis manquants pour charger la session depuis Postgres")
		return auth_models.SessionsPayload{}, numan_error.NewInternal()
	}

	// 2. Préparation des arguments (gestion des types spéciaux)
	args := make([]any, 4)
	args[0] = ID                     // p_session_id (ex: UUID)
	args[1] = UserId                 // p_user_id (ex: UUID)
	args[2] = FirebaseInstallationID // p_firebase_installation_id (ex: "token_string")
	args[3] = MasterToken            // p_master_token (ex: "master_token_string")

	if ID == -1 {
		args[0] = nil
	}
	if UserId == -1 {
		args[1] = nil
	}
	if FirebaseInstallationID == "" {
		args[2] = nil
	}
	if MasterToken == "" {
		args[3] = nil
	}

	// 3. Définition de la requête SQL (TOUJOURS paramétrée pour éviter l'injection SQL)
	sqlStatement := `
		SELECT * FROM auth.func_load_sessions($1, $2, $3, $4)
	`

	// 4. Exécution via la connexion partagée du package 'db'
	//    Nous utilisons QueryRow car la fonction SQL retourne une seule valeur (le UUID)
	var res auth_models.SessionsPayload
	var deviceInfoBytes []byte
	var firebaseInstallationID sql.NullString // Le token peut être NULL en base (stocké en JSON string ou NULL)

	// not used variable
	res.CurrentSecret = ""
	res.LastSecret = ""
	res.LastJWT = ""
	res.ToleranceTime = domain.TimeToMillis(time.Time{})

	err := postgres.PostgresDB.QueryRow(sqlStatement, args...).Scan(
		&res.ID,
		&res.UserID,
		&res.MasterToken,
		&firebaseInstallationID, // <-- Scan sécurisé
		&deviceInfoBytes,
		pq.Array(&res.IPHistory), // <-- pq.Array obligatoire
		&res.CreatedAt,
		&res.ExpiresAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return auth_models.SessionsPayload{}, nil
		}
		numan_log.Error(ctx).Err(err).Msg("Erreur SQL inattendue lors de la vérification de l'enregistrement")
		return auth_models.SessionsPayload{}, numan_error.NewInternal()
	}

	// Traitement des données
	if firebaseInstallationID.Valid {
		res.FirebaseInstallationID = firebaseInstallationID.String
	}

	if len(deviceInfoBytes) > 0 {
		err = json.Unmarshal(deviceInfoBytes, &res.DeviceInfo)
		if err != nil {
			numan_log.Warn(ctx).Err(err).Msg("Erreur lors de la désérialisation des informations du dispositif")
			return auth_models.SessionsPayload{}, numan_error.NewInternal()
		}
	}

	return res, nil
}
