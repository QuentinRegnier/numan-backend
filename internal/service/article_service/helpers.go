package article_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
)

// BuildArticleDetailView hydrate un ArticlePayload en ArticleDetailView avec résolution en batch des URLs publiques S3.
// Ne télécharge ni ne retourne jamais le contenu Markdown brut.
func BuildArticleDetailView(ctx context.Context, payload article_models.ArticlePayload) article_models.ArticleDetailView {
	payload.Normalize()
	mediaMap := media_service.ResolvePublicMediaMapCascade(ctx, payload.ReferencedMediaIDs())

	var coverURL string
	if payload.CoverMediaID > 0 {
		if m, ok := mediaMap[payload.CoverMediaID]; ok {
			coverURL = media_service.GeneratePublicMediaURL(m.StoragePath)
		}
	}

	var markdownURL string
	if payload.MarkdownMediaID > 0 {
		if m, ok := mediaMap[payload.MarkdownMediaID]; ok {
			markdownURL = media_service.GeneratePublicMediaURL(m.StoragePath)
		}
	}

	attachments := make([]article_models.ArticleAssetView, 0, len(payload.AttachmentMediaIDs))
	for _, attID := range payload.AttachmentMediaIDs {
		if attID <= 0 {
			continue
		}
		var storagePath, attURL string
		if m, ok := mediaMap[attID]; ok {
			storagePath = m.StoragePath
			attURL = media_service.GeneratePublicMediaURL(m.StoragePath)
		}
		attachments = append(attachments, article_models.ArticleAssetView{
			MediaID:     attID,
			StoragePath: storagePath,
			URL:         attURL,
		})
	}

	return article_models.ArticleDetailView{
		ID:              payload.ID,
		Title:           payload.Title,
		Summary:         payload.Summary,
		CoverMediaID:    payload.CoverMediaID,
		CoverURL:        coverURL,
		MarkdownMediaID: payload.MarkdownMediaID,
		MarkdownURL:     markdownURL,
		Attachments:     attachments,
		PartnerURL:      payload.PartnerURL,
		Visibility:      payload.Visibility,
		ExpiresAt:       payload.ExpiresAt,
		CreatedAt:       payload.CreatedAt,
		UpdatedAt:       payload.UpdatedAt,
	}
}

// BuildArticleListItemViews hydrate un lot d'articles en vues légères de liste avec un seul appel batch MGET pour les couvertures.
// N'inclut jamais le contenu Markdown, ni l'URL Markdown, ni la liste des pièces jointes.
func BuildArticleListItemViews(ctx context.Context, payloads []article_models.ArticlePayload) []article_models.ArticleListItemView {
	if len(payloads) == 0 {
		return []article_models.ArticleListItemView{}
	}

	coverIDs := make([]int64, 0, len(payloads))
	for _, p := range payloads {
		if p.CoverMediaID > 0 {
			coverIDs = append(coverIDs, p.CoverMediaID)
		}
	}

	mediaMap := media_service.ResolvePublicMediaMapCascade(ctx, coverIDs)

	items := make([]article_models.ArticleListItemView, 0, len(payloads))
	for _, p := range payloads {
		var coverURL string
		if p.CoverMediaID > 0 {
			if m, ok := mediaMap[p.CoverMediaID]; ok {
				coverURL = media_service.GeneratePublicMediaURL(m.StoragePath)
			}
		}
		items = append(items, article_models.ArticleListItemView{
			ID:         p.ID,
			Title:      p.Title,
			Summary:    p.Summary,
			CoverURL:   coverURL,
			PartnerURL: p.PartnerURL,
			Visibility: p.Visibility,
			ExpiresAt:  p.ExpiresAt,
			CreatedAt:  p.CreatedAt,
			UpdatedAt:  p.UpdatedAt,
		})
	}
	return items
}

// diffRemovedMediaIDs retourne les identifiants de médias présents dans oldIDs mais absents de newIDs.
func diffRemovedMediaIDs(oldIDs, newIDs []int64) []int64 {
	if len(oldIDs) == 0 {
		return nil
	}
	retained := make(map[int64]struct{}, len(newIDs))
	for _, id := range newIDs {
		if id > 0 {
			retained[id] = struct{}{}
		}
	}

	var removed []int64
	seen := make(map[int64]struct{}, len(oldIDs))
	for _, id := range oldIDs {
		if id <= 0 {
			continue
		}
		if _, already := seen[id]; already {
			continue
		}
		seen[id] = struct{}{}
		if _, kept := retained[id]; !kept {
			removed = append(removed, id)
		}
	}
	return removed
}
