package two_factor_models

type DeletePasskeyInput struct {
	PasskeyID int64 `json:"passkey_id" binding:"required"`
}
