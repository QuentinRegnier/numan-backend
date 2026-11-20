package article_models

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// CreateArticleInput représente la charge utile de création d'un article par un Administrateur.
type CreateArticleInput struct {
	ID                 int64   `json:"id,omitempty"`
	Title              string  `json:"title"`
	Summary            string  `json:"summary"`
	CoverMediaID       int64   `json:"cover_media_id"`
	MarkdownMediaID    int64   `json:"markdown_media_id"`
	AttachmentMediaIDs []int64 `json:"attachment_media_ids"`
	PartnerURL         string  `json:"partner_url"`
	Visibility         *int    `json:"visibility,omitempty"`
	ExpiresAt          *int64  `json:"expires_at,omitempty"`
}

// ValidateAndNormalize valide et nettoie les champs de création d'un article.
func (in *CreateArticleInput) ValidateAndNormalize(nowMs int64) error {
	if in.ID < 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"L'identifiant de l'article ne peut pas être négatif.",
			fmt.Errorf("negative id: %d", in.ID),
		)
	}

	in.Title = strings.TrimSpace(in.Title)
	titleLen := utf8.RuneCountInString(in.Title)
	if titleLen == 0 || titleLen > variables.MaxArticleTitleLength {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("Le titre de l'article est requis et ne doit pas dépasser %d caractères.", variables.MaxArticleTitleLength),
			fmt.Errorf("invalid title length: %d", titleLen),
		)
	}

	in.Summary = strings.TrimSpace(in.Summary)
	summaryLen := utf8.RuneCountInString(in.Summary)
	if summaryLen > variables.MaxArticleSummaryLength {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("Le résumé de l'article ne doit pas dépasser %d caractères.", variables.MaxArticleSummaryLength),
			fmt.Errorf("invalid summary length: %d", summaryLen),
		)
	}

	if in.CoverMediaID < 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"L'identifiant du média de couverture est invalide.",
			fmt.Errorf("negative cover_media_id: %d", in.CoverMediaID),
		)
	}

	if in.MarkdownMediaID <= 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"L'identifiant du fichier Markdown (markdown_media_id) est obligatoire.",
			fmt.Errorf("missing or invalid markdown_media_id: %d", in.MarkdownMediaID),
		)
	}

	dedupAtt, errAtt := DeduplicateAndValidateAttachmentIDs(in.AttachmentMediaIDs)
	if errAtt != nil {
		return errAtt
	}
	in.AttachmentMediaIDs = dedupAtt

	partnerURL, errURL := NormalizeAndValidatePartnerURL(in.PartnerURL)
	if errURL != nil {
		return errURL
	}
	in.PartnerURL = partnerURL

	vis := variables.ArticleVisibilityPublished
	if in.Visibility != nil {
		vis = *in.Visibility
	}
	if vis != variables.ArticleVisibilityPublished && vis != variables.ArticleVisibilityDraft {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"La visibilité initiale doit être 0 (publié) ou 1 (brouillon).",
			fmt.Errorf("invalid initial visibility: %d", vis),
		)
	}
	in.Visibility = &vis

	if in.ExpiresAt != nil {
		if *in.ExpiresAt <= 0 {
			in.ExpiresAt = nil
		} else if nowMs > 0 && *in.ExpiresAt <= nowMs {
			return numan_error.NewBadRequest(
				numan_error.CodeInvalidPayload,
				"La date d'expiration (expires_at) doit être située dans le futur.",
				fmt.Errorf("expires_at %d <= now %d", *in.ExpiresAt, nowMs),
			)
		}
	}

	return nil
}

// CreateArticleOutput représente la réponse après création d'un article.
type CreateArticleOutput struct {
	Article ArticleDetailView `json:"article"`
}
