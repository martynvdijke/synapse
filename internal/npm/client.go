package npm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"synapse/internal/logging"
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
	Protocol  string `json:"protocol"`
}

type ProxyEntry struct {
	CNAME     string `json:"cname"`
	Container string `json:"container"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Protocol  string `json:"protocol"`
}

var npmTracer = otel.Tracer("npm")

func GetProxyHosts(npmHost, npmUser, npmPass string) ([]ProxyEntry, error) {
	_, span := npmTracer.Start(context.Background(), "GetProxyHosts",
		trace.WithAttributes(
			attribute.String("npm_host", npmHost),
		),
	)
	defer span.End()

	start := time.Now()
	logging.LogDebug("npm", "Fetching proxy hosts from NPM",
		slog.String("npm_host", npmHost),
	)

	url := fmt.Sprintf("%s/api/nginx/proxy-hosts", npmHost)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		logging.LogError("npm", "Failed to create NPM request",
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, err
	}
	req.SetBasicAuth(npmUser, npmPass)

	client := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   30 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		logging.LogError("npm", "Failed to fetch proxy hosts from NPM",
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("failed to get proxy hosts: status %d", resp.StatusCode)
		logging.LogError("npm", "NPM request returned non-OK status",
			slog.Int("status", resp.StatusCode),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, err
	}

	var hosts []ProxyHost
	if err := json.NewDecoder(resp.Body).Decode(&hosts); err != nil {
		logging.LogError("npm", "Failed to decode NPM proxy hosts response",
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
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
				Protocol:  forwarding.Protocol,
			})
		}
	}

	logging.LogInfo("npm", "Fetched proxy hosts from NPM",
		slog.Int("host_count", len(hosts)),
		slog.Int("entry_count", len(entries)),
		slog.Duration("duration", time.Since(start)),
	)

	return entries, nil
}
