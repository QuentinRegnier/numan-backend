package two_factor_models

type GetPasskeysOutput struct {
	Passkeys []PasskeyView `json:"passkeys"`
}
