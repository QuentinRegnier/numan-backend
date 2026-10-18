package two_factor_models

type DisableTwoFactorInput struct {
	PasswordHash string `json:"password_hash" binding:"required"`
	Method       int    `json:"method" binding:"omitempty,oneof=0 1 2 3"` // 0 = Désactiver toute la 2FA, 1 = Email, 2 = TOTP, 3 = Passkey
}
