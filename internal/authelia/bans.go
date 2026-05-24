package authelia

import (
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

// AutheliaBanManager handles IP bans against Authelia's storage.
// It works by executing the `authelia storage bans ip` CLI commands,
// which requires the authelia binary to be available and configured.
type AutheliaBanManager struct {
	// Path to the authelia binary (default: "authelia")
	BinaryPath string
	// Path to authelia configuration.yml
	ConfigPath string
	// Authelia storage encryption key
	EncryptionKey string
	// Authelia SQLite path (optional, for direct DB access)
	SQLitePath string
}

// NewBanManager creates a new AutheliaBanManager with defaults.
func NewBanManager(configPath string) *AutheliaBanManager {
	return &AutheliaBanManager{
		BinaryPath: "authelia",
		ConfigPath: configPath,
	}
}

// BanIP adds an IP ban via the authelia CLI for a specified duration.
func (m *AutheliaBanManager) BanIP(ip, reason, duration string) error {
	args := []string{
		"storage", "bans", "ip", "add",
		ip,
	}

	if reason != "" {
		args = append(args, "--reason", reason)
	}

	if duration != "" {
		args = append(args, "--duration", duration)
	}

	if m.ConfigPath != "" {
		args = append(args, "--config", m.ConfigPath)
	}

	if m.EncryptionKey != "" {
		args = append(args, "--encryption-key", m.EncryptionKey)
	}

	if m.SQLitePath != "" {
		args = append(args, "--sqlite.path", m.SQLitePath)
	}

	cmd := exec.Command(m.BinaryPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("authelia ban ip failed: %w\noutput: %s", err, string(output))
	}

	return nil
}

// ListBans retrieves current IP bans via the authelia CLI.
func (m *AutheliaBanManager) ListBans() (string, error) {
	args := []string{
		"storage", "bans", "ip", "list",
	}

	if m.ConfigPath != "" {
		args = append(args, "--config", m.ConfigPath)
	}

	if m.EncryptionKey != "" {
		args = append(args, "--encryption-key", m.EncryptionKey)
	}

	if m.SQLitePath != "" {
		args = append(args, "--sqlite.path", m.SQLitePath)
	}

	cmd := exec.Command(m.BinaryPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("authelia list bans failed: %w\noutput: %s", err, string(output))
	}

	return string(output), nil
}

// RevokeBan removes an IP ban via the authelia CLI.
func (m *AutheliaBanManager) RevokeBan(ip string) error {
	args := []string{
		"storage", "bans", "ip", "revoke",
		ip,
	}

	if m.ConfigPath != "" {
		args = append(args, "--config", m.ConfigPath)
	}

	if m.EncryptionKey != "" {
		args = append(args, "--encryption-key", m.EncryptionKey)
	}

	if m.SQLitePath != "" {
		args = append(args, "--sqlite.path", m.SQLitePath)
	}

	cmd := exec.Command(m.BinaryPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("authelia revoke ban failed: %w\noutput: %s", err, string(output))
	}

	return nil
}

// ValidateBanDuration parses and validates a duration string for Authelia.
// Supports Go duration strings ("1h", "30m", "24h") and day-based ("7d", "30d").
// Returns the validated duration string or an error.
func ValidateBanDuration(duration string) error {
	if duration == "" {
		return nil
	}

	// Convert "d" (days) to hours since Go's ParseDuration doesn't support days
	normalized := duration
	if len(duration) > 1 && duration[len(duration)-1] == 'd' {
		daysStr := duration[:len(duration)-1]
		days, err := strconv.ParseFloat(daysStr, 64)
		if err != nil {
			return fmt.Errorf("invalid duration %q: bad day format", duration)
		}
		if days <= 0 {
			return fmt.Errorf("invalid duration %q: must be positive", duration)
		}
		normalized = fmt.Sprintf("%fh", days*24)
	}

	d, err := time.ParseDuration(normalized)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", duration, err)
	}
	if d <= 0 {
		return fmt.Errorf("invalid duration %q: must be positive", duration)
	}
	return nil
}
