package community_recommendation_models

// CommunityRecommendationItem représente une communauté publique recommandée.
// Intentionnellement sans champ is_joined (responsabilité déléguée au client Flutter).
// PinSlot vaut 0 (omis en JSON) lorsqu'elle n'est pas épinglée, et 1..100 sinon.
type CommunityRecommendationItem struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	AvatarMediaID int64  `json:"avatar_media_id"`
	Lang          string `json:"lang"`
	Mode          string `json:"mode"`
	MemberCount   int    `json:"member_count"`
	IsPartner     bool   `json:"is_partner"`
	IsPinned      bool   `json:"is_pinned"`
	PinSlot       int    `json:"pin_slot,omitempty"`
	FriendsCount  int    `json:"friends_count"`
}
