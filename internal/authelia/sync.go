package authelia

import (
	"fmt"
	"sort"

	"synapse/internal/npm"
)

// CompareCNAMEs compares NPM CNAMEs against Authelia domains.
// Returns lists of matched and missing CNAMEs.
func CompareCNAMEs(npmCNAMEs []string, autheliaDomains []string) (matched, missing []string) {
	matchedSet := make(map[string]bool)

	for _, cname := range npmCNAMEs {
		if DomainMatches(cname, autheliaDomains) != "" {
			matchedSet[cname] = true
		}
	}

	for _, cname := range npmCNAMEs {
		if matchedSet[cname] {
			matched = append(matched, cname)
		} else {
			missing = append(missing, cname)
		}
	}

	sort.Strings(matched)
	sort.Strings(missing)
	return
}

// SyncConfig syncs NPM proxy entries into Authelia's access_control config.
// If dryRun is true, it returns the planned actions without modifying the file.
// overrides maps CNAME -> policy (e.g., "bypass", "one_factor", "two_factor", "deny").
// If autoSync is false, only alerts are returned (no config modification).
func SyncConfig(autheliaPath, dbPath string, npmEntries []npm.ProxyEntry, defaultPolicy string, overrides map[string]string, autoSync, dryRun bool) ([]SyncAction, error) {
	if defaultPolicy == "" {
		defaultPolicy = DefaultPolicy
	}

	// Parse current Authelia config
	ac, err := ParseConfig(autheliaPath)
	if err != nil {
		return nil, fmt.Errorf("parse authelia config: %w", err)
	}

	existingDomains := GetDomains(ac)
	existingSet := make(map[string]bool)
	for _, d := range existingDomains {
		existingSet[d] = true
	}

	var actions []SyncAction

	for _, entry := range npmEntries {
		cname := entry.CNAME
		container := entry.Container

		// Check if any existing rule already covers this CNAME
		matchedDomain := DomainMatches(cname, existingDomains)

		if matchedDomain != "" {
			actions = append(actions, SyncAction{
				CNAME:     cname,
				Container: container,
				Action:    "skip",
				Policy:    matchedDomain,
				Message:   fmt.Sprintf("Covered by rule for %s", matchedDomain),
			})
			continue
		}

		if !autoSync {
			// Auto-sync disabled: create alert
			actions = append(actions, SyncAction{
				CNAME:     cname,
				Container: container,
				Action:    "alert",
				Message:   "CNAME not found in Authelia access_control rules (auto-sync disabled)",
			})
			continue
		}

		// Auto-sync enabled: add the CNAME to config
		policy := defaultPolicy
		if override, ok := overrides[cname]; ok && override != "" {
			policy = override
		}

		if dryRun {
			actions = append(actions, SyncAction{
				CNAME:     cname,
				Container: container,
				Action:    "add",
				Policy:    policy,
				Message:   fmt.Sprintf("Would add rule for %s with policy %s", cname, policy),
			})
			continue
		}

		// Add the rule to the config
		ac.Rules = append(ac.Rules, AccessRule{
			Domain: cname,
			Policy: policy,
		})

		actions = append(actions, SyncAction{
			CNAME:     cname,
			Container: container,
			Action:    "add",
			Policy:    policy,
			Message:   fmt.Sprintf("Added rule for %s with policy %s", cname, policy),
		})
	}

	// Only write if there are changes and we're not in dry-run mode
	if autoSync && !dryRun && hasAddActions(actions) {
		if err := WriteConfig(autheliaPath, ac); err != nil {
			return actions, fmt.Errorf("write authelia config: %w", err)
		}
	}

	return actions, nil
}

// hasAddActions checks if any action is an "add" type.
func hasAddActions(actions []SyncAction) bool {
	for _, a := range actions {
		if a.Action == "add" {
			return true
		}
	}
	return false
}

// EnsureDomainRules ensures Authelia access_control rules exist for the given
// entries, creating any that are missing. It forces autoSync on so missing
// entries are added (with dryRun, "add" actions are reported as "Would add..."
// and the config is not written).
func EnsureDomainRules(autheliaPath, dbPath string, entries []npm.ProxyEntry, defaultPolicy string, overrides map[string]string, dryRun bool) ([]SyncAction, error) {
	return SyncConfig(autheliaPath, dbPath, entries, defaultPolicy, overrides, true, dryRun)
}
