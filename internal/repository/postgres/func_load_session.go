package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/lib/pq"
)

func FuncLoadSession(ctx context.Context, ID int64, UserId int64, FirebaseInstallationID string, MasterToken string) (auth_models.SessionsPayload, error) {
	// 1. Vérification que les champs sont non nuls
	if ID == -1 && UserId == -1 && FirebaseInstallationID == "" && MasterToken == "" {
		return auth_models.SessionsPayload{}, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Champs requis manquants pour charger la session.",
			errors.New("all session lookup identifiers are empty or invalid"),
		)
	}

	if postgres.PostgresDB == nil {
		return auth_models.SessionsPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
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

	err := postgres.PostgresDB.QueryRowContext(ctx, sqlStatement, args...).Scan(
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
		return auth_models.SessionsPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger la session.",
			err,
		)
	}

	// Traitement des données
	if firebaseInstallationID.Valid {
		res.FirebaseInstallationID = firebaseInstallationID.String
	}

	if len(deviceInfoBytes) > 0 {
		if err := json.Unmarshal(deviceInfoBytes, &res.DeviceInfo); err != nil {
			return auth_models.SessionsPayload{}, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de désérialiser les informations de l'appareil.",
				err,
			)
		}
	}

	return res, nil
}
