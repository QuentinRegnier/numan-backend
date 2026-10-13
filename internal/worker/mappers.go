package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/comment_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/relation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/report_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/saved_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/lib/pq"
)

// ============================================================================
// CONTRAT D'INTERFACE : ENTITY MAPPER
// ============================================================================

// entityMapper définit le contrat obligatoire pour transformer un événement
// générique (Payload Go) en une ligne compatible avec le moteur COPY IN de PostgreSQL (L3).
type entityMapper interface {
	TableName() string                        // Nom complet de la table SQL (ex: "auth.users")
	Columns() []string                        // Liste ordonnée des colonnes ciblées
	ToRow(data any) ([]any, error)            // Transforme le struct Go en tableau de valeurs SQL
	BuildUpdateQuery(tempTable string) string // Génère la requête de fusion (MERGE/UPDATE)
}

// getMapper agit comme une usine (Factory Pattern).
// Il retourne le mapper spécifique correspondant au type d'entité asynchrone traité par Redis.
func getMapper(entity redis.EntityType) entityMapper {
	switch entity {
	case redis.EntityUser:
		return &userMapper{}
	case redis.EntitySession:
		return &sessionMapper{}
	case redis.EntityUserSettings:
		return &userSettingsMapper{}
	case redis.EntityRelation:
		return &relationMapper{}
	case redis.EntityPost:
		return &postMapper{}
	case redis.EntityComment:
		return &commentMapper{}
	case redis.EntityMedia:
		return &mediaMapper{}
	case redis.EntityLike:
		return &likeMapper{}
	case redis.EntitySaved:
		return &savedMapper{}
	case redis.EntityMessage:
		return &messageMapper{}
	case redis.EntityMessageReaction:
		return &messageReactionMapper{}
	case redis.EntityConversation:
		return &conversationMapper{}
	case redis.EntityMembers:
		return &memberMapper{}
	case redis.EntityReport:
		return &reportMapper{}
	default:
		return nil
	}
}

// ############################################################################
// # SCHÉMA : AUTH (Utilisateurs, Sessions, Relations, Paramètres)
// ############################################################################

// ============================================================================
// MAPPER : UTILISATEURS (auth.users)
// ============================================================================
type userMapper struct{}

func (m *userMapper) TableName() string { return "auth.users" }

func (m *userMapper) Columns() []string {
	return []string{
		"id", "username", "email", "email_verified", "phone", "phone_verified",
		"password_hash", "first_name", "last_name", "birthdate", "sex", "bio",
		"profile_picture_id", "grade", "location", "school", "work", "badges",
		"desactivated", "banned", "ban_reason", "ban_expires_at",
		"created_at", "updated_at",
	}
}

func (m *userMapper) ToRow(data any) ([]any, error) {
	// ── ÉTAPE 1 : DÉSÉRIALISATION ───────────────────────────────────────────
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("UserMapper : Échec de la sérialisation du payload générique")
		return nil, numan_error.NewInternal()
	}

	var u auth_models.UserPayload
	if err := json.Unmarshal(jsonBytes, &u); err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("UserMapper : Échec de la désérialisation vers le modèle métier")
		return nil, numan_error.NewInternal()
	}

	// ── ÉTAPE 2 : TRANSLATION DES VALEURS NULLES (SQL NULL) ─────────────────
	var phoneDB any = u.Phone
	if u.Phone == "" {
		phoneDB = nil
	}

	var birthdateDB any = u.Birthdate
	if u.Birthdate == 0 {
		birthdateDB = nil
	}

	var bioDB any = u.Bio
	if u.Bio == "" {
		bioDB = nil
	}

	var ppDB any = u.ProfilePictureID
	if u.ProfilePictureID == 0 {
		ppDB = nil
	}

	var locDB any = u.Location
	if u.Location == "" {
		locDB = nil
	}

	var schoolDB any = u.School
	if u.School == "" {
		schoolDB = nil
	}

	var workDB any = u.Work
	if u.Work == "" {
		workDB = nil
	}

	var banReasonDB any = u.BanReason
	if u.BanReason == "" {
		banReasonDB = nil
	}

	var banExpiresDB any = u.BanExpiresAt
	if u.BanExpiresAt == 0 {
		banExpiresDB = nil
	}

	badgesJSON, _ := json.Marshal(u.Badges)
	var badgesDB any = string(badgesJSON)
	if u.Badges == nil || len(u.Badges) == 0 {
		badgesDB = "{}"
	}

	// ── ÉTAPE 3 : FORMATAGE DE LA LIGNE SQL ─────────────────────────────────
	return []any{
		u.ID, u.Username, u.Email, u.EmailVerified, phoneDB, u.PhoneVerified,
		u.PasswordHash, u.FirstName, u.LastName, birthdateDB, u.Sex, bioDB,
		ppDB, u.Grade, locDB, schoolDB, workDB, badgesDB,
		u.Desactivated, u.Banned, banReasonDB, banExpiresDB,
		u.CreatedAt, u.UpdatedAt,
	}, nil
}

func (m *userMapper) BuildUpdateQuery(tempTable string) string {
	return buildGenericUpdateQuery(m.TableName(), tempTable, m.Columns())
}

// ============================================================================
// MAPPER : PARAMÈTRES UTILISATEUR (auth.user_settings)
// ============================================================================
type userSettingsMapper struct{}

func (m *userSettingsMapper) TableName() string { return "auth.user_settings" }

func (m *userSettingsMapper) Columns() []string {
	return []string{
		"id", "user_id", "privacy", "notifications", "display_and_content",
		"telemetry_vector", "telemetry_tags", "telemetry_timestamp",
		"created_at", "updated_at",
	}
}

func (m *userSettingsMapper) ToRow(data any) ([]any, error) {
	// ── ÉTAPE 1 : DÉSÉRIALISATION ───────────────────────────────────────────
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("UserSettingsMapper : Échec de la sérialisation")
		return nil, numan_error.NewInternal()
	}

	var s user_settings_models.UserSettingsPayload
	if err := json.Unmarshal(jsonBytes, &s); err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("UserSettingsMapper : Échec de la désérialisation")
		return nil, numan_error.NewInternal()
	}

	// ── ÉTAPE 2 : SÉRIALISATION DES COLONNES JSONB ──────────────────────────
	privacyJSON, _ := json.Marshal(s.Privacy)
	notifJSON, _ := json.Marshal(s.Notifications)
	displayJSON, _ := json.Marshal(s.DisplayAndContent)

	// ── ÉTAPE 3 : TRANSLATION DES VALEURS NULLES (SQL NULL) ─────────────────
	var telVecDB any = pq.Array(s.TelemetryVector)
	if len(s.TelemetryVector) == 0 {
		telVecDB = nil
	}

	var telTagsDB any = pq.Array(s.TelemetryTags)
	if len(s.TelemetryTags) == 0 {
		telTagsDB = nil
	}

	var telTsDB any = s.TelemetryTimestamp
	if s.TelemetryTimestamp == 0 {
		telTsDB = nil
	}

	// ── ÉTAPE 4 : FORMATAGE DE LA LIGNE SQL ─────────────────────────────────
	return []any{
		s.ID, s.UserID, string(privacyJSON), string(notifJSON), string(displayJSON),
		telVecDB, telTagsDB, telTsDB, s.CreatedAt, s.UpdatedAt,
	}, nil
}

func (m *userSettingsMapper) BuildUpdateQuery(tempTable string) string {
	return buildGenericUpdateQuery(m.TableName(), tempTable, m.Columns())
}

// ============================================================================
// MAPPER : SESSIONS D'APPAREILS (auth.sessions)
// ============================================================================
type sessionMapper struct{}

func (m *sessionMapper) TableName() string { return "auth.sessions" }

func (m *sessionMapper) Columns() []string {
	return []string{
		"id", "user_id", "master_token", "firebase_installation_id", "device_info",
		"ip_history", "current_secret", "last_secret", "last_jwt",
		"tolerance_time", "created_at", "expires_at",
	}
}

func (m *sessionMapper) ToRow(data any) ([]any, error) {
	// ── ÉTAPE 1 : DÉSÉRIALISATION ───────────────────────────────────────────
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("SessionMapper : Échec de la sérialisation")
		return nil, numan_error.NewInternal()
	}

	var s auth_models.SessionsPayload
	if err := json.Unmarshal(jsonBytes, &s); err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("SessionMapper : Échec de la désérialisation")
		return nil, numan_error.NewInternal()
	}

	// ── ÉTAPE 2 : SÉRIALISATION DES COLONNES JSONB ET NULLS ─────────────────
	deviceInfoJSON, _ := json.Marshal(s.DeviceInfo)

	var devInfoDB any = string(deviceInfoJSON)
	if len(s.DeviceInfo) == 0 {
		devInfoDB = nil
	}

	var curSecDB any = s.CurrentSecret
	if s.CurrentSecret == "" {
		curSecDB = nil
	}

	var lastSecDB any = s.LastSecret
	if s.LastSecret == "" {
		lastSecDB = nil
	}

	var lastJwtDB any = s.LastJWT
	if s.LastJWT == "" {
		lastJwtDB = nil
	}

	var tolTimeDB any = s.ToleranceTime
	if s.ToleranceTime == 0 {
		tolTimeDB = nil
	}

	// ── ÉTAPE 3 : FORMATAGE DE LA LIGNE SQL ─────────────────────────────────
	return []any{
		s.ID, s.UserID, s.MasterToken, s.FirebaseInstallationID, devInfoDB,
		pq.Array(s.IPHistory), curSecDB, lastSecDB, lastJwtDB,
		tolTimeDB, s.CreatedAt, s.ExpiresAt,
	}, nil
}

func (m *sessionMapper) BuildUpdateQuery(tempTable string) string {
	return buildGenericUpdateQuery(m.TableName(), tempTable, m.Columns())
}

// ============================================================================
// MAPPER : RELATIONS SOCIALES (auth.relations)
// ============================================================================
type relationMapper struct{}

func (m *relationMapper) TableName() string { return "auth.relations" }

func (m *relationMapper) Columns() []string {
	return []string{"id", "primary_id", "secondary_id", "state", "created_at", "updated_at"}
}

func (m *relationMapper) ToRow(data any) ([]any, error) {
	// ── ÉTAPE 1 : DÉSÉRIALISATION ───────────────────────────────────────────
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("RelationMapper : Échec de la sérialisation")
		return nil, numan_error.NewInternal()
	}

	var r relation_models.RelationPayload
	if err := json.Unmarshal(jsonBytes, &r); err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("RelationMapper : Échec de la désérialisation")
		return nil, numan_error.NewInternal()
	}

	// ── ÉTAPE 2 : FORMATAGE DE LA LIGNE SQL ─────────────────────────────────
	return []any{r.ID, r.PrimaryID, r.SecondaryID, r.State, r.CreatedAt, r.UpdatedAt}, nil
}

func (m *relationMapper) BuildUpdateQuery(tempTable string) string {
	// Spécificité : L'Update se fait sur la clé composite (primary_id, secondary_id)
	return fmt.Sprintf(
		"UPDATE %s SET state = %s.state, updated_at = %s.updated_at FROM %s WHERE %s.primary_id = %s.primary_id AND %s.secondary_id = %s.secondary_id",
		m.TableName(), tempTable, tempTable, tempTable, m.TableName(), tempTable, m.TableName(), tempTable,
	)
}

// ############################################################################
// # SCHÉMA : CONTENT (Posts, Commentaires, Médias, Likes, Favoris)
// ############################################################################

// ============================================================================
// MAPPER : PUBLICATIONS (content.posts)
// ============================================================================
type postMapper struct{}

func (m *postMapper) TableName() string { return "content.posts" }

func (m *postMapper) Columns() []string {
	return []string{
		"id", "user_id", "content", "hashtags", "identifiers", "media_ids",
		"visibility", "priority_level", "location", "like_count",
		"comment_count", "view_count", "report_count", "has_media",
		"vector", "vector_version", "telemetry_dwell_sum", "telemetry_dwell_sq",
		"telemetry_clicks", "created_at", "updated_at",
	}
}

func (m *postMapper) ToRow(data any) ([]any, error) {
	// ── ÉTAPE 1 : DÉSÉRIALISATION ───────────────────────────────────────────
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("PostMapper : Échec de la sérialisation")
		return nil, numan_error.NewInternal()
	}

	var p post_models.PostPayload
	if err := json.Unmarshal(jsonBytes, &p); err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("PostMapper : Échec de la désérialisation")
		return nil, numan_error.NewInternal()
	}

	// ── ÉTAPE 2 : TRANSLATION DES VALEURS NULLES (SQL NULL) ─────────────────
	var contentDB any = p.Content
	if p.Content == "" {
		contentDB = nil
	}

	var locationDB any = p.Location
	if p.Location == "" {
		locationDB = nil
	}

	var vectorDB any = pq.Array(p.Vector)
	if len(p.Vector) == 0 {
		vectorDB = nil
	}

	// ── ÉTAPE 3 : FORMATAGE DE LA LIGNE SQL ─────────────────────────────────
	return []any{
		p.ID, p.UserID, contentDB, pq.Array(p.Hashtags), pq.Array(p.IndirectHashtags), pq.Array(p.Identifiers), pq.Array(p.MediaIDs),
		p.Visibility, p.PriorityLevel, locationDB, p.LikeCount, p.CommentCount,
		p.ViewCount, p.ReportCount, p.HasMedia, vectorDB, p.VectorVersion, p.TelemetryDwellSum,
		p.TelemetryDwellSq, p.TelemetryClicks, p.CreatedAt, p.UpdatedAt,
	}, nil
}

func (m *postMapper) BuildUpdateQuery(tempTable string) string {
	return buildGenericUpdateQuery(m.TableName(), tempTable, m.Columns())
}

// ============================================================================
// MAPPER : MÉDIAS (content.media)
// ============================================================================
type mediaMapper struct{}

func (m *mediaMapper) TableName() string { return "content.media" }

func (m *mediaMapper) Columns() []string {
	return []string{"id", "owner_id", "storage_path", "visibility", "created_at", "updated_at"}
}

func (m *mediaMapper) ToRow(data any) ([]any, error) {
	// ── ÉTAPE 1 : DÉSÉRIALISATION ───────────────────────────────────────────
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("MediaMapper : Échec de la sérialisation")
		return nil, numan_error.NewInternal()
	}

	var med media_models.MediaPayload
	if err := json.Unmarshal(jsonBytes, &med); err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("MediaMapper : Échec de la désérialisation")
		return nil, numan_error.NewInternal()
	}

	// ── ÉTAPE 2 : TRANSLATION DES VALEURS NULLES ────────────────────────────
	var storageDB any = med.StoragePath
	if med.StoragePath == "" {
		storageDB = nil
	}

	// ── ÉTAPE 3 : FORMATAGE DE LA LIGNE SQL ─────────────────────────────────
	return []any{
		med.ID, med.OwnerID, storageDB, med.Visibility, med.CreatedAt, med.UpdatedAt,
	}, nil
}

func (m *mediaMapper) BuildUpdateQuery(tempTable string) string {
	return buildGenericUpdateQuery(m.TableName(), tempTable, m.Columns())
}

// ============================================================================
// MAPPER : COMMENTAIRES (content.comments)
// ============================================================================
type commentMapper struct{}

func (m *commentMapper) TableName() string { return "content.comments" }

func (m *commentMapper) Columns() []string {
	return []string{"id", "post_id", "user_id", "content", "visibility", "like_count", "score", "created_at", "updated_at"}
}

func (m *commentMapper) ToRow(data any) ([]any, error) {
	// ── ÉTAPE 1 : DÉSÉRIALISATION ───────────────────────────────────────────
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("CommentMapper : Échec de la sérialisation")
		return nil, numan_error.NewInternal()
	}

	var c comment_models.CommentPayload
	if err := json.Unmarshal(jsonBytes, &c); err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("CommentMapper : Échec de la désérialisation")
		return nil, numan_error.NewInternal()
	}

	// ── ÉTAPE 2 : FORMATAGE DE LA LIGNE SQL ─────────────────────────────────
	return []any{c.ID, c.PostID, c.UserID, c.Content, c.Visibility, c.LikeCount, c.Score, c.CreatedAt, c.UpdatedAt}, nil
}

func (m *commentMapper) BuildUpdateQuery(t string) string {
	return buildGenericUpdateQuery(m.TableName(), t, m.Columns())
}

// ============================================================================
// MAPPER : LIKES (content.likes)
// ============================================================================
type likeMapper struct{}

// likeWorkerPayload définit la structure allégée attendue par le Worker pour l'entité Like.
type likeWorkerPayload struct {
	ID         int64  `json:"id"`
	TargetType int    `json:"target_type"`
	TargetID   int64  `json:"target_id"`
	UserID     int64  `json:"user_id"`
	CreatedAt  string `json:"created_at"`
}

func (m *likeMapper) TableName() string { return "content.likes" }

func (m *likeMapper) Columns() []string {
	return []string{"id", "target_type", "target_id", "user_id", "created_at"}
}

func (m *likeMapper) ToRow(data any) ([]any, error) {
	// ── ÉTAPE 1 : DÉSÉRIALISATION ───────────────────────────────────────────
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("LikeMapper : Échec de la sérialisation")
		return nil, numan_error.NewInternal()
	}

	var l likeWorkerPayload
	if err := json.Unmarshal(jsonBytes, &l); err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("LikeMapper : Échec de la désérialisation")
		return nil, numan_error.NewInternal()
	}

	// ── ÉTAPE 2 : FORMATAGE DE LA LIGNE SQL ─────────────────────────────────
	return []any{l.ID, l.TargetType, l.TargetID, l.UserID, l.CreatedAt}, nil
}

func (m *likeMapper) BuildUpdateQuery(_ string) string {
	return "" // Pas de mise à jour pour un Like (Insert ou Delete uniquement)
}

// ============================================================================
// MAPPER : FAVORIS (content.saved)
// ============================================================================
type savedMapper struct{}

func (m *savedMapper) TableName() string { return "content.saved" }

func (m *savedMapper) Columns() []string {
	return []string{"id", "user_id", "post_id", "created_at"}
}

func (m *savedMapper) ToRow(data any) ([]any, error) {
	// ── ÉTAPE 1 : DÉSÉRIALISATION ───────────────────────────────────────────
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("SavedMapper : Échec de la sérialisation")
		return nil, numan_error.NewInternal()
	}

	var s saved_models.SavedPayload
	if err := json.Unmarshal(jsonBytes, &s); err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("SavedMapper : Échec de la désérialisation")
		return nil, numan_error.NewInternal()
	}

	// ── ÉTAPE 2 : FORMATAGE DE LA LIGNE SQL ─────────────────────────────────
	return []any{s.ID, s.UserID, s.PostID, s.CreatedAt}, nil
}

func (m *savedMapper) BuildUpdateQuery(_ string) string {
	return "" // Pas de mise à jour pour un Favori (Insert ou Delete uniquement)
}

// ############################################################################
// # SCHÉMA : MESSAGING (Conversations, Membres, Messages)
// ############################################################################

// ============================================================================
// MAPPER : MESSAGES (messaging.messages)
// ============================================================================
type messageMapper struct{}

func (m *messageMapper) TableName() string { return "messaging.messages" }

func (m *messageMapper) Columns() []string {
	return []string{"id", "conversation_id", "sender_id", "message_type", "visibility", "content", "attachments", "allow_new_reactions", "created_at", "updated_at"}
}

func (m *messageMapper) ToRow(data any) ([]any, error) {
	// ── ÉTAPE 1 : DÉSÉRIALISATION ───────────────────────────────────────────
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("MessageMapper : Échec de la sérialisation")
		return nil, numan_error.NewInternal()
	}

	var msg message_models.MessagePayload
	if err := json.Unmarshal(jsonBytes, &msg); err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("MessageMapper : Échec de la désérialisation")
		return nil, numan_error.NewInternal()
	}

	// ── ÉTAPE 2 : SÉRIALISATION DES COLONNES JSONB ET NULLS ─────────────────
	attachJSON, _ := json.Marshal(msg.Attachments)

	var contentDB any = msg.Content
	if msg.Content == "" {
		contentDB = nil
	}

	var attachDB any = string(attachJSON)
	if len(msg.Attachments) == 0 {
		attachDB = nil
	}

	// ── ÉTAPE 3 : FORMATAGE DE LA LIGNE SQL ─────────────────────────────────
	return []any{
		msg.ID, msg.ConversationID, msg.SenderID, msg.MessageType, msg.Visibility, contentDB, attachDB, msg.AllowNewReactions, msg.CreatedAt, msg.UpdatedAt,
	}, nil
}

func (m *messageMapper) BuildUpdateQuery(tempTable string) string {
	return buildGenericUpdateQuery(m.TableName(), tempTable, m.Columns())
}

// ============================================================================
// MAPPER : RÉACTIONS AUX MESSAGES (messaging.message_reactions)
// ============================================================================
type messageReactionMapper struct{}

func (m *messageReactionMapper) TableName() string { return "messaging.message_reactions" }

func (m *messageReactionMapper) Columns() []string {
	return []string{"id", "message_id", "user_id", "reaction", "created_at"}
}

func (m *messageReactionMapper) ToRow(data any) ([]any, error) {
	// ── ÉTAPE 1 : DÉSÉRIALISATION ───────────────────────────────────────────
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("MessageReactionMapper : Échec de la sérialisation")
		return nil, numan_error.NewInternal()
	}

	var r message_models.MessageReactionPayload
	if err := json.Unmarshal(jsonBytes, &r); err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("MessageReactionMapper : Échec de la désérialisation")
		return nil, numan_error.NewInternal()
	}

	// ── ÉTAPE 2 : FORMATAGE DE LA LIGNE SQL ─────────────────────────────────
	return []any{r.ID, r.MessageID, r.UserID, r.Reaction, r.CreatedAt}, nil
}

func (m *messageReactionMapper) BuildUpdateQuery(_ string) string {
	// L'Update des réactions est géré via un UPSERT explicite dans postgres_batch.go
	return ""
}

// ============================================================================
// MAPPER : CONVERSATIONS (messaging.conversations)
// ============================================================================
type conversationMapper struct{}

func (m *conversationMapper) TableName() string { return "messaging.conversations" }

func (m *conversationMapper) Columns() []string {
	return []string{"id", "type", "title", "description", "avatar_id", "last_message_id", "state", "settings", "external_link", "created_at", "updated_at"}
}

func (m *conversationMapper) ToRow(data any) ([]any, error) {
	// ── ÉTAPE 1 : DÉSÉRIALISATION ───────────────────────────────────────────
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("ConversationMapper : Échec de la sérialisation")
		return nil, numan_error.NewInternal()
	}

	var c conversation_models.ConversationPayload
	if err := json.Unmarshal(jsonBytes, &c); err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("ConversationMapper : Échec de la désérialisation")
		return nil, numan_error.NewInternal()
	}

	// ── ÉTAPE 2 : SÉRIALISATION DES COLONNES JSONB ET NULLS ─────────────────
	var titleDB any = c.Title
	if c.Title == "" {
		titleDB = nil
	}

	var lastMsgDB any = c.LastMessageID
	if c.LastMessageID == 0 {
		lastMsgDB = nil
	}

	var descDB any = c.Description
	if c.Description == "" {
		descDB = nil
	}

	var avatarDB any = c.AvatarID
	if c.AvatarID == 0 {
		avatarDB = nil
	}

	settingsJSON, _ := json.Marshal(c.Settings)
	var settingsDB any = string(settingsJSON)

	linkJSON, _ := json.Marshal(c.ExternalLink)
	var linkDB any = string(linkJSON)

	// ── ÉTAPE 3 : FORMATAGE DE LA LIGNE SQL ─────────────────────────────────
	return []any{c.ID, c.Type, titleDB, descDB, avatarDB, lastMsgDB, c.State, settingsDB, linkDB, c.CreatedAt, c.UpdatedAt}, nil
}

func (m *conversationMapper) BuildUpdateQuery(t string) string {
	return buildGenericUpdateQuery(m.TableName(), t, m.Columns())
}

// ============================================================================
// MAPPER : MEMBRES (messaging.members)
// ============================================================================
type memberMapper struct{}

func (m *memberMapper) TableName() string { return "messaging.members" }

func (m *memberMapper) Columns() []string {
	return []string{
		"id", "conversation_id", "user_id", "role", "settings",
		"joined_at", "unread_count", "frozen_message_id",
		"last_read_message_id", "created_at", "updated_at",
	}
}

func (m *memberMapper) ToRow(data any) ([]any, error) {
	// ── ÉTAPE 1 : DÉSÉRIALISATION ───────────────────────────────────────────
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("MemberMapper : Échec de la sérialisation")
		return nil, numan_error.NewInternal()
	}

	var mem member_models.MemberPayload
	if err := json.Unmarshal(jsonBytes, &mem); err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("MemberMapper : Échec de la désérialisation")
		return nil, numan_error.NewInternal()
	}

	// ── ÉTAPE 2 : SÉRIALISATION DES COLONNES JSONB ET NULLS ─────────────────
	settingsJSON, _ := json.Marshal(mem.Settings)
	var settingsDB any = string(settingsJSON)

	var frozenDB any = mem.FrozenMessageID
	if mem.FrozenMessageID == 0 {
		frozenDB = nil
	}

	var lastReadDB any = mem.LastReadMessageID
	if mem.LastReadMessageID == 0 {
		lastReadDB = nil
	}

	// ── ÉTAPE 3 : FORMATAGE DE LA LIGNE SQL ─────────────────────────────────
	return []any{
		mem.ID,
		mem.ConversationID,
		mem.UserID,
		mem.Role,
		settingsDB,
		mem.JoinedAt,
		mem.UnreadCount,
		frozenDB,
		lastReadDB,
		mem.CreatedAt,
		mem.UpdatedAt,
	}, nil
}

func (m *memberMapper) BuildUpdateQuery(t string) string {
	return buildGenericUpdateQuery(m.TableName(), t, m.Columns())
}

// ############################################################################
// # SCHÉMA : MODÉRATION (Signalements)
// ############################################################################

// ============================================================================
// MAPPER : SIGNALEMENTS (moderation.reports)
// ============================================================================
type reportMapper struct{}

func (m *reportMapper) TableName() string { return "moderation.reports" }

func (m *reportMapper) Columns() []string {
	return []string{
		"id", "reporter_id", "target_type", "target_ids", "category",
		"reason", "rationale", "state", "importance", "created_at", "updated_at",
	}
}

func (m *reportMapper) ToRow(data any) ([]any, error) {
	// ── ÉTAPE 1 : DÉSÉRIALISATION ───────────────────────────────────────────
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("ReportMapper : Échec de la sérialisation")
		return nil, numan_error.NewInternal()
	}

	var r report_models.ReportPayload
	if err := json.Unmarshal(jsonBytes, &r); err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("ReportMapper : Échec de la désérialisation")
		return nil, numan_error.NewInternal()
	}

	// ── ÉTAPE 2 : TRANSLATION DES VALEURS NULLES ────────────────────────────
	var reasonDB any = r.Reason
	if r.Reason == "" {
		reasonDB = nil
	}

	var rationaleDB any = r.Rationale
	if r.Rationale == "" {
		rationaleDB = nil
	}

	// ── ÉTAPE 3 : FORMATAGE DE LA LIGNE SQL ─────────────────────────────────
	return []any{
		r.ID, r.ReporterID, r.TargetType, pq.Array(r.TargetIDs),
		r.Category, reasonDB, rationaleDB, r.State, r.Importance, r.CreatedAt, r.UpdatedAt,
	}, nil
}

func (m *reportMapper) BuildUpdateQuery(tempTable string) string {
	return buildGenericUpdateQuery(m.TableName(), tempTable, m.Columns())
}

// ============================================================================
// UTILITAIRES DE REQUÊTES GÉNÉRIQUES
// ============================================================================

// buildGenericUpdateQuery génère dynamiquement la requête SQL de fusion
// "UPDATE ... FROM temp_table" en respectant la structure de la table cible.
// Cette méthode est utilisée pour le Slow Path (Bulk Update) dans PostgreSQL.
func buildGenericUpdateQuery(tableName, tempTable string, columns []string) string {
	var sets []string

	// On lie chaque colonne à sa version dans la table temporaire
	for _, c := range columns {
		if c == "id" {
			continue // Sécurité : On ne met jamais à jour la Primary Key
		}
		sets = append(sets, fmt.Sprintf("%s = %s.%s", c, tempTable, c))
	}

	return fmt.Sprintf(
		"UPDATE %s SET %s FROM %s WHERE %s.id = %s.id",
		tableName, strings.Join(sets, ", "), tempTable, tableName, tempTable,
	)
}
