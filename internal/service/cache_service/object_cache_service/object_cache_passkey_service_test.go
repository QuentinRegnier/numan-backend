package object_cache_service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoMongo "github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/lib/pq"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestPasskeyObjectCache_SetAndDelete(t *testing.T) {
	srv := test.SetupTestRedis(t)
	ctx := context.Background()

	pk := two_factor_models.PasskeyPayload{
		ID:              10,
		UserID:          100,
		CredentialID:    "cred-abc",
		PublicKey:       []byte{1, 2, 3},
		AttestationType: "none",
		AAGUID:          []byte{4, 5, 6},
		SignCount:       1,
		DeviceName:      "MacBook Pro",
		Transports:      []string{"internal"},
		CreatedAt:       1700000001000,
		UpdatedAt:       1700000002000,
	}

	// Pre-populate user list key to verify SetPasskeyInObjectCache invalidates it
	_ = repoRedis.Passkeys.SetObject(ctx, passkeyUserKey(pk.UserID), []two_factor_models.PasskeyPayload{pk})

	if err := SetPasskeyInObjectCache(ctx, pk); err != nil {
		t.Fatalf("SetPasskeyInObjectCache failed: %v", err)
	}

	var byID two_factor_models.PasskeyPayload
	if err := repoRedis.Passkeys.GetObject(ctx, pk.ID, &byID); err != nil || !reflect.DeepEqual(byID, pk) {
		t.Errorf("expected passkey cached by ID, got %+v (err=%v)", byID, err)
	}

	var byCred two_factor_models.PasskeyPayload
	if err := repoRedis.Passkeys.GetObject(ctx, passkeyCredKey(pk.CredentialID), &byCred); err != nil || !reflect.DeepEqual(byCred, pk) {
		t.Errorf("expected passkey cached by credID, got %+v (err=%v)", byCred, err)
	}

	if exists, _ := repoRedis.Passkeys.Exists(ctx, passkeyUserKey(pk.UserID)); exists {
		t.Errorf("expected user passkey list key to be invalidated by SetPasskeyInObjectCache")
	}

	// DeletePasskeyFromObjectCache evicts all keys
	_ = repoRedis.Passkeys.SetObject(ctx, passkeyUserKey(pk.UserID), []two_factor_models.PasskeyPayload{pk})
	if err := DeletePasskeyFromObjectCache(ctx, pk.UserID, pk.ID, pk.CredentialID); err != nil {
		t.Fatalf("DeletePasskeyFromObjectCache failed: %v", err)
	}
	if exists, _ := repoRedis.Passkeys.Exists(ctx, pk.ID); exists {
		t.Errorf("expected passkey ID key deleted")
	}
	if exists, _ := repoRedis.Passkeys.Exists(ctx, passkeyCredKey(pk.CredentialID)); exists {
		t.Errorf("expected passkey cred key deleted")
	}
	if exists, _ := repoRedis.Passkeys.Exists(ctx, passkeyUserKey(pk.UserID)); exists {
		t.Errorf("expected passkey user key deleted")
	}

	// DeletePasskeyFromObjectCache with empty credentialID
	if err := DeletePasskeyFromObjectCache(ctx, pk.UserID, pk.ID, ""); err != nil {
		t.Fatalf("DeletePasskeyFromObjectCache with empty credentialID failed: %v", err)
	}

	// Error paths
	srv.SetFailCmd("SET", errors.New("set failed"))
	errSet := SetPasskeyInObjectCache(ctx, pk)
	assertAppError(t, errSet, http.StatusInternalServerError, numan_error.CodeCacheError)
	srv.SetFailCmd("SET", nil)

	srv.SetFailCmd("DEL", errors.New("del failed"))
	errSetDel := SetPasskeyInObjectCache(ctx, pk)
	assertAppError(t, errSetDel, http.StatusInternalServerError, numan_error.CodeCacheError)
	errDel := DeletePasskeyFromObjectCache(ctx, pk.UserID, pk.ID, pk.CredentialID)
	assertAppError(t, errDel, http.StatusInternalServerError, numan_error.CodeCacheError)
	srv.SetFailCmd("DEL", nil)
}

func TestGetUserPasskeysCascade(t *testing.T) {
	ctx := context.Background()

	want := two_factor_models.PasskeyPayload{
		ID:              21,
		UserID:          200,
		CredentialID:    "cred-21",
		PublicKey:       []byte{9, 8, 7},
		AttestationType: "packed",
		AAGUID:          []byte{1, 1, 1},
		SignCount:       3,
		DeviceName:      "iPhone",
		Transports:      []string{"hybrid", "internal"},
		CreatedAt:       1700000010000,
		UpdatedAt:       1700000020000,
	}

	t.Run("L1 hit returns cached list immediately", func(t *testing.T) {
		test.SetupTestRedis(t)
		_ = repoRedis.Passkeys.SetObject(ctx, passkeyUserKey(want.UserID), []two_factor_models.PasskeyPayload{want})

		got, err := GetUserPasskeysCascade(ctx, want.UserID)
		if err != nil {
			t.Fatalf("GetUserPasskeysCascade(L1) failed: %v", err)
		}
		if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
			t.Errorf("GetUserPasskeysCascade(L1) = %+v, want [%+v]", got, want)
		}
	})

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("L2 Mongo hit promotes to L1", func(mt *mtest.T) {
		srv := test.SetupTestRedis(mt.T)
		restore := test.ResetCollections()
		defer restore()
		repoMongo.Passkeys = test.NewMockCollection(mt, "auth.user_passkeys", nil)

		// Also exercise L1 warning branch when GET fails with non-Nil error
		srv.SetFailCmd("GET", errors.New("redis l1 read err"))

		mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.auth.user_passkeys", mtest.FirstBatch, bson.D{
			{Key: "id", Value: want.ID},
			{Key: "user_id", Value: want.UserID},
			{Key: "credential_id", Value: want.CredentialID},
			{Key: "public_key", Value: want.PublicKey},
			{Key: "attestation_type", Value: want.AttestationType},
			{Key: "aaguid", Value: want.AAGUID},
			{Key: "sign_count", Value: want.SignCount},
			{Key: "device_name", Value: want.DeviceName},
			{Key: "transports", Value: want.Transports},
			{Key: "created_at", Value: want.CreatedAt},
			{Key: "updated_at", Value: want.UpdatedAt},
		}))

		got, err := GetUserPasskeysCascade(ctx, want.UserID)
		srv.SetFailCmd("GET", nil)
		if err != nil {
			mt.Fatalf("GetUserPasskeysCascade(L2) failed: %v", err)
		}
		if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
			mt.Errorf("GetUserPasskeysCascade(L2) = %+v, want [%+v]", got, want)
		}

		// Verify promoted to L1
		var byCred two_factor_models.PasskeyPayload
		if err := repoRedis.Passkeys.GetObject(ctx, passkeyCredKey(want.CredentialID), &byCred); err != nil || !reflect.DeepEqual(byCred, want) {
			mt.Errorf("expected passkey promoted to L1 by credID, got %+v (err=%v)", byCred, err)
		}
	})

	mt.Run("L3 Postgres hit self-heals L1 and L2, plus L3 empty and L3 error", func(mt *mtest.T) {
		test.SetupTestRedis(mt.T)
		restore := test.ResetCollections()
		defer restore()
		repoMongo.Passkeys = test.NewMockCollection(mt, "auth.user_passkeys", nil)

		// L2 returns error -> falls back to L3
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{
			Code:    1,
			Message: "mongo down",
		}))

		transportsVal, _ := pq.Array(want.Transports).Value()
		cols := []string{"id", "user_id", "credential_id", "public_key", "attestation_type", "aaguid", "sign_count", "device_name", "transports", "created_at", "updated_at"}

		var mode string
		test.SetupMockDB(mt.T, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			switch mode {
			case "error":
				return nil, errors.New("pg down")
			case "empty":
				return test.NewMockRows(cols, nil), nil
			default:
				return test.NewMockRows(cols, [][]driver.Value{{
					want.ID, want.UserID, want.CredentialID, want.PublicKey, want.AttestationType,
					want.AAGUID, want.SignCount, want.DeviceName, transportsVal, want.CreatedAt, want.UpdatedAt,
				}}), nil
			}
		}, nil)

		got, err := GetUserPasskeysCascade(ctx, want.UserID)
		if err != nil {
			mt.Fatalf("GetUserPasskeysCascade(L3) failed: %v", err)
		}
		if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
			mt.Errorf("GetUserPasskeysCascade(L3) = %+v, want [%+v]", got, want)
		}

		// Verify L1 was hydrated from L3
		waitForCondition(mt, 250*time.Millisecond, func() bool {
			ok, _ := repoRedis.Passkeys.Exists(ctx, want.ID)
			return ok
		})

		// L3 empty
		mode = "empty"
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.auth.user_passkeys", mtest.FirstBatch))
		emptyList, err := GetUserPasskeysCascade(ctx, 999)
		if err != nil || len(emptyList) != 0 {
			mt.Errorf("expected empty slice when L3 returns no rows, got %+v (err=%v)", emptyList, err)
		}

		// L3 error
		mode = "error"
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.auth.user_passkeys", mtest.FirstBatch))
		_, err = GetUserPasskeysCascade(ctx, 998)
		assertAppError(mt, err, http.StatusInternalServerError, numan_error.CodeDatabaseError)
	})
}

func TestGetPasskeyByCredentialIDCascade(t *testing.T) {
	ctx := context.Background()

	want := two_factor_models.PasskeyPayload{
		ID:              31,
		UserID:          300,
		CredentialID:    "cred-31",
		PublicKey:       []byte{5, 6, 7},
		AttestationType: "none",
		AAGUID:          []byte{2, 2, 2},
		SignCount:       7,
		DeviceName:      "YubiKey",
		Transports:      []string{"usb"},
		CreatedAt:       1700000030000,
		UpdatedAt:       1700000040000,
	}

	t.Run("L1 hit returns cached passkey", func(t *testing.T) {
		test.SetupTestRedis(t)
		_ = SetPasskeyInObjectCache(ctx, want)

		got, err := GetPasskeyByCredentialIDCascade(ctx, want.CredentialID)
		if err != nil {
			t.Fatalf("GetPasskeyByCredentialIDCascade(L1) failed: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("GetPasskeyByCredentialIDCascade(L1) = %+v, want %+v", got, want)
		}
	})

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("L2 hit promotes to L1", func(mt *mtest.T) {
		srv := test.SetupTestRedis(mt.T)
		restore := test.ResetCollections()
		defer restore()
		repoMongo.Passkeys = test.NewMockCollection(mt, "auth.user_passkeys", nil)

		srv.SetFailCmd("GET", errors.New("redis l1 read err"))

		mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.auth.user_passkeys", mtest.FirstBatch, bson.D{
			{Key: "id", Value: want.ID},
			{Key: "user_id", Value: want.UserID},
			{Key: "credential_id", Value: want.CredentialID},
			{Key: "public_key", Value: want.PublicKey},
			{Key: "attestation_type", Value: want.AttestationType},
			{Key: "aaguid", Value: want.AAGUID},
			{Key: "sign_count", Value: want.SignCount},
			{Key: "device_name", Value: want.DeviceName},
			{Key: "transports", Value: want.Transports},
			{Key: "created_at", Value: want.CreatedAt},
			{Key: "updated_at", Value: want.UpdatedAt},
		}))

		got, err := GetPasskeyByCredentialIDCascade(ctx, want.CredentialID)
		srv.SetFailCmd("GET", nil)
		if err != nil {
			mt.Fatalf("GetPasskeyByCredentialIDCascade(L2) failed: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			mt.Errorf("GetPasskeyByCredentialIDCascade(L2) = %+v, want %+v", got, want)
		}

		var cached two_factor_models.PasskeyPayload
		if err := repoRedis.Passkeys.GetObject(ctx, want.ID, &cached); err != nil || !reflect.DeepEqual(cached, want) {
			mt.Errorf("expected passkey promoted to L1 by ID, got %+v (err=%v)", cached, err)
		}
	})

	mt.Run("L3 hit self-heals L1 and L2, 404 when not found, 500 on L3 error", func(mt *mtest.T) {
		test.SetupTestRedis(mt.T)
		restore := test.ResetCollections()
		defer restore()
		repoMongo.Passkeys = test.NewMockCollection(mt, "auth.user_passkeys", nil)

		transportsVal, _ := pq.Array(want.Transports).Value()
		cols := []string{"id", "user_id", "credential_id", "public_key", "attestation_type", "aaguid", "sign_count", "device_name", "transports", "created_at", "updated_at"}

		var mode string
		test.SetupMockDB(mt.T, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			switch mode {
			case "not_found":
				return nil, sql.ErrNoRows
			case "error":
				return nil, errors.New("pg down")
			default:
				return test.NewMockRows(cols, [][]driver.Value{{
					want.ID, want.UserID, want.CredentialID, want.PublicKey, want.AttestationType,
					want.AAGUID, want.SignCount, want.DeviceName, transportsVal, want.CreatedAt, want.UpdatedAt,
				}}), nil
			}
		}, nil)

		// 1. L3 hit
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.auth.user_passkeys", mtest.FirstBatch))
		got, err := GetPasskeyByCredentialIDCascade(ctx, want.CredentialID)
		if err != nil {
			mt.Fatalf("GetPasskeyByCredentialIDCascade(L3) failed: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			mt.Errorf("GetPasskeyByCredentialIDCascade(L3) = %+v, want %+v", got, want)
		}

		waitForCondition(mt, 250*time.Millisecond, func() bool {
			ok, _ := repoRedis.Passkeys.Exists(ctx, passkeyCredKey(want.CredentialID))
			return ok
		})

		// 2. 404 NotFound when L3 returns ID == 0 (sql.ErrNoRows)
		mode = "not_found"
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.auth.user_passkeys", mtest.FirstBatch))
		_, err = GetPasskeyByCredentialIDCascade(ctx, "cred-missing")
		assertAppError(mt, err, http.StatusNotFound, numan_error.CodeNotFound)

		// 3. 500 error when L3 fails
		mode = "error"
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.auth.user_passkeys", mtest.FirstBatch))
		_, err = GetPasskeyByCredentialIDCascade(ctx, "cred-err")
		assertAppError(mt, err, http.StatusInternalServerError, numan_error.CodeDatabaseError)
	})
}
