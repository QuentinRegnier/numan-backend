package post_models

type CreatePostInput struct {
	Content     string    `json:"content" binding:"max=2200"`
	Hashtags    []string  `json:"hashtags" binding:"max=10,dive,alphanum,max=20"`
	Identifiers []int64   `json:"identifiers" binding:"max=10"`
	MediaIDs    []int64   `json:"media_ids" binding:"max=4"`                                      // Maximum 4 médias par post
	IAScores    []float64 `json:"ia_scores,omitempty" binding:"omitempty,max=4,dive,min=0,max=1"` // Scores NSFWJS Porn [0.0, 1.0] des images
	Location    string    `json:"location" binding:"max=100"`
	Visibility  int       `json:"visibility" binding:"oneof=0 1 2"` // 0: Public, 1: Abonnés, 2: Amis (Mode 3 Privé supprimé)
}

type CreatePostResponse struct {
	PostID             int64  `json:"post_id"`
	Visibility         int    `json:"visibility"`                    // 3 si placé en quarantaine (Shadowban), sinon 0/1/2
	RequiresModeration bool   `json:"requires_moderation,omitempty"` // true si TrustScore < 0.40
	Message            string `json:"message,omitempty"`
}
