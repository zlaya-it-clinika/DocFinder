package livestore

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"docfinder/core/internal/auth"
)

// Вход через каталог не забирает локальную учетную запись с той же почтой,
// а запись, созданную каталогом, открывает и при следующем входе
func TestDirectoryLoginKeepsLocalAccount(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is empty")
	}
	store, err := Open_LiveStore(databaseURL, 768, nil, auth.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)

	ctx := context.Background()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	localID := "user-dir-local-" + suffix
	localEmail := "local-" + suffix + "@corp.local"
	directoryEmail := "directory-" + suffix + "@corp.local"
	t.Cleanup(func() {
		_, _ = store.pool.Exec(ctx, `DELETE FROM auth_tokens WHERE user_id IN (SELECT user_id FROM users WHERE login IN ($1, $2))`, localEmail, directoryEmail)
		_, _ = store.pool.Exec(ctx, `DELETE FROM permission_events WHERE target_user_id IN (SELECT user_id FROM users WHERE login IN ($1, $2))`, localEmail, directoryEmail)
		_, _ = store.pool.Exec(ctx, `DELETE FROM user_roles WHERE user_id IN (SELECT user_id FROM users WHERE login IN ($1, $2))`, localEmail, directoryEmail)
		_, _ = store.pool.Exec(ctx, `DELETE FROM users WHERE login IN ($1, $2)`, localEmail, directoryEmail)
	})
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO users (user_id, login, password_hash, display_name, access_level) VALUES ($1, $2, 'x', 'Локальный', 1)`, localID, localEmail); err != nil {
		t.Fatal(err)
	}

	_, err = store.login_DirectoryIdentity(ctx, auth.LdapSettings{}, auth.LdapIdentity{Email: localEmail, Name: "Каталог"}, "token-dir-local-"+suffix)
	if !errors.Is(err, ErrDirectoryAccountConflict) {
		t.Fatalf("directory login into local account: %v", err)
	}

	first, err := store.login_DirectoryIdentity(ctx, auth.LdapSettings{}, auth.LdapIdentity{Email: directoryEmail, Name: "Каталог"}, "token-dir-first-"+suffix)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.login_DirectoryIdentity(ctx, auth.LdapSettings{}, auth.LdapIdentity{Email: directoryEmail, Name: "Каталог"}, "token-dir-second-"+suffix)
	if err != nil {
		t.Fatal(err)
	}
	if first.UserID != second.UserID {
		t.Fatalf("directory account changed: %s %s", first.UserID, second.UserID)
	}
}
