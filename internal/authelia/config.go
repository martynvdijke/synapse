package authelia

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseConfig reads and parses an Authelia configuration.yml file,
// extracting the access_control section.
func ParseConfig(path string) (*AccessControlConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read authelia config: %w", err)
	}

	var cfg AutheliaConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse authelia config: %w", err)
	}

	if cfg.AccessControl == nil {
		return nil, fmt.Errorf("authelia config has no access_control section")
	}

	return cfg.AccessControl, nil
}

// WriteConfig writes an Authelia config with the given access_control rules.
// It reads the existing config, replaces the access_control section, and writes back.
// This preserves all other sections of the config file.
func WriteConfig(path string, ac *AccessControlConfig) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read authelia config for write: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse authelia config for write: %w", err)
	}

	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return fmt.Errorf("invalid YAML document")
	}

	root := doc.Content[0]

	// Build the access_control node
	acNode, err := accessControlToYAMLNode(ac)
	if err != nil {
		return fmt.Errorf("build access_control yaml: %w", err)
	}

	// Find and replace the access_control key in the root mapping
	setOrAddMappingKey(root, "access_control", acNode)

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshal updated config: %w", err)
	}

	return os.WriteFile(path, out, 0644)
}

// GetDomains extracts all domain strings from access control rules.
// It handles both string and []string domain fields.
func GetDomains(ac *AccessControlConfig) []string {
	if ac == nil {
		return nil
	}

	seen := make(map[string]bool)
	var domains []string

	for _, rule := range ac.Rules {
		for _, d := range extractDomains(rule.Domain) {
			if !seen[d] {
				seen[d] = true
				domains = append(domains, d)
			}
		}
	}

	return domains
}

// extractDomains converts the domain field (string or []interface{}) to []string.
func extractDomains(domain interface{}) []string {
	if domain == nil {
		return nil
	}

	switch v := domain.(type) {
	case string:
		return []string{v}
	case []interface{}:
		var result []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case []string:
		return v
	default:
		return nil
	}
}

// DomainMatches checks if a CNAME matches any of the given Authelia domain rules.
// Supports wildcard prefix matching (*.example.com matches foo.example.com).
// Returns the matching domain rule if found, empty string otherwise.
// When multiple wildcards match, returns the most specific (longest) match.
func DomainMatches(cname string, domains []string) string {
	var bestMatch string

	for _, d := range domains {
		if strings.HasPrefix(d, "*.") {
			suffix := d[1:] // ".example.com"
			if strings.HasSuffix(cname, suffix) && len(d) > len(bestMatch) {
				bestMatch = d
			}
		} else if d == cname {
			return d // exact match is always best
		}

		// Also check with wildcard at end (less common but supported by some setups)
		if strings.HasSuffix(d, ".*") {
			prefix := d[:len(d)-2] // "prefix."
			if strings.HasPrefix(cname, prefix) && len(d) > len(bestMatch) {
				bestMatch = d
			}
		}
	}

	return bestMatch
}

// accessControlToYAMLNode converts an AccessControlConfig to a yaml.Node.
func accessControlToYAMLNode(ac *AccessControlConfig) (*yaml.Node, error) {
	var buf strings.Builder
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)

	// Build a temporary struct for encoding
	tmp := map[string]interface{}{
		"access_control": ac,
	}

	if err := encoder.Encode(tmp); err != nil {
		return nil, err
	}
	encoder.Close()

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(buf.String()), &doc); err != nil {
		return nil, err
	}

	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, fmt.Errorf("invalid generated YAML")
	}

	root := doc.Content[0]
	return findMappingValue(root, "access_control"), nil
}

// setOrAddMappingKey sets or adds a key-value pair in a YAML mapping node.
func setOrAddMappingKey(mapping *yaml.Node, key string, value *yaml.Node) {
	if mapping.Kind != yaml.MappingNode {
		return
	}

	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = value
			return
		}
	}

	// Key not found, append it
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		value,
	)
}

// findMappingValue finds the value node for a given key in a mapping node.
func findMappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}
