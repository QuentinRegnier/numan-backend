package article_models

import (
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ArticlePayload représente le modèle de persistance L1 (Redis HASH MsgPack), L2 (MongoDB BSON) et L3 (PostgreSQL).
type ArticlePayload struct {
	ID                 int64   `json:"id"                   bson:"id"                   msgpack:"id"`
	Title              string  `json:"title"                bson:"title"                msgpack:"title"`
	Summary            string  `json:"summary"              bson:"summary"              msgpack:"summary"`
	CoverMediaID       int64   `json:"cover_media_id"       bson:"cover_media_id"       msgpack:"cover_media_id"`
	MarkdownMediaID    int64   `json:"markdown_media_id"    bson:"markdown_media_id"    msgpack:"markdown_media_id"`
	AttachmentMediaIDs []int64 `json:"attachment_media_ids" bson:"attachment_media_ids" msgpack:"attachment_media_ids"`
	PartnerURL         string  `json:"partner_url"          bson:"partner_url"          msgpack:"partner_url"`
	Visibility         int     `json:"visibility"           bson:"visibility"           msgpack:"visibility"`
	ExpiresAt          *int64  `json:"expires_at"           bson:"expires_at"           msgpack:"expires_at"`
	CreatedAt          int64   `json:"created_at"           bson:"created_at"           msgpack:"created_at"`
	UpdatedAt          int64   `json:"updated_at"           bson:"updated_at"           msgpack:"updated_at"`
}

// ValidateAndNormalize nettoie, déduplique et valide les invariants métiers de l'article.
func (a *ArticlePayload) ValidateAndNormalize() error {
	if a.ID <= 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"L'identifiant de l'article est invalide.",
			fmt.Errorf("invalid article ID: %d", a.ID),
		)
	}

	a.Title = strings.TrimSpace(a.Title)
	titleLen := utf8.RuneCountInString(a.Title)
	if titleLen == 0 || titleLen > variables.MaxArticleTitleLength {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("Le titre de l'article est requis et ne doit pas dépasser %d caractères.", variables.MaxArticleTitleLength),
			fmt.Errorf("invalid article title length: %d", titleLen),
		)
	}

	a.Summary = strings.TrimSpace(a.Summary)
	summaryLen := utf8.RuneCountInString(a.Summary)
	if summaryLen > variables.MaxArticleSummaryLength {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("Le résumé de l'article ne doit pas dépasser %d caractères.", variables.MaxArticleSummaryLength),
			fmt.Errorf("invalid article summary length: %d", summaryLen),
		)
	}

	if a.CoverMediaID < 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"L'identifiant du média de couverture ne peut pas être négatif.",
			fmt.Errorf("negative cover_media_id: %d", a.CoverMediaID),
		)
	}

	if a.MarkdownMediaID < 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"L'identifiant du document Markdown ne peut pas être négatif.",
			fmt.Errorf("negative markdown_media_id: %d", a.MarkdownMediaID),
		)
	}

	if a.Visibility != variables.ArticleVisibilitySoftDeleted &&
		a.Visibility != variables.ArticleVisibilityPublished &&
		a.Visibility != variables.ArticleVisibilityDraft {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"La visibilité de l'article est invalide (0 = publié, 1 = brouillon, -1 = supprimé).",
			fmt.Errorf("invalid article visibility: %d", a.Visibility),
		)
	}

	normalizedPartnerURL, errURL := NormalizeAndValidatePartnerURL(a.PartnerURL)
	if errURL != nil {
		return errURL
	}
	a.PartnerURL = normalizedPartnerURL

	dedupAttachments, errAtt := DeduplicateAndValidateAttachmentIDs(a.AttachmentMediaIDs)
	if errAtt != nil {
		return errAtt
	}
	a.AttachmentMediaIDs = dedupAttachments

	if a.ExpiresAt != nil {
		if *a.ExpiresAt <= 0 {
			a.ExpiresAt = nil
		}
	}

	if a.CreatedAt <= 0 || a.UpdatedAt <= 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Les horodatages de l'article doivent être strictement positifs.",
			fmt.Errorf("invalid timestamps: created_at=%d, updated_at=%d", a.CreatedAt, a.UpdatedAt),
		)
	}

	return nil
}

// Normalize garantit des valeurs par défaut déterministes (ex: slice non-nil pour attachment_media_ids).
func (a *ArticlePayload) Normalize() {
	if a.AttachmentMediaIDs == nil {
		a.AttachmentMediaIDs = []int64{}
	}
	if a.ExpiresAt != nil && *a.ExpiresAt <= 0 {
		a.ExpiresAt = nil
	}
}

// IsExpired indique si l'article a dépassé sa date d'expiration par rapport à nowMs.
func (a *ArticlePayload) IsExpired(nowMs int64) bool {
	return a.ExpiresAt != nil && *a.ExpiresAt > 0 && *a.ExpiresAt <= nowMs
}

// ReferencedMediaIDs retourne la liste dédupliquée de tous les identifiants de médias (> 0) référencés par l'article.
func (a *ArticlePayload) ReferencedMediaIDs() []int64 {
	seen := make(map[int64]struct{}, len(a.AttachmentMediaIDs)+2)
	out := make([]int64, 0, len(a.AttachmentMediaIDs)+2)

	add := func(id int64) {
		if id <= 0 {
			return
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}

	add(a.CoverMediaID)
	add(a.MarkdownMediaID)
	for _, id := range a.AttachmentMediaIDs {
		add(id)
	}
	return out
}

// NormalizeAndValidatePartnerURL valide qu'une URL partenaire est vide ou une URL HTTP/HTTPS absolue sûre.
func NormalizeAndValidatePartnerURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}

	if len(trimmed) > variables.MaxArticlePartnerURLLength {
		return "", numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("L'URL partenaire ne doit pas dépasser %d caractères.", variables.MaxArticlePartnerURLLength),
			fmt.Errorf("partner_url length %d exceeds max %d", len(trimmed), variables.MaxArticlePartnerURLLength),
		)
	}

	if strings.ContainsAny(trimmed, " \t\r\n\x00") {
		return "", numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"L'URL partenaire contient des caractères invalides.",
			fmt.Errorf("partner_url contains whitespace or control character"),
		)
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"L'URL partenaire est malformée.",
			err,
		)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"L'URL partenaire doit utiliser exclusivement le protocole HTTP ou HTTPS.",
			fmt.Errorf("forbidden partner_url scheme: %q", parsed.Scheme),
		)
	}

	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return "", numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"L'URL partenaire doit comporter un nom d'hôte valide.",
			fmt.Errorf("empty partner_url host"),
		)
	}

	parsed.Scheme = scheme
	return parsed.String(), nil
}

// DeduplicateAndValidateAttachmentIDs vérifie que tous les IDs sont > 0, les déduplique dans l'ordre et vérifie la limite max.
func DeduplicateAndValidateAttachmentIDs(ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return []int64{}, nil
	}

	seen := make(map[int64]struct{}, len(ids))
	dedup := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, numan_error.NewBadRequest(
				numan_error.CodeInvalidPayload,
				"Tous les identifiants de pièces jointes doivent être strictement positifs.",
				fmt.Errorf("invalid attachment_media_id: %d", id),
			)
		}
		if _, exists := seen[id]; !exists {
			seen[id] = struct{}{}
			dedup = append(dedup, id)
		}
	}

	if len(dedup) > variables.MaxArticleAttachments {
		return nil, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("Un article ne peut pas contenir plus de %d pièces jointes.", variables.MaxArticleAttachments),
			fmt.Errorf("too many attachments: %d > %d", len(dedup), variables.MaxArticleAttachments),
		)
	}

	return dedup, nil
}
