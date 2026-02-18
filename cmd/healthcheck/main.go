package main

import (
	"net/http"
	"os"
)

func main() {
	url := os.Getenv("HEALTH_URL")
	if url == "" {
		url = "http://localhost:8080/health"
	}
	resp, err := http.Get(url)
	if err != nil || resp.StatusCode != http.StatusOK {
		os.Exit(1)
	}
}
