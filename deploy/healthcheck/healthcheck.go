// healthcheck is a minimal static binary that sends an HTTP GET to the
// local furnace-web server's /v1/health endpoint and exits 0 on success.
// Port defaults to 8080 unless HEALTHCHECK_PORT is set.
package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	port := os.Getenv("HEALTHCHECK_PORT")
	if port == "" {
		port = "8080"
	}

	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
		},
	}

	resp, err := client.Get("http://localhost:" + port + "/v1/health")
	if err != nil {
		fmt.Fprintf(os.Stderr, "health: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusBadRequest {
		os.Exit(0)
	}

	fmt.Fprintf(os.Stderr, "health: status %d\n", resp.StatusCode)
	os.Exit(1)
}
