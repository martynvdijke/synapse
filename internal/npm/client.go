package npm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type ProxyHost struct {
	ID          int              `json:"id"`
	DomainNames []string         `json:"domain_names"`
	Forwarding  ForwardingConfig `json:"forwarding"`
}

type ForwardingConfig struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Container string `json:"container"`
	Protocol   string `json:"protocol"`
}

type ProxyEntry struct {
	CNAME     string `json:"cname"`
	Container string `json:"container"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Protocol   string `json:"protocol"`
}

func GetProxyHosts(npmHost, npmUser, npmPass string) ([]ProxyEntry, error) {
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

	var entries []ProxyEntry
	for _, host := range hosts {
		forwarding := host.Forwarding
		if len(host.DomainNames) == 0 || forwarding.Container == "" {
			continue
		}

		for _, domain := range host.DomainNames {
			entries = append(entries, ProxyEntry{
				CNAME:     domain,
				Container: forwarding.Container,
				Host:      forwarding.Host,
				Port:      forwarding.Port,
				Protocol:   forwarding.Protocol,
			})
		}
	}

	return entries, nil
}
