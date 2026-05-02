package main

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"kuma-sync/docker"
	"kuma-sync/npm"
)

var defaultKumaURL = getEnv("KUMA_URL", "http://uptime-kuma:3001")
var defaultKumaUser = getEnv("KUMA_USER", "martynvandijke")
var defaultKumaPass = getEnv("KUMA_PASS", "")

var defaultNPMHost = getEnv("NPM_HOST", "http://nginx:81")
var defaultNPMUser = getEnv("NPM_USER", "admin")
var defaultNMPass = getEnv("NPM_PASS", "admin")

var defaultDockerCompose = getEnv("COMPOSE_PATH", "docker-compose.yml")

var defaultGotifyURL = getEnv("GOTIFY_URL", "http://gotify:80")
var defaultGotifyToken = getEnv("GOTIFY_TOKEN", "")

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func main() {
	cmd := &cli.Command{
		Name:        "kuma-sync",
		Description: "Sync docker-compose containers and NPM proxy hosts to Uptime Kuma",
		Version:    "1.0.0",
		Commands: []*cli.Command{
			{
				Name:  "docker",
				Usage: "Sync docker-compose containers to Uptime Kuma",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "compose, c", DefaultText: defaultDockerCompose, Usage: "Path to docker-compose.yml"},
					&cli.StringFlag{Name: "kuma-url", DefaultText: defaultKumaURL, Usage: "Uptime Kuma URL"},
					&cli.StringFlag{Name: "kuma-user", DefaultText: defaultKumaUser, Usage: "Uptime Kuma username"},
					&cli.StringFlag{Name: "kuma-pass", DefaultText: defaultKumaPass, Usage: "Uptime Kuma password"},
					&cli.StringFlag{Name: "docker-host", Usage: "Docker host name in Uptime Kuma"},
					&cli.BoolFlag{Name: "dry-run", Usage: "Print actions without executing"},
					&cli.StringFlag{Name: "docker-socket", DefaultText: "/var/run/docker.sock", Usage: "Path to Docker socket"},
					&cli.StringFlag{Name: "gotify-url", Usage: "Gotify URL for alerts"},
					&cli.StringFlag{Name: "gotify-token", Usage: "Gotify app token"},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					result := docker.SyncContainers(
						cmd.String("compose"),
						cmd.String("kuma-url"),
						cmd.String("kuma-user"),
						cmd.String("kuma-pass"),
						cmd.String("docker-host"),
						cmd.String("docker-socket"),
						cmd.Bool("dry-run"),
						cmd.String("gotify-url"),
						cmd.String("gotify-token"),
					)
					fmt.Printf("\nDocker sync done: %d added, %d skipped\n", result.Added, result.Skipped)
					return nil
				},
			},
			{
				Name:  "npm",
				Usage: "Sync NPM proxy hosts to Uptime Kuma",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "npm-host", DefaultText: defaultNPMHost, Usage: "Nginx Proxy Manager host"},
					&cli.StringFlag{Name: "npm-user", DefaultText: defaultNPMUser, Usage: "NPM username"},
					&cli.StringFlag{Name: "npm-pass", DefaultText: defaultNMPass, Usage: "NPM password"},
					&cli.StringFlag{Name: "kuma-url", DefaultText: defaultKumaURL, Usage: "Uptime Kuma URL"},
					&cli.StringFlag{Name: "kuma-user", DefaultText: defaultKumaUser, Usage: "Uptime Kuma username"},
					&cli.StringFlag{Name: "kuma-pass", DefaultText: defaultKumaPass, Usage: "Uptime Kuma password"},
					&cli.StringFlag{Name: "parent-domain", Usage: "Parent domain to strip from CNAMEs"},
					&cli.BoolFlag{Name: "dry-run", Usage: "Print actions without executing"},
					&cli.StringFlag{Name: "gotify-url", Usage: "Gotify URL for alerts"},
					&cli.StringFlag{Name: "gotify-token", Usage: "Gotify app token"},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					result := npm.SyncNPM(
						cmd.String("npm-host"),
						cmd.String("npm-user"),
						cmd.String("npm-pass"),
						cmd.String("kuma-url"),
						cmd.String("kuma-user"),
						cmd.String("kuma-pass"),
						cmd.String("parent-domain"),
						cmd.Bool("dry-run"),
						cmd.String("gotify-url"),
						cmd.String("gotify-token"),
					)
					fmt.Printf("\nNPM sync done: %d added, %d skipped\n", result.Added, result.Skipped)
					return nil
				},
			},
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}