package community_recommendation_models

// PinMutationResult reflète le retour atomique de messaging.func_set_community_pin.
// OldPinSlot vaut 0 si la communauté n'était pas épinglée auparavant.
// DisplacedConversationID vaut 0 si aucune autre communauté n'a été déplacée.
type PinMutationResult struct {
	StatusCode              string
	ConvLang                string
	OldPinSlot              int
	DisplacedConversationID int64
}
