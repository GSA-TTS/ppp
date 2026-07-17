package sandbox_test

import (
	"path/filepath"
	"testing"

	"github.com/GSA-TTS/ppp/internal/sandbox"
)

// TestResolveDataDir exercises the $PPP_DATA / $XDG_DATA_HOME / $HOME
// precedence chain (spec §5.8). t.Setenv isolates each case so the real
// user environment is never touched.
func TestResolveDataDir(t *testing.T) {
	cases := []struct {
		name       string
		pppData    string
		xdgData    string
		home       string
		wantSuffix string // joined onto home when the case falls through to $HOME
		want       string // absolute expected path when not derived from home
	}{
		{
			name:    "PPP_DATA overrides everything",
			pppData: "/explicit/ppp/data",
			xdgData: "/xdg/data",
			home:    "/home/user",
			want:    "/explicit/ppp/data",
		},
		{
			name:    "XDG_DATA_HOME used when PPP_DATA empty",
			xdgData: "/xdg/data",
			home:    "/home/user",
			want:    filepath.Join("/xdg/data", "ppp"),
		},
		{
			name:       "HOME fallback when neither set",
			home:       "/home/user",
			wantSuffix: filepath.Join(".local", "share", "ppp"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PPP_DATA", tc.pppData)
			t.Setenv("XDG_DATA_HOME", tc.xdgData)
			t.Setenv("HOME", tc.home)

			got, err := sandbox.ResolveDataDir()
			if err != nil {
				t.Fatalf("ResolveDataDir() error = %v", err)
			}
			want := tc.want
			if tc.wantSuffix != "" {
				want = filepath.Join(tc.home, tc.wantSuffix)
			}
			if got != want {
				t.Errorf("ResolveDataDir() = %q, want %q", got, want)
			}
		})
	}
}

func TestResolveConfigDir(t *testing.T) {
	cases := []struct {
		name       string
		pppConfig  string
		xdgConfig  string
		home       string
		wantSuffix string
		want       string
	}{
		{
			name:      "PPP_CONFIG overrides",
			pppConfig: "/explicit/config",
			xdgConfig: "/xdg/config",
			home:      "/home/user",
			want:      "/explicit/config",
		},
		{
			name:      "XDG_CONFIG_HOME used when PPP_CONFIG empty",
			xdgConfig: "/xdg/config",
			home:      "/home/user",
			want:      filepath.Join("/xdg/config", "ppp"),
		},
		{
			name:       "HOME fallback",
			home:       "/home/user",
			wantSuffix: filepath.Join(".config", "ppp"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PPP_CONFIG", tc.pppConfig)
			t.Setenv("XDG_CONFIG_HOME", tc.xdgConfig)
			t.Setenv("HOME", tc.home)

			got, err := sandbox.ResolveConfigDir()
			if err != nil {
				t.Fatalf("ResolveConfigDir() error = %v", err)
			}
			want := tc.want
			if tc.wantSuffix != "" {
				want = filepath.Join(tc.home, tc.wantSuffix)
			}
			if got != want {
				t.Errorf("ResolveConfigDir() = %q, want %q", got, want)
			}
		})
	}
}

func TestResolveCacheDir(t *testing.T) {
	cases := []struct {
		name       string
		pppCache   string
		xdgCache   string
		home       string
		wantSuffix string
		want       string
	}{
		{
			name:     "PPP_CACHE overrides",
			pppCache: "/explicit/cache",
			xdgCache: "/xdg/cache",
			home:     "/home/user",
			want:     "/explicit/cache",
		},
		{
			name:     "XDG_CACHE_HOME used when PPP_CACHE empty",
			xdgCache: "/xdg/cache",
			home:     "/home/user",
			want:     filepath.Join("/xdg/cache", "ppp"),
		},
		{
			name:       "HOME fallback",
			home:       "/home/user",
			wantSuffix: filepath.Join(".cache", "ppp"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PPP_CACHE", tc.pppCache)
			t.Setenv("XDG_CACHE_HOME", tc.xdgCache)
			t.Setenv("HOME", tc.home)

			got, err := sandbox.ResolveCacheDir()
			if err != nil {
				t.Fatalf("ResolveCacheDir() error = %v", err)
			}
			want := tc.want
			if tc.wantSuffix != "" {
				want = filepath.Join(tc.home, tc.wantSuffix)
			}
			if got != want {
				t.Errorf("ResolveCacheDir() = %q, want %q", got, want)
			}
		})
	}
}

func TestSandboxDirAndFilePaths(t *testing.T) {
	data := t.TempDir()
	t.Setenv("PPP_DATA", data)

	dir, err := sandbox.SandboxDir("alpha")
	if err != nil {
		t.Fatalf("SandboxDir() error = %v", err)
	}
	wantDir := filepath.Join(data, "sandboxes", "alpha")
	if dir != wantDir {
		t.Errorf("SandboxDir() = %q, want %q", dir, wantDir)
	}

	jsonPath, err := sandbox.SandboxJSONPath("alpha")
	if err != nil {
		t.Fatalf("SandboxJSONPath() error = %v", err)
	}
	wantJSON := filepath.Join(wantDir, "sandbox.json")
	if jsonPath != wantJSON {
		t.Errorf("SandboxJSONPath() = %q, want %q", jsonPath, wantJSON)
	}

	lockPath, err := sandbox.StateLockPath()
	if err != nil {
		t.Fatalf("StateLockPath() error = %v", err)
	}
	wantLock := filepath.Join(data, "state.lock")
	if lockPath != wantLock {
		t.Errorf("StateLockPath() = %q, want %q", lockPath, wantLock)
	}
}
