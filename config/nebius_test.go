package config

import (
	"os"
	"testing"

	kingpin "github.com/alecthomas/kingpin/v2"
	"github.com/stretchr/testify/require"
)

func TestNebiusCredentialsFileFlag(t *testing.T) {
	originalApp, originalArgs := kingpin.CommandLine, os.Args
	t.Cleanup(func() { kingpin.CommandLine, os.Args = originalApp, originalArgs })
	t.Setenv("NEBIUS_CREDENTIALS_FILE", "/ignored/environment.json")
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"unset", nil, ""},
		{"configured", []string{"--nebius-credentials-file=/path/credentials.json"}, "/path/credentials.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kingpin.CommandLine = kingpin.New("clm", "")
			os.Args = append([]string{"clm"}, tc.args...)
			cfg := New("test")
			cfg.ParseFlags()
			require.Equal(t, tc.want, cfg.NebiusCredentialsFile)
		})
	}
}
