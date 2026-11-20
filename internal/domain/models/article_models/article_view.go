package article_models

// ArticleAssetView représente un média public rattaché à un article (image, GIF/WebP animé ou fichier Markdown).
type ArticleAssetView struct {
	MediaID     int64  `json:"media_id"`
	StoragePath string `json:"storage_path"`
	URL         string `json:"url"`
}

// ArticleListItemView représente la vue ultra-légère d'un article retournée dans les listes paginées (~1-2 Ko).
// Ne contient ni le contenu Markdown, ni l'URL du Markdown, ni les pièces jointes.
type ArticleListItemView struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	CoverURL   string `json:"cover_url,omitempty"`
	PartnerURL string `json:"partner_url,omitempty"`
	Visibility int    `json:"visibility"`
	ExpiresAt  *int64 `json:"expires_at,omitempty"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

// ArticleDetailView représente la vue complète des métadonnées d'un article.
// Le client Flutter télécharge directement le document Markdown via `markdown_url` et les médias via `attachments`.
type ArticleDetailView struct {
	ID              int64              `json:"id"`
	Title           string             `json:"title"`
	Summary         string             `json:"summary"`
	CoverMediaID    int64              `json:"cover_media_id,omitempty"`
	CoverURL        string             `json:"cover_url,omitempty"`
	MarkdownMediaID int64              `json:"markdown_media_id"`
	MarkdownURL     string             `json:"markdown_url"`
	Attachments     []ArticleAssetView `json:"attachments"`
	PartnerURL      string             `json:"partner_url,omitempty"`
	Visibility      int                `json:"visibility"`
	ExpiresAt       *int64             `json:"expires_at,omitempty"`
	CreatedAt       int64              `json:"created_at"`
	UpdatedAt       int64              `json:"updated_at"`
}
