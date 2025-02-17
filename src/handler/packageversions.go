package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v9"
	"github.com/google/uuid"
)

const UUIDSuffix = "PACKAGE_UUID"

type PackageDetail struct {
	CurrentVersion    string `json:"current_version"`
	CurrentVersionEoF string `json:"current_version_eof"`
	NewestVersion     string `json:"newest_version"`
	Expired           bool   `json:"expired"`
}

type PackageVersions struct {
	IDPkg         uuid.UUID                `json:"id"`
	DataCenterPkg string                   `json:"data_center"`
	HostIPPkg     string                   `json:"host_ip"`
	UpdatedAt     string                   `json:"updated_at"`
	Packages      map[string]PackageDetail `json:"packages"`
}

type PackageVersionss struct {
	Items map[uuid.UUID]PackageVersions
}

type EOL string

func (e *EOL) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		*e = EOL(str)
		return nil
	}

	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		if b {
			*e = EOL("true")
		} else {
			*e = EOL("false")
		}
		return nil
	}

	return fmt.Errorf("invalid EOL value")
}

type EndOfLifeEntry struct {
	Cycle             string `json:"cycle"`
	EOL               EOL    `json:"eol"`
	Latest            string `json:"latest"`
	LatestReleaseDate string `json:"latestReleaseDate"`
}

var (
	ErrInsertFailedPackage  = errors.New("Insert failed")
	ErrMarshalFailedPackage = errors.New("Marshal failed")
	ErrIDNotFoundPackage    = errors.New("ID not found")
)

func (c *PackageVersionss) Insert(
	pkg PackageVersions,
	ctx context.Context,
	con *redis.Client,
	queryFunc func(string) (string, string, error),
	ttl int,
) (uuid.UUID, error) {

	updatedPackages := make(map[string]PackageDetail)

	for name, versionDetail := range pkg.Packages {
		if versionDetail.CurrentVersion == "unknown" || versionDetail.CurrentVersion == "" {
			continue
		}

		currentVersion := extractMajorMinor(versionDetail.CurrentVersion)
		latestVersion, eolDate, err := queryFunc(name)
		if err != nil {
			latestVersion = "unknown"
		} else {
			latestVersion = extractMajorMinor(latestVersion)
		}

		if eolDate == "" {
			eolDate = "false"
		}

		expired := isVersionExpired(currentVersion, latestVersion)

		updatedPackages[name] = PackageDetail{
			CurrentVersion:    currentVersion,
			CurrentVersionEoF: eolDate,
			NewestVersion:     latestVersion,
			Expired:           expired,
		}
	}

	pkg.Packages = updatedPackages
	pkg.IDPkg = UUIDFromDcAndIPPackage(pkg.DataCenterPkg, pkg.HostIPPkg)
	pkg.UpdatedAt = fmt.Sprint(time.Now().Unix())

	data, err := json.Marshal(pkg)
	if err != nil {
		return pkg.IDPkg, ErrMarshalFailedPackage
	}

	var result string
	result, err = con.Set(ctx, fmt.Sprint(pkg.IDPkg), data, time.Duration(ttl)*time.Second).Result()
	if err != nil {
		return pkg.IDPkg, ErrInsertFailedPackage
	}
	log.Printf("Creating %s: %s", pkg.IDPkg, result)
	return pkg.IDPkg, nil
}

func (c *PackageVersionss) Retrieve(id uuid.UUID, ctx context.Context, con *redis.Client) (PackageVersions, error) {
	result, err := con.Get(ctx, fmt.Sprint(id)).Result()
	if err != nil {
		return PackageVersions{}, ErrIDNotFoundPackage
	}

	pkg := PackageVersions{}
	err = json.Unmarshal([]byte(result), &pkg)
	if err != nil {
		return PackageVersions{}, ErrMarshalFailedPackage
	}
	return pkg, nil
}

func (c *PackageVersionss) Scan(ctx context.Context, con *redis.Client) (PackageVersionss, error) {
	var pkgs = PackageVersionss{
		Items: make(map[uuid.UUID]PackageVersions),
	}

	iter := con.Scan(ctx, 0, "*", 0).Iterator()
	for iter.Next(ctx) {
		uid, err := uuid.Parse(iter.Val())
		if err != nil {
			continue
		}
		pkgs.Items[uid], _ = c.Retrieve(uid, ctx, con)
	}
	return pkgs, nil
}

func UUIDFromDcAndIPPackage(dc string, ip string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceDNS, []byte(fmt.Sprintf("%s-%s-%s", dc, ip, UUIDSuffix)))
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

func queryEndOfLifeAPI(packageName string, ctx context.Context, con *redis.Client) (string, string, error) {
	response, err := getEOLData(ctx, con, packageName)
	if err != nil {
		if err := updateEOLCache(ctx, con); err != nil {
			return "", "", fmt.Errorf("failed to update cache: %w", err)
		}
		response, err = getEOLData(ctx, con, packageName)
		if err != nil {
			return "", "", fmt.Errorf("failed to retrieve updated cache for %s: %w", packageName, err)
		}
	}

	latestVersion := "unknown"
	eolDate := "false"

	for _, entry := range response {
		if entry.Cycle == packageName {
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
	key := "eol_cache:all_packages"
	ttl := 7 * 24 * time.Hour
	//TODO: Handle all related packages.
	//Option 1: Get all data from endoflife and store in redis.
	//Option 2: Dynamicly resolve pacakge names, but should be checked fro eof api side.
	supportedPackages := []string{"redis", "memcached", "mongodb", "mysql", "rabbitmq", "envoy", "debian", "postgresql", "elasticsearch"}

	cacheDocument := map[string]interface{}{
		"package": map[string][]EndOfLifeEntry{},
	}

	for _, packageName := range supportedPackages {
		apiURL := fmt.Sprintf("https://endoflife.date/api/%s.json", packageName)
		httpClient := &http.Client{Timeout: 10 * time.Second}

		resp, err := httpClient.Get(apiURL)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		var entries []EndOfLifeEntry
		if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
			continue
		}

		cacheDocument["package"].(map[string][]EndOfLifeEntry)[packageName] = entries
	}

	data, err := json.Marshal(cacheDocument)
	if err != nil {
		return fmt.Errorf("failed to marshal updated cache: %w", err)
	}

	err = con.Set(ctx, key, data, ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to update cache in Redis: %w", err)
	}

	return nil
}

func getEOLData(ctx context.Context, con *redis.Client, packageName string) ([]EndOfLifeEntry, error) {
	key := "eol_cache:all_packages"

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cachedData, err := con.Get(ctxWithTimeout, key).Result()
	if err == redis.Nil {
		return nil, fmt.Errorf("cache miss")
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch cache: %w", err)
	}

	var cacheDocument map[string]interface{}
	if err := json.Unmarshal([]byte(cachedData), &cacheDocument); err != nil {
		return nil, fmt.Errorf("failed to parse cached data: %w", err)
	}

	packages, ok := cacheDocument["package"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid cache format: missing 'package' key")
	}

	rawData, exists := packages[packageName]
	if !exists {
		return nil, fmt.Errorf("no data found for package: %s", packageName)
	}

	rawBytes, _ := json.Marshal(rawData)
	var result []EndOfLifeEntry

	if err := json.Unmarshal(rawBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal raw package data: %w", err)
	}

	return result, nil
}

func isVersionExpired(current, newest string) bool {
	parseVersion := func(version string) (int, int) {
		segments := strings.Split(version, ".")
		major, _ := strconv.Atoi(segments[0])
		minor := 0
		if len(segments) > 1 {
			minor, _ = strconv.Atoi(segments[1])
		}
		return major, minor
	}

	currentMajor, currentMinor := parseVersion(current)
	newestMajor, newestMinor := parseVersion(newest)

	if currentMajor < newestMajor {
		return true
	} else if currentMajor == newestMajor && currentMinor < newestMinor {
		return true
	}
	return false
}
