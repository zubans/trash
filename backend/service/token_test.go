package service

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"healthlogin/backend/repository"
)

// fakeRefreshRepo is an in-memory RefreshTokenRepository that mirrors the
// guarded UPDATE in the real one: a token can be consumed exactly once.
type fakeRefreshRepo struct {
	tokens map[string]*repository.RefreshToken
}

func newFakeRefreshRepo() *fakeRefreshRepo {
	return &fakeRefreshRepo{tokens: make(map[string]*repository.RefreshToken)}
}

func (f *fakeRefreshRepo) Create(userID uuid.UUID, tokenHash string, expiresAt time.Time) error {
	f.tokens[tokenHash] = &repository.RefreshToken{
		ID: uuid.New(), UserID: userID, ExpiresAt: expiresAt, CreatedAt: time.Now(),
	}
	return nil
}

func (f *fakeRefreshRepo) FindByHash(tokenHash string) (*repository.RefreshToken, error) {
	t, ok := f.tokens[tokenHash]
	if !ok {
		return nil, repository.ErrRefreshTokenNotFound
	}
	return t, nil
}

func (f *fakeRefreshRepo) MarkUsed(tokenHash string) error {
	t, ok := f.tokens[tokenHash]
	if !ok || !t.IsUsable(time.Now()) {
		return repository.ErrConflict
	}
	now := time.Now()
	t.UsedAt = &now
	return nil
}

func (f *fakeRefreshRepo) RevokeAllForUser(userID uuid.UUID) error {
	now := time.Now()
	for _, t := range f.tokens {
		if t.UserID == userID && t.RevokedAt == nil {
			t.RevokedAt = &now
		}
	}
	return nil
}

func (f *fakeRefreshRepo) Revoke(tokenHash string) error {
	if t, ok := f.tokens[tokenHash]; ok && t.RevokedAt == nil {
		now := time.Now()
		t.RevokedAt = &now
	}
	return nil
}

func (f *fakeRefreshRepo) DeleteExpired() (int64, error) { return 0, nil }

func (f *fakeRefreshRepo) usableCount(userID uuid.UUID) int {
	n := 0
	for _, t := range f.tokens {
		if t.UserID == userID && t.IsUsable(time.Now()) {
			n++
		}
	}
	return n
}

func newSessionTestService(t *testing.T) (*AuthService, *mockRepo, *fakeRefreshRepo, *repository.User) {
	t.Helper()
	repo := newMockRepo()
	refresh := newFakeRefreshRepo()
	svc := NewAuthServiceWithSecret(repo, "test-secret", nil, nil).
		WithSessionStorage(refresh, nil)

	user := &repository.User{ID: uuid.New(), Phone: "+79990000000", Role: "EXECUTOR", Status: "ACTIVE"}
	repo.users[user.Phone] = user
	return svc, repo, refresh, user
}

func TestRefreshRotatesTheToken(t *testing.T) {
	svc, _, _, user := newSessionTestService(t)

	first, err := svc.IssueTokenPair(user)
	if err != nil {
		t.Fatalf("unexpected error issuing pair: %v", err)
	}
	if first.RefreshToken == "" || first.AccessToken == "" {
		t.Fatal("expected both tokens to be issued")
	}

	second, err := svc.Refresh(first.RefreshToken)
	if err != nil {
		t.Fatalf("unexpected error refreshing: %v", err)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Error("refresh must rotate the token, the same value came back")
	}

	// The rotated token works.
	if _, err := svc.Refresh(second.RefreshToken); err != nil {
		t.Errorf("expected the rotated token to be usable: %v", err)
	}
}

// TestRefreshReplayRevokesEverySession is the reason rotation exists: a token
// presented twice means the value leaked, so every session must end.
func TestRefreshReplayRevokesEverySession(t *testing.T) {
	svc, _, refresh, user := newSessionTestService(t)

	stolen, err := svc.IssueTokenPair(user)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// A second, unrelated session of the same user (another device).
	if _, err := svc.IssueTokenPair(user); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := svc.Refresh(stolen.RefreshToken); err != nil {
		t.Fatalf("first exchange should succeed: %v", err)
	}

	// The attacker replays the value the legitimate client already used.
	if _, err := svc.Refresh(stolen.RefreshToken); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Errorf("expected the replay to be rejected, got %v", err)
	}
	if n := refresh.usableCount(user.ID); n != 0 {
		t.Errorf("a replay must end every session of the user, %d still usable", n)
	}
}

func TestRefreshRejectsUnknownExpiredAndRevoked(t *testing.T) {
	svc, _, refresh, user := newSessionTestService(t)

	if _, err := svc.Refresh("not-a-real-token"); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Errorf("unknown token: expected ErrInvalidRefreshToken, got %v", err)
	}
	if _, err := svc.Refresh(""); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Errorf("empty token: expected ErrInvalidRefreshToken, got %v", err)
	}

	expired, _ := svc.IssueTokenPair(user)
	refresh.tokens[hashRefreshToken(expired.RefreshToken)].ExpiresAt = time.Now().Add(-time.Minute)
	if _, err := svc.Refresh(expired.RefreshToken); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Errorf("expired token: expected ErrInvalidRefreshToken, got %v", err)
	}

	loggedOut, _ := svc.IssueTokenPair(user)
	if err := svc.RevokeRefreshToken(loggedOut.RefreshToken); err != nil {
		t.Fatalf("unexpected error revoking: %v", err)
	}
	if _, err := svc.Refresh(loggedOut.RefreshToken); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Errorf("revoked token: expected ErrInvalidRefreshToken, got %v", err)
	}
}

// TestRefreshRefusesBannedUser keeps a ban from being outlived by a session that
// simply keeps renewing itself.
func TestRefreshRefusesBannedUser(t *testing.T) {
	svc, repo, refresh, user := newSessionTestService(t)

	pair, err := svc.IssueTokenPair(user)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	repo.users[user.Phone].Status = "BANNED"

	if _, err := svc.Refresh(pair.RefreshToken); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Errorf("expected a banned user to be refused, got %v", err)
	}
	if n := refresh.usableCount(user.ID); n != 0 {
		t.Errorf("banning must end every session, %d still usable", n)
	}
}

// TestIssuedAccessTokenCarriesTheUser guards the claims the middleware reads.
func TestIssuedAccessTokenCarriesTheUser(t *testing.T) {
	svc, _, _, user := newSessionTestService(t)

	pair, err := svc.IssueTokenPair(user)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	claims, err := svc.ParseJWT(pair.AccessToken)
	if err != nil {
		t.Fatalf("issued access token does not parse: %v", err)
	}
	if claims.UserID != user.ID {
		t.Errorf("expected sub %s, got %s", user.ID, claims.UserID)
	}
	if pair.ExpiresAt.Before(time.Now()) {
		t.Error("expires_at must be in the future")
	}
}

// TestChangePasswordRequiresTheCurrentOne covers the endpoint that the profile
// page has always called and that did not exist until now.
func TestChangePasswordRequiresTheCurrentOne(t *testing.T) {
	svc, repo, refresh, user := newSessionTestService(t)

	hash, err := bcrypt.GenerateFromPassword([]byte("Str0ngPassw0rd"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	repo.users[user.Phone].Password = string(hash)

	// A session that is about to be ended by the change.
	if _, err := svc.IssueTokenPair(user); err != nil {
		t.Fatalf("issue pair: %v", err)
	}

	if _, err := svc.ChangePassword(user.ID, "wrong-password", "An0therStr0ng!"); err == nil {
		t.Error("expected the wrong current password to be refused")
	}
	if _, err := svc.ChangePassword(user.ID, "Str0ngPassw0rd", "short"); err == nil {
		t.Error("expected a weak new password to be refused")
	}
	if _, err := svc.ChangePassword(user.ID, "Str0ngPassw0rd", "Str0ngPassw0rd"); err == nil {
		t.Error("expected an unchanged password to be refused")
	}

	pair, err := svc.ChangePassword(user.ID, "Str0ngPassw0rd", "An0therStr0ng!")
	if err != nil {
		t.Fatalf("unexpected error changing password: %v", err)
	}
	if pair.RefreshToken == "" {
		t.Error("the caller must receive a usable session back")
	}

	// The new password works and the old one does not.
	stored := repo.users[user.Phone].Password
	if bcrypt.CompareHashAndPassword([]byte(stored), []byte("An0therStr0ng!")) != nil {
		t.Error("the new password was not stored")
	}
	if bcrypt.CompareHashAndPassword([]byte(stored), []byte("Str0ngPassw0rd")) == nil {
		t.Error("the old password still works")
	}

	// Only the freshly issued session survives.
	if n := refresh.usableCount(user.ID); n != 1 {
		t.Errorf("expected every other session to end, %d are usable", n)
	}
}
