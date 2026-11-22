package community_recommendation_models

// UnpinMutationResult reflète le retour atomique de messaging.func_unset_community_pin.
// OldPinSlot vaut 0 si la communauté n'était déjà pas épinglée.
type UnpinMutationResult struct {
	StatusCode string
	ConvLang   string
	OldPinSlot int
}
