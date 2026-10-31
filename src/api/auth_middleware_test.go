package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gorilla/sessions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newMiddlewareTestAuth(t *testing.T) Auth {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "auth-middleware.sqlite")), nil)
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&User{}))
	sqlDatabase, err := database.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDatabase.Close() })
	return Auth{db: database}
}

func setupMiddlewareTest(t *testing.T) (Auth, *sessions.CookieStore, []byte) {
	t.Helper()
	testAuth := newMiddlewareTestAuth(t)
	require.NoError(t, testAuth.db.Create(&User{Username: "admin", Password: "hash-a", Role: UserRoleAdmin}).Error)

	originalAuth := auth
	originalStore := sessionStore
	originalCredentialKey := sessionCredentialKey
	auth = testAuth
	sessionStore = sessions.NewCookieStore(bytes.Repeat([]byte{0x42}, 32))
	sessionCredentialKey = bytes.Repeat([]byte{0x43}, 32)
	sessionStore.Options = &sessions.Options{Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: loginSessionMaxAge}
	t.Cleanup(func() {
		auth = originalAuth
		sessionStore = originalStore
		sessionCredentialKey = originalCredentialKey
	})
	return testAuth, sessionStore, sessionCredentialKey
}

func seedSession(t *testing.T, store *sessions.CookieStore, username, credentialVersion string) *http.Cookie {
	t.Helper()
	seedRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	seedRecorder := httptest.NewRecorder()
	session, err := store.Get(seedRequest, "authentication")
	require.NoError(t, err)
	session.Values["username"] = username
	session.Values["credential_version"] = credentialVersion
	require.NoError(t, session.Save(seedRequest, seedRecorder))
	require.NotEmpty(t, seedRecorder.Result().Cookies())
	return seedRecorder.Result().Cookies()[0]
}

func TestAuthMiddlewareRejectsSessionError(t *testing.T) {
	setupMiddlewareTest(t)

	request := httptest.NewRequest(http.MethodGet, "/api/server/status", nil)
	request.AddCookie(&http.Cookie{Name: "authentication", Value: "invalid-signed-cookie"})
	recorder := httptest.NewRecorder()
	AuthMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler must not run for session error")
	})).ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, "authentication", cookies[0].Name)
	assert.Equal(t, -1, cookies[0].MaxAge)
}

func TestAuthMiddlewareRejectsNonStringUsername(t *testing.T) {
	_, store, _ := setupMiddlewareTest(t)
	// Seed a valid cookie, then plant a non-string username into a fresh seed
	// so the middleware must reject it without panicking.
	seedRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	seedRecorder := httptest.NewRecorder()
	session, err := store.Get(seedRequest, "authentication")
	require.NoError(t, err)
	session.Values["username"] = 42
	require.NoError(t, session.Save(seedRequest, seedRecorder))
	require.NotEmpty(t, seedRecorder.Result().Cookies())

	request := httptest.NewRequest(http.MethodGet, "/api/server/status", nil)
	request.AddCookie(seedRecorder.Result().Cookies()[0])
	recorder := httptest.NewRecorder()
	AuthMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler must not run for malformed session username")
	})).ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestAuthMiddlewareRejectsUnknownUser(t *testing.T) {
	_, store, _ := setupMiddlewareTest(t)
	cookie := seedSession(t, store, "does-not-exist", "unused")

	request := httptest.NewRequest(http.MethodGet, "/api/server/status", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	AuthMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler must not run for unknown user")
	})).ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	assert.Equal(t, "authentication is no longer valid\n", recorder.Body.String())
}

func TestAuthMiddlewareAcceptsValidSession(t *testing.T) {
	_, store, _ := setupMiddlewareTest(t)
	credentialVersion, err := sessionCredentialVersion("hash-a")
	require.NoError(t, err)
	cookie := seedSession(t, store, "admin", credentialVersion)

	var seen *User
	request := httptest.NewRequest(http.MethodGet, "/api/server/status", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := authenticatedUserFromRequest(r)
		require.True(t, ok)
		assert.Equal(t, "admin", user.Username)
		assert.Equal(t, UserRoleAdmin, user.Role)
		assert.Empty(t, user.Password)
		seen = &user
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.NotNil(t, seen)
}

func TestAuthMiddlewareRejectsStaleCredentialVersion(t *testing.T) {
	testAuth, store, _ := setupMiddlewareTest(t)
	// Seed with a credential version for hash-a, then change the password to
	// hash-b so the stored version no longer matches.
	credentialVersion, err := sessionCredentialVersion("hash-a")
	require.NoError(t, err)
	cookie := seedSession(t, store, "admin", credentialVersion)
	require.NoError(t, testAuth.db.Model(&User{}).Where("username = ?", "admin").Update("password", "hash-b").Error)

	request := httptest.NewRequest(http.MethodGet, "/api/server/status", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	AuthMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler must not run for revoked session")
	})).ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestAuthMiddlewareRejectsMissingCredentialVersion(t *testing.T) {
	_, store, _ := setupMiddlewareTest(t)
	seedRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	seedRecorder := httptest.NewRecorder()
	session, err := store.Get(seedRequest, "authentication")
	require.NoError(t, err)
	session.Values["username"] = "admin"
	// No credential_version set.
	require.NoError(t, session.Save(seedRequest, seedRecorder))
	cookie := seedRecorder.Result().Cookies()[0]

	request := httptest.NewRequest(http.MethodGet, "/api/server/status", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	AuthMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler must not run for missing credential version")
	})).ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestGetSessionUsernameReturnsFalseForInvalid(t *testing.T) {
	session := &sessions.Session{Values: map[interface{}]interface{}{"username": 42}}
	_, ok := getSessionUsername(session)
	assert.False(t, ok)

	session = &sessions.Session{Values: map[interface{}]interface{}{"username": ""}}
	_, ok = getSessionUsername(session)
	assert.False(t, ok)

	session = &sessions.Session{Values: map[interface{}]interface{}{}}
	_, ok = getSessionUsername(session)
	assert.False(t, ok)

	session = &sessions.Session{Values: map[interface{}]interface{}{"username": "admin"}}
	username, ok := getSessionUsername(session)
	assert.True(t, ok)
	assert.Equal(t, "admin", username)
}

func TestValidateCredentialVersionReturnsFalseForMissingOrMismatched(t *testing.T) {
	session := &sessions.Session{Values: map[interface{}]interface{}{}}
	assert.False(t, validateCredentialVersion(session, "expected"))

	session = &sessions.Session{Values: map[interface{}]interface{}{"credential_version": "wrong"}}
	assert.False(t, validateCredentialVersion(session, "expected"))

	session = &sessions.Session{Values: map[interface{}]interface{}{"credential_version": "expected"}}
	assert.True(t, validateCredentialVersion(session, "expected"))
}

func TestAuthMiddlewareRejectsValidationErrors(t *testing.T) {
	for _, failure := range []string{"database", "credential key"} {
		t.Run(failure, func(t *testing.T) {
			testAuth, store, _ := setupMiddlewareTest(t)
			cookie := seedSession(t, store, "admin", "unused")
			if failure == "database" {
				require.NoError(t, testAuth.db.Migrator().DropTable(&User{}))
			} else {
				sessionCredentialKey = nil
			}
			request := httptest.NewRequest(http.MethodGet, "/api/server/status", nil)
			request.AddCookie(cookie)
			recorder := httptest.NewRecorder()
			AuthMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("handler must not run when authentication cannot be validated")
			})).ServeHTTP(recorder, request)
			assert.Equal(t, http.StatusInternalServerError, recorder.Code)
			assert.Equal(t, "unable to validate authentication\n", recorder.Body.String())
		})
	}
}
