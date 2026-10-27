package moderation_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/models/report_models"

// GetModerationQueueInput valide la pagination et le filtrage du back-office de modération.
type GetModerationQueueInput struct {
	Limit            int64  `form:"limit,default=50" json:"limit" binding:"omitempty,min=1,max=100"`
	Offset           int64  `form:"offset,default=0" json:"offset" binding:"omitempty,min=0"`
	QueueType        string `form:"queue_type" json:"queue_type,omitempty" binding:"omitempty,oneof=active peer_review final_vote admin"`
	OriginFilter     *int   `form:"origin_filter" json:"origin_filter,omitempty" binding:"omitempty,oneof=0 1 2 3"`             // 0: User, 1: Auto_AI, 2: Hybrid, 3: System
	TargetTypeFilter *int   `form:"target_type_filter" json:"target_type_filter,omitempty" binding:"omitempty,oneof=0 1 2 3 5"` // 0: Post, 1: Comment, 2: Conv, 3: Message, 5: User
}

type ModerationQueueStats struct {
	TotalQuarantinedMedia int `json:"total_quarantined_media"`
	TotalPendingReports   int `json:"total_pending_reports"`
	AutoAIReportsCount    int `json:"auto_ai_reports_count"`
	UserReportsCount      int `json:"user_reports_count"`
	HybridReportsCount    int `json:"hybrid_reports_count"`
	SystemReportsCount    int `json:"system_reports_count"`
}

type GetModerationQueueOutput struct {
	Stats            ModerationQueueStats          `json:"stats"`
	QuarantinedMedia []QuarantinedMediaItem        `json:"quarantined_media"`
	PendingReports   []report_models.ReportPayload `json:"pending_reports"`
	QueueTickets     []ModerationTicketPublicView  `json:"queue_tickets,omitempty"`
}
