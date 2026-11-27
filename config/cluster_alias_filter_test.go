package config

import (
	"os"
	"os/exec"
	"testing"

	kingpin "github.com/alecthomas/kingpin/v2"
	"github.com/stretchr/testify/require"
)

func TestClusterAliasFilterFlag(t *testing.T) {
	originalApp, originalArgs := kingpin.CommandLine, os.Args
	t.Cleanup(func() { kingpin.CommandLine, os.Args = originalApp, originalArgs })
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"unset", nil, ""},
		{"configured", []string{"--cluster-alias-filter=^nebius-poc-1$"}, "^nebius-poc-1$"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kingpin.CommandLine = kingpin.New("clm", "")
			os.Args = append([]string{"clm"}, tc.args...)
			cfg := New("test")
			cfg.ParseFlags()
			if tc.want == "" {
				require.Nil(t, cfg.ClusterAliasFilter)
			} else {
				require.Equal(t, tc.want, cfg.ClusterAliasFilter.String())
				require.True(t, cfg.ClusterAliasFilter.MatchString("nebius-poc-1"))
				require.False(t, cfg.ClusterAliasFilter.MatchString("nebius-poc-2"))
			}
		})
	}
}

func TestClusterAliasFilterFlagRejectsInvalidRegex(t *testing.T) {
	if os.Getenv("CLM_TEST_INVALID_ALIAS_FILTER") == "1" {
		kingpin.CommandLine = kingpin.New("clm", "")
		os.Args = []string{"clm", "--cluster-alias-filter=["}
		New("test").ParseFlags()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestClusterAliasFilterFlagRejectsInvalidRegex$")
	cmd.Env = append(os.Environ(), "CLM_TEST_INVALID_ALIAS_FILTER=1")
	output, err := cmd.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(output), "error parsing regexp")
}
