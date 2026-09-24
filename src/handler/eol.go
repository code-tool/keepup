package handler

import (
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "strconv"
    "strings"
)

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

	var entries []EndOfLifeEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func isVersionExpired(current, newest string) bool {
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
