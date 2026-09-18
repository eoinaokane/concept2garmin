package concept2

import (
	"os"
	"path/filepath"
	"strings"
)

// TokenPath returns ~/.config/concept2garmin/concept2.token, where the
// Concept2 API access token is cached between runs so it only needs to be
// supplied once (via 'concept2garmin auth <token>' or --token/CONCEPT2_TOKEN).
func TokenPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "concept2garmin", "concept2.token"), nil
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
