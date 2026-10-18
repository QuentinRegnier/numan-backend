package variables

import "time"

const (
	TwoFactorMethodNone    = 0
	TwoFactorMethodEmail   = 1
	TwoFactorMethodTOTP    = 2
	TwoFactorMethodPasskey = 3

	// Bitmask stocké dans auth.users.two_factor_method pour cumuler plusieurs méthodes actives
	TwoFactorBitEmail   = 1 << 0 // 1
	TwoFactorBitTOTP    = 1 << 1 // 2
	TwoFactorBitPasskey = 1 << 2 // 4
)

var (
	TwoFactorPreAuthTTL  = 5 * time.Minute  // Durée de vie d'un challenge 2FA en RAM L1
	TwoFactorMaxAttempts = 5                // Nombre max d'essais avant destruction du challenge
	WebAuthnTimeoutMs    = 90 * time.Second // Timeout client WebAuthn (90s)
)
