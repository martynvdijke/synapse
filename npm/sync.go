package npm

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"pulsenode/kumasync"
)

type ProxyHost struct {
	ID           int              `json:"id"`
	DomainNames  []string         `json:"domain_names"`
	Forwarding   ForwardingConfig `json:"forwarding"`
}

type ForwardingConfig struct {
	Container string `json:"container"`
	Protocol   string `json:"protocol"`
}

func GetProxyHosts(npmHost, npmUser, npmPass string) ([]ProxyHost, error) {
	url := fmt.Sprintf("%s/api/nginx/proxy-hosts", npmHost)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(npmUser, npmPass)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get proxy hosts: status %d", resp.StatusCode)
	}

	var hosts []ProxyHost
	if err := json.NewDecoder(resp.Body).Decode(&hosts); err != nil {
		return nil, err
	}

	return hosts, nil
}

func GetCNameToContainerMapping(npmHost, npmUser, npmPass string) ([]map[string]string, error) {
	hosts, err := GetProxyHosts(npmHost, npmUser, npmPass)
	if err != nil {
		return nil, err
	}

	cnames := []map[string]string{}
	for _, host := range hosts {
		domain := host.DomainNames
		forwarding := host.Forwarding
		if len(domain) == 0 || forwarding.Container == "" {
			continue
		}

		container := forwarding.Container
		for _, d := range domain {
			cnames = append(cnames, map[string]string{
				"cname":     d,
				"container": container,
			})
		}
	}

	return cnames, nil
}

func SyncNPM(npmHost, npmUser, npmPass, kumaURL, kumaUser, kumaPass string,
	parentDomain string, dryRun bool, gotifyURL, gotifyToken string) kumasync.SyncResult {

	proxies, err := GetCNameToContainerMapping(npmHost, npmUser, npmPass)
	if err != nil {
		slog.Error("Failed to get NPM proxy hosts", "host", npmHost, "error", err)
		return kumasync.SyncResult{}
	}

	if len(proxies) == 0 {
		slog.Warn("No proxy hosts found in NPM", "host", npmHost)
		return kumasync.SyncResult{}
	}

	slog.Debug("Discovered proxy hosts", "count", len(proxies))

	if dryRun {
		added := 0
		for _, proxy := range proxies {
			cname := proxy["cname"]
			slog.Info("Would add HTTP monitor", "name", cname, "url", fmt.Sprintf("http://%s", cname))
			added++
		}
		return kumasync.SyncResult{Added: added}
	}

	api := kumasync.NewKumaAPI(kumaURL)
	if err := api.Login(kumaUser, kumaPass); err != nil {
		slog.Error("Uptime Kuma login failed", "url", kumaURL, "error", err)
		return kumasync.SyncResult{}
	}

	existingMonitors, err := api.GetMonitors()
	if err != nil {
		slog.Error("Failed to get existing monitors", "error", err)
		return kumasync.SyncResult{}
	}

	existing := make(map[string]bool)
	for _, m := range existingMonitors {
		existing[m.Name] = true
	}
	slog.Debug("Existing monitors", "count", len(existing))

	added := 0
	skipped := 0
	newMonitors := []string{}

	for _, proxy := range proxies {
		cname := proxy["cname"]
		monitorName := cname

		if parentDomain != "" {
			prefix := parentDomain + "."
			if len(cname) > len(prefix) && cname[:len(prefix)] == prefix {
				monitorName = cname[len(prefix):]
			}
		}

		if existing[monitorName] {
			slog.Debug("Already exists, skipping", "name", monitorName)
			skipped++
			continue
		}

		slog.Info("Adding HTTP monitor", "name", monitorName, "url", fmt.Sprintf("http://%s", cname))
		api.AddMonitor("http", monitorName, fmt.Sprintf("http://%s", cname), "", 0, 60, 60, 3)
		newMonitors = append(newMonitors, monitorName)
		added++
	}

	if gotifyURL != "" && gotifyToken != "" && added > 0 {
		msg := fmt.Sprintf("Added %d new NPM proxy monitors:\n", added)
		for i, m := range newMonitors {
			if i >= 10 {
				break
			}
			msg += fmt.Sprintf("• %s\n", m)
		}
		if len(newMonitors) > 10 {
			msg += fmt.Sprintf("... and %d more\n", len(newMonitors)-10)
		}
		gotify := kumasync.NewGotifyClient(gotifyURL, gotifyToken)
		gotify.SendAlert("New NPM Monitors Added", msg, 5)
	}

	return kumasync.SyncResult{
		Added:       added,
		Skipped:     skipped,
		NewMonitors: newMonitors,
	}
}
