package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

func FuncLoadUser(ctx context.Context, ID int64, Username string, Email string, Phone string) (auth_models.UserPayload, error) {

	// Args...
	args := make([]any, 4)
	args[0] = ID
	if ID == -1 {
		args[0] = nil
	}
	args[1] = Username
	if Username == "" {
		args[1] = nil
	}
	args[2] = Email
	if Email == "" {
		args[2] = nil
	}
	args[3] = Phone
	if Phone == "" {
		args[3] = nil
	}

	// 🕵️ DEBUG : On affiche la requête exacte
	fmt.Printf("\n🐘 POSTGRES QUERY : SELECT * FROM auth.func_load_user(%v, %v, '%v', %v)\n", args[0], args[1], args[2], args[3])

	sqlStatement := `SELECT * FROM auth.func_load_user($1, $2, $3, $4)`
	var res auth_models.UserPayload

	var birthdateRaw sql.NullString
	var profilePicID sql.NullInt64
	var banReason sql.NullString
	var banExpiresAt sql.NullTime
	var firstName sql.NullString
	var lastName sql.NullString
	var bio sql.NullString
	var location sql.NullString
	var school sql.NullString
	var work sql.NullString
	var createdAt time.Time // NOUVEAU
	var updatedAt time.Time // NOUVEAU

	err := postgres.PostgresDB.QueryRow(sqlStatement, args...).Scan(
		&res.ID,
		&res.Username,
		&res.Email,
		&res.EmailVerified,
		&res.Phone,
		&res.PhoneVerified,
		&res.PasswordHash,
		&firstName,
		&lastName,
		&birthdateRaw,
		&res.Sex,
		&bio,
		&profilePicID,
		&res.Grade,
		&location,
		&school,
		&work,
		&res.Badges,
		&res.Desactivated,
		&res.Banned,
		&banReason,
		&banExpiresAt,
		&createdAt, // AU LIEU DE &res.CreatedAt
		&updatedAt, // AU LIEU DE &res.UpdatedAt
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return auth_models.UserPayload{}, nil // Pas d'erreur technique
		}
		numan_log.Error(ctx).Err(err).Msg("Erreur SQL inattendue lors de la vérification de l'enregistrement")
		return auth_models.UserPayload{}, numan_error.NewInternal()
	}

	// 🕵️ DEBUG : On affiche ce qu'on a scanné
	fmt.Printf("🐘 POSTGRES SUCCÈS : ID=%d, Email='%s', Pass='%s'\n", res.ID, res.Email, res.PasswordHash)

	// ... (Le reste de la conversion date/nulls inchangé) ...
	if birthdateRaw.Valid {
		t, errTime := time.Parse(time.RFC3339, birthdateRaw.String)
		if errTime == nil {
			res.Birthdate = domain.TimeToMillis(t)
		} else {
			t2, errTime2 := time.Parse("02012006", birthdateRaw.String)
			if errTime2 == nil {
				res.Birthdate = domain.TimeToMillis(t2)
			}
		}
	}
	if profilePicID.Valid {
		res.ProfilePictureID = profilePicID.Int64
	}
	if banReason.Valid {
		res.BanReason = banReason.String
	}
	if banExpiresAt.Valid {
		res.BanExpiresAt = domain.TimeToMillis(banExpiresAt.Time)
	}
	if firstName.Valid {
		res.FirstName = firstName.String
	}
	if lastName.Valid {
		res.LastName = lastName.String
	}
	if bio.Valid {
		res.Bio = bio.String
	}
	if location.Valid {
		res.Location = location.String
	}
	if school.Valid {
		res.School = school.String
	}
	if work.Valid {
		res.Work = work.String
	}

	// Conversion finale des timestamps
	res.CreatedAt = domain.TimeToMillis(createdAt)
	res.UpdatedAt = domain.TimeToMillis(updatedAt)

	return res, nil
}
