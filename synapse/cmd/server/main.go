package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"synapse/internal/db"
	"synapse/internal/handler"
)

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	composePath := getEnv("COMPOSE_PATH", "docker-compose.yml")
	kumaURL := getEnv("KUMA_URL", "http://uptime-kuma:3001")
	kumaUser := getEnv("KUMA_USER", "admin")
	kumaPass := getEnv("KUMA_PASS", "")
	dbPath := getEnv("DB_PATH", "synapse.db")
	addr := getEnv("LISTEN_ADDR", ":8080")

	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	mux := http.NewServeMux()
	h := handler.New(database, composePath, kumaURL, kumaUser, kumaPass)
	h.Register(mux)

	fmt.Printf("synapse listening on %s\n", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
