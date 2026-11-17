package schemas

import (
	"reflect"
	"testing"
)

func TestUserRecoSkipsSchema(t *testing.T) {
	if UserRecoSkipsSchema["user_id"] != reflect.Int64 {
		t.Fatalf("attendu user_id de type reflect.Int64, obtenu %v", UserRecoSkipsSchema["user_id"])
	}
	if UserRecoSkipsSchema["skipped_user_id"] != reflect.Int64 {
		t.Fatalf("attendu skipped_user_id de type reflect.Int64, obtenu %v", UserRecoSkipsSchema["skipped_user_id"])
	}
	if UserRecoSkipsSchema["created_at"] != reflect.Struct {
		t.Fatalf("attendu created_at de type reflect.Struct (time.Time), obtenu %v", UserRecoSkipsSchema["created_at"])
	}
}
