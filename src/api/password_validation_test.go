package api

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPasswordValidationMatchesHasherLimit(t *testing.T) {
	for _, password := range []string{strings.Repeat("a", 10), strings.Repeat("a", 72), strings.Repeat("é", 36)} {
		require.NoError(t, validateUserPassword(password))
	}
	for _, password := range []string{"", "too-short", strings.Repeat("a", 73), strings.Repeat("é", 37)} {
		require.ErrorIs(t, validateUserPassword(password), ErrInvalidUser)
	}
}

func TestRejectedPasswordLeavesStoredCredentialUnchanged(t *testing.T) {
	a := newAuthorizationTestAuth(t)
	require.NoError(t, a.addUser(User{Username: "operator", Password: "original-password", Role: UserRoleAdmin}))
	before, err := a.getUser("operator")
	require.NoError(t, err)
	require.ErrorIs(t, a.changePassword("operator", strings.Repeat("p", 73)), ErrInvalidUser)
	after, err := a.getUser("operator")
	require.NoError(t, err)
	require.Equal(t, before.Password, after.Password)
	require.NoError(t, a.checkPassword("operator", "original-password"))
}
