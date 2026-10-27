package moderation_models

// BlameRecord trace un blâme individuel infligé à un utilisateur pour l'audit des 3 blâmes (§C.11).
type BlameRecord struct {
	ReportID        int64  `msgpack:"report_id" json:"report_id"`
	TargetType      int    `msgpack:"target_type" json:"target_type"`
	TargetID        int64  `msgpack:"target_id" json:"target_id"`
	ProposerModID   int64  `msgpack:"proposer_mod_id" json:"proposer_mod_id"`
	ValidatorModID  int64  `msgpack:"validator_mod_id" json:"validator_mod_id"`
	FinalVoterModID int64  `msgpack:"final_voter_mod_id,omitempty" json:"final_voter_mod_id,omitempty"`
	Motivation      string `msgpack:"motivation" json:"motivation"`
	CreatedAt       int64  `msgpack:"created_at" json:"created_at"`
}
