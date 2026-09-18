package strava

import (
	"os"
	"testing"
)

// withTempConfigDir points userConfigDir at a fresh temp directory for the
// duration of the test, restoring it afterward. os.UserConfigDir ignores
// $XDG_CONFIG_HOME on darwin (it always returns $HOME/Library/Application
// Support there), so overriding the env var alone would not isolate these
// tests from a real user's actual saved Strava credentials on macOS.
func withTempConfigDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	original := userConfigDir
	userConfigDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { userConfigDir = original })
}

func TestConfigRoundTrip(t *testing.T) {
	withTempConfigDir(t)

	want := Config{ClientID: "12345", ClientSecret: "secret-value"}
	if err := SaveConfig(want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	path, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config file mode = %o, want 0600", perm)
	}

	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got != want {
		t.Errorf("LoadConfig() = %+v, want %+v", got, want)
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	withTempConfigDir(t)
	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig() with no saved config: want error, got nil")
	}
}
