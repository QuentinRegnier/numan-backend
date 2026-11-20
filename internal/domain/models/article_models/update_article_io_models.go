package article_models

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// UpdateArticleInput représente la charge utile de mise à jour partielle ou complète d'un article (Admin).
type UpdateArticleInput struct {
	ArticleID          int64    `json:"article_id"`
	ID                 int64    `json:"id,omitempty"`
	Title              *string  `json:"title,omitempty"`
	Summary            *string  `json:"summary,omitempty"`
	CoverMediaID       *int64   `json:"cover_media_id,omitempty"`
	MarkdownMediaID    *int64   `json:"markdown_media_id,omitempty"`
	AttachmentMediaIDs *[]int64 `json:"attachment_media_ids,omitempty"`
	PartnerURL         *string  `json:"partner_url,omitempty"`
	Visibility         *int     `json:"visibility,omitempty"`
	ExpiresAt          *int64   `json:"expires_at,omitempty"`
	ClearExpiresAt     bool     `json:"clear_expires_at,omitempty"`
	ExpectedUpdatedAt  *int64   `json:"expected_updated_at,omitempty"`
}

// ResolvedID retourne l'identifiant de l'article (`article_id` ou alias `id`).
func (in UpdateArticleInput) ResolvedID() int64 {
	if in.ArticleID > 0 {
		return in.ArticleID
	}
	return in.ID
}

// ValidateAndNormalize valide les champs modifiés dans la requête de mise à jour.
func (in *UpdateArticleInput) ValidateAndNormalize() error {
	if in.ResolvedID() <= 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"L'identifiant de l'article (article_id) est requis et doit être strictement positif.",
			fmt.Errorf("invalid article_id: %d", in.ResolvedID()),
		)
	}

	if in.Title != nil {
		trimmed := strings.TrimSpace(*in.Title)
		l := utf8.RuneCountInString(trimmed)
		if l == 0 || l > variables.MaxArticleTitleLength {
			return numan_error.NewBadRequest(
				numan_error.CodeInvalidPayload,
				fmt.Sprintf("Le titre de l'article ne peut pas être vide et ne doit pas dépasser %d caractères.", variables.MaxArticleTitleLength),
				fmt.Errorf("invalid title length: %d", l),
			)
		}
		*in.Title = trimmed
	}

	if in.Summary != nil {
		trimmed := strings.TrimSpace(*in.Summary)
		l := utf8.RuneCountInString(trimmed)
		if l > variables.MaxArticleSummaryLength {
			return numan_error.NewBadRequest(
				numan_error.CodeInvalidPayload,
				fmt.Sprintf("Le résumé de l'article ne doit pas dépasser %d caractères.", variables.MaxArticleSummaryLength),
				fmt.Errorf("invalid summary length: %d", l),
			)
		}
		*in.Summary = trimmed
	}

	if in.CoverMediaID != nil && *in.CoverMediaID < 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"L'identifiant du média de couverture ne peut pas être négatif.",
			fmt.Errorf("negative cover_media_id: %d", *in.CoverMediaID),
		)
	}

	if in.MarkdownMediaID != nil && *in.MarkdownMediaID <= 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"L'identifiant du fichier Markdown (markdown_media_id) doit être strictement positif.",
			fmt.Errorf("invalid markdown_media_id: %d", *in.MarkdownMediaID),
		)
	}

	if in.AttachmentMediaIDs != nil {
		dedup, errAtt := DeduplicateAndValidateAttachmentIDs(*in.AttachmentMediaIDs)
		if errAtt != nil {
			return errAtt
		}
		*in.AttachmentMediaIDs = dedup
	}

	if in.PartnerURL != nil {
		normURL, errURL := NormalizeAndValidatePartnerURL(*in.PartnerURL)
		if errURL != nil {
			return errURL
		}
		*in.PartnerURL = normURL
	}

	if in.Visibility != nil {
		v := *in.Visibility
		if v != variables.ArticleVisibilityPublished && v != variables.ArticleVisibilityDraft {
			return numan_error.NewBadRequest(
				numan_error.CodeInvalidPayload,
				"La visibilité de l'article doit être 0 (publié) ou 1 (brouillon).",
				fmt.Errorf("invalid visibility in update: %d", v),
			)
		}
	}

	if in.ExpiresAt != nil && *in.ExpiresAt < 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"La date d'expiration (expires_at) ne peut pas être négative.",
			fmt.Errorf("negative expires_at: %d", *in.ExpiresAt),
		)
	}

	if in.ExpectedUpdatedAt != nil && *in.ExpectedUpdatedAt <= 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"L'horodatage de contrôle de concurrence (expected_updated_at) doit être strictement positif.",
			fmt.Errorf("invalid expected_updated_at: %d", *in.ExpectedUpdatedAt),
		)
	}

	return nil
}

// UpdateArticleOutput représente la réponse après mise à jour d'un article.
type UpdateArticleOutput struct {
	Article            ArticleDetailView `json:"article"`
	DeactivatedMediaID []int64           `json:"deactivated_media_ids,omitempty"`
}
