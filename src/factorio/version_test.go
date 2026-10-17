package factorio

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionUnmarshalTextResetsOmittedComponents(t *testing.T) {
	for input, expected := range map[string]Version{
		"2": {2, 0, 0, 0}, "2.1": {2, 1, 0, 0}, "2.1.20": {2, 1, 20, 0}, "2.1.20.4": {2, 1, 20, 4},
	} {
		t.Run(input, func(t *testing.T) {
			version := Version{1, 1, 110, 42}
			require.NoError(t, version.UnmarshalText([]byte(input)))
			require.Equal(t, expected, version)
		})
	}
}

func TestVersionUnmarshalTextFailureIsAtomic(t *testing.T) {
	for _, input := range []string{"", "2.bad", "2.1.bad", "2.1.20.4.5", "-1.0", "2.4294967296"} {
		t.Run(input, func(t *testing.T) {
			original := Version{1, 1, 110, 42}
			version := original
			require.Error(t, version.UnmarshalText([]byte(input)))
			require.Equal(t, original, version)
		})
	}
}
