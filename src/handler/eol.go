package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	eolCacheKey = "eol_cache:all_packages"
	eolCacheTTL = 7 * 24 * time.Hour
	// Used instead of eolCacheTTL when some products failed to fetch, so they are retried soon.
	eolCachePartialTTL = time.Hour
)

var eolAPIBaseURL = "https://endoflife.date/api"

// Products tracked on endoflife.date. Shared by package and Helm chart lookups,
// both of which read the same cache document.
var supportedEOLProducts = []string{
	"redis",
	"memcached",
	"mongodb",
	"mysql",
	"rabbitmq",
	"envoy",
	"debian",
	"postgresql",
	"elasticsearch",
	"php",
	"gitlab-runner",
	"linux",
	"openbao",
	"metallb",
	"authentik",
	"argo-cd",
}

// Products not on endoflife.date are reported as unknown without touching the cache.
func queryEndOfLifeAPI(productName string, ctx context.Context, con *redis.Client) (string, string, error) {
	response, err := getEOLData(ctx, con, productName)
	if err != nil {
		if err := updateEOLCache(ctx, con); err != nil {
			return "", "", fmt.Errorf("failed to update cache: %w", err)
		}
		response, err = getEOLData(ctx, con, productName)
		if err != nil {
			return "", "", fmt.Errorf("failed to retrieve updated cache for %s: %w", productName, err)
		}
	}

	latestVersion := "unknown"
	eolDate := "false"

	for _, entry := range response {
		if entry.Cycle == productName {
			latestVersion = extractMajorMinor(entry.Latest)
			eolDate = string(entry.EOL)
			break
		}
	}

	if latestVersion == "unknown" && len(response) > 0 {
		latestVersion = extractMajorMinor(response[0].Latest)
		eolDate = string(response[0].EOL)
	}

	return latestVersion, eolDate, nil
}

func updateEOLCache(ctx context.Context, con *redis.Client) error {
	products := map[string][]EndOfLifeEntry{}
	ttl := eolCacheTTL

	httpClient := &http.Client{Timeout: 10 * time.Second}
	for _, productName := range supportedEOLProducts {
		apiURL := fmt.Sprintf("%s/%s.json", eolAPIBaseURL, productName)
		entries, err := fetchEOLEntries(httpClient, apiURL)
		if err != nil {
			log.Printf("Failed to fetch EOL data for %s: %v", productName, err)
			ttl = eolCachePartialTTL
			continue
		}
		products[productName] = entries
	}

	data, err := json.Marshal(map[string]interface{}{"package": products})
	if err != nil {
		return fmt.Errorf("failed to marshal updated cache: %w", err)
	}

	err = con.Set(ctx, eolCacheKey, data, ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to update cache in Redis: %w", err)
	}

	return nil
}

// Returns an error only when the cache is missing or unreadable, which means it must be rebuilt.
// A product absent from a valid cache yields (nil, nil).
func getEOLData(ctx context.Context, con *redis.Client, productName string) ([]EndOfLifeEntry, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cachedData, err := con.Get(ctxWithTimeout, eolCacheKey).Result()
	if err == redis.Nil {
		return nil, fmt.Errorf("cache miss")
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch cache: %w", err)
	}

	var cacheDocument struct {
		Package map[string][]EndOfLifeEntry `json:"package"`
	}
	if err := json.Unmarshal([]byte(cachedData), &cacheDocument); err != nil {
		return nil, fmt.Errorf("failed to parse cached data: %w", err)
	}
	if cacheDocument.Package == nil {
		return nil, fmt.Errorf("invalid cache format: missing 'package' key")
	}

	return cacheDocument.Package[productName], nil
}

func extractMajorMinor(version string) string {
	parts := strings.Split(version, ":")
	if len(parts) > 1 {
		version = parts[1]
	}

	segments := strings.Split(version, ".")
	if len(segments) >= 2 {
		return fmt.Sprintf("%s.%s", segments[0], segments[1])
	}
	return segments[0]
}

func fetchEOLEntries(client *http.Client, url string) ([]EndOfLifeEntry, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %s", resp.Status)
	}

	var entries []EndOfLifeEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func isVersionExpired(current, newest string) bool {
	if newest == "unknown" {
		return false
	}

	parseVersion := func(label, version string) (int, int) {
		segments := strings.Split(version, ".")
		major, err := strconv.Atoi(segments[0])
		if err != nil {
			log.Printf("Can't parse %s version major segment %q: %v", label, version, err)
		}
		minor := 0
		if len(segments) > 1 {
			minor, err = strconv.Atoi(segments[1])
			if err != nil {
				log.Printf("Can't parse %s version minor segment %q: %v", label, version, err)
			}
		}
		return major, minor
	}

	currentMajor, currentMinor := parseVersion("current", current)
	newestMajor, newestMinor := parseVersion("newest", newest)

	if currentMajor < newestMajor {
		return true
	} else if currentMajor == newestMajor && currentMinor < newestMinor {
		return true
	}
	return false
}
