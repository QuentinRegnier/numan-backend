package community_recommendation_models

// PartnerMutationResult reflète le retour atomique de messaging.func_set_community_partner.
type PartnerMutationResult struct {
	StatusCode string
	ConvLang   string
	IsPartner  bool
}
