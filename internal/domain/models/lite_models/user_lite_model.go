package lite_models

type UserLiteRequest struct {
	ID                     int64      `bson:"id" json:"id"`
	Username               string     `bson:"username" json:"username"`
	FirstName              string     `bson:"first_name" json:"first_name"`
	LastName               string     `bson:"last_name" json:"last_name"`
	ProfilePictureID       int64      `bson:"profile_picture_id" json:"profile_picture_id"`
	Bio                    string     `bson:"bio" json:"bio"`
	Grade                  int        `bson:"grade" json:"grade"`
	Badges                 UserBadges `bson:"badges" json:"badges" msgpack:"badges"`
	ConversationPermission int        `bson:"conversation_permission" json:"conversation_permission"`
	AddGroupPermission     int        `bson:"add_group_permission" json:"add_group_permission"`
	HideConnections        int        `bson:"hide_connections" json:"hide_connections" msgpack:"hide_connections"`
	ShowOnlineStatus       bool       `bson:"show_online_status" json:"show_online_status" msgpack:"show_online_status"`
	AllowTagging           int        `bson:"allow_tagging" json:"allow_tagging" msgpack:"allow_tagging"`
	AllowMentions          int        `bson:"allow_mentions" json:"allow_mentions" msgpack:"allow_mentions"`
}
