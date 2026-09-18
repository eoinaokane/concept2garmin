package concept2

import (
	"os"
	"path/filepath"
	"strings"
)

// TokenPath returns the file used to cache the Concept2 API access token
// between runs, defaulting to $XDG_CONFIG_HOME/concept2upload/concept2.token
// (or the platform equivalent via os.UserConfigDir - e.g. ~/Library/Application
// Support/concept2upload/concept2.token on macOS), so it only needs to be
// supplied once (via 'concept2upload auth-concept2 <token>' or
// --token/CONCEPT2_TOKEN). This matches internal/strava's TokenPath/ConfigPath,
// which use the same os.UserConfigDir approach.
func TokenPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "concept2upload", "concept2.token"), nil
}

// LoadStoredToken reads a previously saved token, if any.
func LoadStoredToken() (string, error) {
	path, err := TokenPath()
	if err != nil {
		return "", err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

// SaveToken persists token to TokenPath with owner-only permissions.
func SaveToken(token string) error {
	path, err := TokenPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.TrimSpace(token)+"\n"), 0o600)
}
