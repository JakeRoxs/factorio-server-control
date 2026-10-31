package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var expectedFrontendRoutes = []struct {
	path string
	name string
}{
	{"/saves", "Saves"},
	{"/mods", "Mods"},
	{"/server-settings", "Server settings"},
	{"/game-settings", "Game settings"},
	{"/console", "Console"},
	{"/logs", "Logs"},
	{"/releases", "Releases"},
	{"/profiles", "Profiles"},
	{"/servers", "Servers"},
	{"/user-management", "User management"},
}

func TestFrontendRoutesMatchBeforeIndexFallback(t *testing.T) {
	router := NewRouter()
	for _, expected := range expectedFrontendRoutes {
		t.Run(expected.name, func(t *testing.T) {
			var match mux.RouteMatch
			require.True(t, router.Match(httptest.NewRequest(http.MethodGet, expected.path, nil), &match))
			require.NotNil(t, match.Route)
			assert.Equal(t, expected.name, match.Route.GetName())
			pattern, err := match.Route.GetPathTemplate()
			require.NoError(t, err)
			assert.Equal(t, expected.path, pattern)
			methods, err := match.Route.GetMethods()
			require.NoError(t, err)
			assert.Equal(t, []string{http.MethodGet}, methods)
		})
	}
	var match mux.RouteMatch
	require.True(t, router.Match(httptest.NewRequest(http.MethodGet, "/unknown-frontend-path", nil), &match))
	assert.Equal(t, "Index", match.Route.GetName())
}

func TestProtectedFrontendRoutesRequireAuthentication(t *testing.T) {
	testAuth, store, _ := setupMiddlewareTest(t)
	user, err := testAuth.getUser("admin")
	require.NoError(t, err)
	version, err := sessionCredentialVersion(user.Password)
	require.NoError(t, err)
	cookie := seedSession(t, store, user.Username, version)
	router := NewRouter()
	for _, expected := range expectedFrontendRoutes {
		t.Run(expected.name, func(t *testing.T) {
			// Keep the real router/middleware and replace only file serving so
			// generated frontend output is not required by the backend tests.
			route := router.GetRoute(expected.name)
			require.NotNil(t, route)
			route.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			anonymous := httptest.NewRecorder()
			router.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, expected.path, nil))
			assert.Equal(t, http.StatusUnauthorized, anonymous.Code)

			request := httptest.NewRequest(http.MethodGet, expected.path, nil)
			request.AddCookie(cookie)
			authenticated := httptest.NewRecorder()
			router.ServeHTTP(authenticated, request)
			assert.Equal(t, http.StatusNoContent, authenticated.Code)
			assert.Equal(t, "nosniff", authenticated.Header().Get("X-Content-Type-Options"))
		})
	}
}

func TestLoginFrontendRemainsPublicAndGetOnly(t *testing.T) {
	router := NewRouter()
	router.GetRoute("Login").Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/login", nil))
	assert.Equal(t, http.StatusNoContent, response.Code)
	wrongMethod := httptest.NewRecorder()
	router.ServeHTTP(wrongMethod, httptest.NewRequest(http.MethodPost, "/login", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, wrongMethod.Code)
}
