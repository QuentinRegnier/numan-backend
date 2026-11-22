package community_recommendation_models

// LangChangeMutationResult reflète le retour atomique de messaging.func_update_community_lang.
// OldPinSlot vaut 0 si aucun slot d'épinglage n'était actif avant le changement de langue.
type LangChangeMutationResult struct {
	StatusCode string
	OldLang    string
	NewLang    string
	OldPinSlot int
}
