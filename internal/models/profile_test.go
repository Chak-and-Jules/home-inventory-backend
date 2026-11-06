package models

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openProfileTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	config := &gorm.Config{TranslateError: true, Logger: logger.Default.LogMode(logger.Silent)}
	if dsn := os.Getenv("PROFILE_TEST_POSTGRES_DSN"); dsn != "" {
		admin, err := gorm.Open(postgres.Open(dsn), config)
		require.NoError(t, err)
		adminConn, err := admin.DB()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, adminConn.Close()) })
		schema := "profile_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)
		t.Cleanup(func() { require.NoError(t, admin.Exec("DROP SCHEMA "+schema+" CASCADE").Error) })
		db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), config)
		require.NoError(t, err)
		conn, err := db.DB()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, conn.Close()) })
		return db
	}
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), config)
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	return db
}

func TestProfileFirebaseUIDMigration(t *testing.T) {
	db := openProfileTestDB(t)
	// Simulate an existing profile before the Firebase mapping column exists.
	require.NoError(t, db.Exec("CREATE TABLE profiles (id uuid PRIMARY KEY, email varchar(255), is_admin boolean)").Error)
	id := uuid.New()
	require.NoError(t, db.Exec("INSERT INTO profiles (id, email, is_admin) VALUES (?, ?, ?)", id, "existing@example.com", true).Error)

	require.NoError(t, db.AutoMigrate(&Profile{}))
	var existing Profile
	require.NoError(t, db.First(&existing, "id = ?", id).Error)
	require.Equal(t, id, existing.ID)
	require.Equal(t, "existing@example.com", existing.Email)
	require.True(t, existing.IsAdmin)
	require.Nil(t, existing.FirebaseUID)

	// More than one profile can remain unlinked during the transition.
	require.NoError(t, db.Create(&Profile{ID: uuid.New(), Email: "unlinked@example.com"}).Error)

	uid := strings.Repeat("f", 128)
	require.NoError(t, db.Model(&existing).Update("firebase_uid", uid).Error)
	var linked Profile
	require.NoError(t, db.Where("firebase_uid = ?", uid).First(&linked).Error)
	require.Equal(t, id, linked.ID)
	require.NotNil(t, linked.FirebaseUID)
	require.Equal(t, uid, *linked.FirebaseUID)

	duplicate := Profile{ID: uuid.New(), Email: "duplicate@example.com", FirebaseUID: &uid}
	require.ErrorIs(t, db.Create(&duplicate).Error, gorm.ErrDuplicatedKey)

	// Repeat startup migration without changing the existing mapping.
	require.NoError(t, db.AutoMigrate(&Profile{}))
	var afterRestart Profile
	require.NoError(t, db.First(&afterRestart, "id = ?", id).Error)
	require.Equal(t, id, afterRestart.ID)
	require.Equal(t, uid, *afterRestart.FirebaseUID)
}

func TestProfileFirebaseUIDIsInternal(t *testing.T) {
	uid := "firebase-user-id"
	profile := Profile{ID: uuid.New(), Email: "user@example.com", FirebaseUID: &uid}
	encoded, err := json.Marshal(profile)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &fields))
	require.NotContains(t, fields, "FirebaseUID")
	require.NotContains(t, fields, "firebase_uid")
	require.NotContains(t, string(encoded), uid)

	var decoded Profile
	require.NoError(t, json.Unmarshal([]byte(`{"FirebaseUID":"injected","firebase_uid":"injected"}`), &decoded))
	require.Nil(t, decoded.FirebaseUID)
}
