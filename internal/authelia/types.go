package authelia

// AccessControlConfig represents the access_control section of Authelia's configuration.yml.
type AccessControlConfig struct {
	DefaultPolicy string       `yaml:"default_policy"`
	Rules         []AccessRule `yaml:"rules"`
}

// AccessRule represents a single access control rule in Authelia config.
type AccessRule struct {
	Domain      any      `yaml:"domain,omitempty"`       // string or []string
	DomainRegex any      `yaml:"domain_regex,omitempty"` // string or []string
	Policy      string   `yaml:"policy"`
	Subject     any      `yaml:"subject,omitempty"` // string or []string
	Resources   []string `yaml:"resources,omitempty"`
	Networks    []string `yaml:"networks,omitempty"`
}

// AutheliaConfig is the top-level structure of Authelia's configuration.yml.
// Only fields relevant to Synapse are included.
type AutheliaConfig struct {
	AccessControl *AccessControlConfig `yaml:"access_control"`
}

// SyncAction describes what Synapse did (or would do) during authelia sync.
type SyncAction struct {
	CNAME     string `json:"cname"`
	Container string `json:"container,omitempty"`
	Action    string `json:"action"` // "add", "skip", "alert", "error"
	Policy    string `json:"policy,omitempty"`
	Message   string `json:"message,omitempty"`
}

// AlertSeverity indicates how serious an alert is.
type AlertSeverity string

const (
	AlertWarning AlertSeverity = "warning"
	AlertError   AlertSeverity = "error"
)

// TempAccessRule represents a temporary IP access rule managed by Synapse.
type TempAccessRule struct {
	ID        int64  `json:"id"`
	IP        string `json:"ip"`
	Reason    string `json:"reason"`
	ExpiresAt string `json:"expires_at"` // RFC3339 string
	CreatedAt string `json:"created_at"`
	Status    string `json:"status"` // "active", "expired", "revoked"
}

// AutheliaAlert represents a Synapse alert about Authelia coverage.
type AutheliaAlert struct {
	ID        int64         `json:"id"`
	CNAME     string        `json:"cname"`
	Message   string        `json:"message"`
	Severity  AlertSeverity `json:"severity"`
	Status    string        `json:"status"` // "open", "resolved"
	CreatedAt string        `json:"created_at"`
}

// DefaultPolicy is the default policy for auto-synced CNAME rules.
const DefaultPolicy = "one_factor"
