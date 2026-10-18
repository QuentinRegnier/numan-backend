package postgres

import (
	"context"
	"database/sql"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

func FuncLoadUser(ctx context.Context, ID int64, Username string, Email string, Phone string) (auth_models.UserPayload, error) {
	args := make([]any, 4)
	if ID != -1 {
		args[0] = ID
	}
	if Username != "" {
		args[1] = Username
	}
	if Email != "" {
		args[2] = Email
	}
	if Phone != "" {
		args[3] = Phone
	}

	sqlStatement := `SELECT * FROM auth.func_load_user($1, $2, $3, $4)`
	rows, err := postgres.PostgresDB.QueryContext(ctx, sqlStatement, args...)
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("Erreur SQL inattendue lors du chargement de l'utilisateur")
		return auth_models.UserPayload{}, numan_error.NewInternal()
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return auth_models.UserPayload{}, nil
	}

	var res auth_models.UserPayload
	var birthdateRaw, banReason, firstName, lastName, bio, location, school, work, totpSecret sql.NullString
	var profilePicID sql.NullInt64
	var banExpiresAt sql.NullTime
	var createdAt, updatedAt time.Time

	dest := []any{
		&res.ID, &res.Username, &res.Email, &res.EmailVerified,
		&res.Phone, &res.PhoneVerified, &res.PasswordHash,
		&firstName, &lastName, &birthdateRaw, &res.Sex,
		&bio, &profilePicID, &res.Grade, &location,
		&school, &work, &res.Badges, &res.Desactivated,
		&res.Banned, &banReason, &banExpiresAt,
		&createdAt, &updatedAt, &res.TwoFactorEnabled, &res.TwoFactorMethod, &totpSecret,
	}

	if err := rows.Scan(dest...); err != nil {
		numan_log.Error(ctx).Err(err).Msg("Erreur lors du Scan de auth.func_load_user")
		return auth_models.UserPayload{}, numan_error.NewInternal()
	}

	if birthdateRaw.Valid {
		if t, errTime := time.Parse(time.RFC3339, birthdateRaw.String); errTime == nil {
			res.Birthdate = domain.TimeToMillis(t)
		} else if t2, errTime2 := time.Parse("02012006", birthdateRaw.String); errTime2 == nil {
			res.Birthdate = domain.TimeToMillis(t2)
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
	if totpSecret.Valid {
		res.TOTPSecret = totpSecret.String
	}
	res.CreatedAt = domain.TimeToMillis(createdAt)
	res.UpdatedAt = domain.TimeToMillis(updatedAt)

	return res, nil
}
