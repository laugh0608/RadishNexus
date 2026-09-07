package authn

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

type fakePasswordHasher struct {
	dummyCalls int
}

func (*fakePasswordHasher) Hash(password string) (string, error) {
	return "encoded:" + password, nil
}

func (*fakePasswordHasher) Verify(encoded string, password string) (bool, error) {
	return encoded == "encoded:"+password, nil
}

func (hasher *fakePasswordHasher) VerifyDummy(string) error {
	hasher.dummyCalls++
	return nil
}

type fixedSecrets struct {
	ids    []string
	tokens []string
}

func (secrets *fixedSecrets) NewID(string) (string, error) {
	if len(secrets.ids) == 0 {
		return "", errors.New("ID sequence exhausted")
	}
	value := secrets.ids[0]
	secrets.ids = secrets.ids[1:]
	return value, nil
}

func (secrets *fixedSecrets) NewToken() (string, error) {
	if len(secrets.tokens) == 0 {
		return "", errors.New("token sequence exhausted")
	}
	value := secrets.tokens[0]
	secrets.tokens = secrets.tokens[1:]
	return value, nil
}

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

type recordingStore struct {
	bootstrap     BootstrapRecord
	account       LocalAccount
	accountErr    error
	failedLogins  int
	session       SessionRecord
	resolved      ResolvedSession
	resolvedErr   error
	workspace     VerifiedUser
	workspaceErr  error
	revokedDigest []byte
}

func (store *recordingStore) Bootstrap(_ context.Context, record BootstrapRecord) error {
	store.bootstrap = record
	return nil
}

func (store *recordingStore) FindLocalAccount(context.Context, string) (LocalAccount, error) {
	return store.account, store.accountErr
}

func (store *recordingStore) RecordFailedLogin(context.Context, string, time.Time, time.Time, int) error {
	store.failedLogins++
	return nil
}

func (store *recordingStore) CreateSession(_ context.Context, record SessionRecord) error {
	store.session = record
	return nil
}

func (store *recordingStore) ResolveSession(context.Context, []byte, time.Time) (ResolvedSession, error) {
	return store.resolved, store.resolvedErr
}

func (store *recordingStore) ResolveWorkspaceSession(
	context.Context,
	[]byte,
	string,
	time.Time,
) (VerifiedUser, error) {
	return store.workspace, store.workspaceErr
}

func (store *recordingStore) RevokeSession(_ context.Context, digest []byte, _ time.Time) error {
	store.revokedDigest = append([]byte(nil), digest...)
	return nil
}

func TestBootstrapCanonicalizesIdentityAndCreatesOwnerContext(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 30, 4, 0, 0, 0, time.UTC)
	store := &recordingStore{}
	service := NewService(
		store,
		&fakePasswordHasher{},
		&fixedSecrets{ids: []string{"usr_first", "wrk_first"}},
		fixedClock{now: now},
	)

	result, err := service.Bootstrap(context.Background(), BootstrapInput{
		LoginName:     "  Radish.Admin ",
		DisplayName:   "  萝卜管理员  ",
		WorkspaceName: "  Radish Nexus  ",
		Password:      "correct horse battery staple",
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if result != (BootstrapResult{UserID: "usr_first", WorkspaceID: "wrk_first", LoginName: "radish.admin"}) {
		t.Fatalf("Bootstrap() = %#v", result)
	}
	wantRecord := BootstrapRecord{
		UserID:        "usr_first",
		WorkspaceID:   "wrk_first",
		LoginName:     "radish.admin",
		DisplayName:   "萝卜管理员",
		WorkspaceName: "Radish Nexus",
		PasswordHash:  "encoded:correct horse battery staple",
		CreatedAt:     now,
	}
	if !reflect.DeepEqual(store.bootstrap, wantRecord) {
		t.Fatalf("bootstrap record = %#v, want %#v", store.bootstrap, wantRecord)
	}
}

func TestBootstrapRejectsWeakPasswordBeforeHashing(t *testing.T) {
	t.Parallel()
	service := NewService(&recordingStore{}, &fakePasswordHasher{}, &fixedSecrets{}, fixedClock{})
	_, err := service.Bootstrap(context.Background(), BootstrapInput{
		LoginName:     "admin",
		DisplayName:   "Admin",
		WorkspaceName: "Workspace",
		Password:      "too short",
	})
	if !errors.Is(err, authz.ErrInvalid) {
		t.Fatalf("Bootstrap() error = %v, want invalid", err)
	}
}

func TestLoginHidesUnknownAccountBehindDummyVerification(t *testing.T) {
	t.Parallel()
	passwords := &fakePasswordHasher{}
	service := NewService(
		&recordingStore{accountErr: ErrAccountNotFound},
		passwords,
		&fixedSecrets{},
		fixedClock{},
	)
	_, err := service.Login(context.Background(), LoginInput{LoginName: "missing", Password: "not-the-password"})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v, want invalid credentials", err)
	}
	if passwords.dummyCalls != 1 {
		t.Fatalf("dummy verifier calls = %d, want 1", passwords.dummyCalls)
	}
}

func TestLoginCreatesOnlyDigestedSessionSecrets(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 30, 5, 0, 0, 0, time.UTC)
	sessionToken := testToken(1)
	csrfToken := testToken(2)
	resolved := ResolvedSession{Account: SessionAccount{
		User:       User{ID: "usr_first", DisplayName: "Admin"},
		Workspaces: []Workspace{{ID: "wrk_first", Name: "Workspace", Role: "owner"}},
		ExpiresAt:  now.Add(SessionLifetime),
	}}
	store := &recordingStore{
		account:  LocalAccount{UserID: "usr_first", PasswordHash: "encoded:valid password", Status: "active"},
		resolved: resolved,
	}
	service := NewService(
		store,
		&fakePasswordHasher{},
		&fixedSecrets{ids: []string{"ses_first"}, tokens: []string{sessionToken, csrfToken}},
		fixedClock{now: now},
	)

	session, err := service.Login(context.Background(), LoginInput{LoginName: "admin", Password: "valid password"})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if session.Token != sessionToken || session.CSRFToken != csrfToken || !reflect.DeepEqual(session.Account, resolved.Account) {
		t.Fatalf("Login() = %#v", session)
	}
	if string(store.session.TokenDigest) == sessionToken || string(store.session.CSRFTokenDigest) == csrfToken {
		t.Fatal("session record contains a raw token")
	}
	if !reflect.DeepEqual(store.session.TokenDigest, digestToken(sessionToken)) ||
		!reflect.DeepEqual(store.session.CSRFTokenDigest, digestToken(csrfToken)) {
		t.Fatalf("session digests = %x / %x", store.session.TokenDigest, store.session.CSRFTokenDigest)
	}
	if store.session.ExpiresAt.Sub(store.session.CreatedAt) != SessionLifetime {
		t.Fatalf("session lifetime = %s", store.session.ExpiresAt.Sub(store.session.CreatedAt))
	}
}

func TestFailedLoginRecordsAttemptWithoutCreatingSession(t *testing.T) {
	t.Parallel()
	store := &recordingStore{
		account: LocalAccount{UserID: "usr_first", PasswordHash: "encoded:right-password", Status: "active"},
	}
	service := NewService(store, &fakePasswordHasher{}, &fixedSecrets{}, fixedClock{})
	_, err := service.Login(context.Background(), LoginInput{LoginName: "admin", Password: "wrong-password"})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v, want invalid credentials", err)
	}
	if store.failedLogins != 1 || store.session.ID != "" {
		t.Fatalf("failedLogins = %d, session = %#v", store.failedLogins, store.session)
	}
}

func TestCSRFVerificationAndRevocationUseSessionDigests(t *testing.T) {
	t.Parallel()
	token := testToken(3)
	csrfToken := testToken(4)
	store := &recordingStore{resolved: ResolvedSession{CSRFTokenDigest: digestToken(csrfToken)}}
	service := NewService(store, &fakePasswordHasher{}, &fixedSecrets{}, fixedClock{})

	if err := service.VerifyCSRF(context.Background(), token, testToken(5)); !errors.Is(err, ErrInvalidCSRFToken) {
		t.Fatalf("VerifyCSRF() wrong token error = %v", err)
	}
	if err := service.RevokeSession(context.Background(), token, csrfToken); err != nil {
		t.Fatalf("RevokeSession() error = %v", err)
	}
	if !reflect.DeepEqual(store.revokedDigest, digestToken(token)) {
		t.Fatalf("revoked digest = %x", store.revokedDigest)
	}
}

func testToken(value byte) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%032d", value)))
}
