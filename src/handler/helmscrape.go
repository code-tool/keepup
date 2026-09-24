package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type KubernetesCluster struct {
	ID          uuid.UUID       `json:"id"`
	ClusterName string          `json:"cluster_name"` // Default value from scraper: minikube
	KubeVersion string          `json:"kube_version"`
	TeamCluster string          `json:"team"`
	HelmCharts  []HelmChartData `json:"helm_charts"`
	UpdatedAt   string          `json:"updated_at"`
}

type HelmChartData struct {
	ChartName           string `json:"chart_name"`
	ChartVersion        string `json:"version"`
	ChartNamespace      string `json:"namespace"`
	ChartVersionEoF     string `json:"current_version_eof"`
	ChartNewestVersion  string `json:"newest_version"`
	ChartVersionExpired bool   `json:"expired"`
}

type KubernetesClusters struct {
	Items map[uuid.UUID]KubernetesCluster
}

var (
	ErrClusterInsertFailed  = errors.New("Cluster insert failed")
	ErrClusterMarshalFailed = errors.New("Cluster marshal failed")
	ErrClusterNotFound      = errors.New("Cluster ID not found")
)

func (c *KubernetesClusters) InsertClusterData(
    cluster KubernetesCluster,
    ctx context.Context,
    con *redis.Client,
    queryFunc func(string) (string, string, error),
    ttl int,
) (uuid.UUID, error) {

	updatedHelmCharts := make([]HelmChartData, 0, len(cluster.HelmCharts))

    for _, helmChart := range cluster.HelmCharts {
        if helmChart.ChartVersion == "unknown" || helmChart.ChartVersion == "" {
            continue
        }

        version := extractMajorMinor(helmChart.ChartVersion)

        latestVersion, eolDate, err := queryFunc(helmChart.ChartName)
        if err != nil {
            log.Printf("Failed to query EOL for Helm chart %s: %v", helmChart.ChartName, err)
            latestVersion = "unknown"
        } else {
            latestVersion = extractMajorMinor(latestVersion)
        }

        if eolDate == "" {
            eolDate = "false"
        }

        expired := isVersionExpired(version, latestVersion)

        updatedHelmCharts = append(updatedHelmCharts, HelmChartData{
            ChartName:           helmChart.ChartName,
            ChartVersion:        version,
            ChartNamespace:      helmChart.ChartNamespace,
            ChartVersionEoF:     eolDate,
            ChartNewestVersion:  latestVersion,
            ChartVersionExpired: expired,
        })
    }

    cluster.HelmCharts = updatedHelmCharts

	cluster.ID = UUIDFromClusterName(cluster.ClusterName)
	cluster.UpdatedAt = fmt.Sprint(time.Now().Unix())

	data, err := json.Marshal(cluster)
	if err != nil {
		return cluster.ID, ErrClusterMarshalFailed
	}

	_, err = con.Set(ctx, fmt.Sprint(cluster.ID), data, time.Duration(ttl)*time.Second).Result()
	if err != nil {
		return cluster.ID, ErrClusterInsertFailed
	}

	log.Printf("Cluster %s stored with ID: %s", cluster.ClusterName, cluster.ID)
	return cluster.ID, nil
}

func (c *KubernetesClusters) RetrieveCluster(id uuid.UUID, ctx context.Context, con *redis.Client) (KubernetesCluster, error) {
	data, err := con.Get(ctx, fmt.Sprint(id)).Result()
	if err != nil {
		return KubernetesCluster{}, ErrClusterNotFound
	}

	var cluster KubernetesCluster
	if err := json.Unmarshal([]byte(data), &cluster); err != nil {
		return KubernetesCluster{}, ErrClusterMarshalFailed
	}
	return cluster, nil
}

func (c *KubernetesClusters) ScanClusters(ctx context.Context, con *redis.Client) (KubernetesClusters, error) {
	clusters := KubernetesClusters{
		Items: make(map[uuid.UUID]KubernetesCluster),
	}

	var uids []uuid.UUID
	var keys []string
	iter := con.Scan(ctx, 0, "*", 0).Iterator()
	for iter.Next(ctx) {
		uid, err := uuid.Parse(iter.Val())
		if err != nil {
			if iter.Val() != "eol_cache:all_packages" {
				log.Printf("Cannot parse UUID: %s, %v", iter.Val(), err)
			}
			continue
		}
		uids = append(uids, uid)
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		log.Printf("Error scanning clusters: %v", err)
		return clusters, err
	}
	if len(keys) == 0 {
		return clusters, nil
	}

	values, err := con.MGet(ctx, keys...).Result()
	if err != nil {
		log.Printf("Error fetching clusters: %v", err)
		return clusters, err
	}
	for i, val := range values {
		if val == nil {
			// Key expired between SCAN and MGET.
			continue
		}
		str, ok := val.(string)
		if !ok {
			log.Printf("Unexpected value type for key %s", keys[i])
			continue
		}
		var cluster KubernetesCluster
		if err := json.Unmarshal([]byte(str), &cluster); err != nil {
			log.Printf("Can't unmarshal cluster %s: %v", keys[i], err)
			continue
		}
		clusters.Items[uids[i]] = cluster
	}

	return clusters, nil
}

// TODO
// Add more uniqe values to identife entity. To prevent names overlaping for different projects.
func UUIDFromClusterName(clusterName string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceDNS, []byte(clusterName))
}

func queryHelmChartEndOfLifeAPI(chartName string, ctx context.Context, con *redis.Client) (string, string, error) {
    response, err := getHelmChartEOLData(ctx, con, chartName)
    if err != nil {
        if err := updateHelmChartEOLCache(ctx, con); err != nil {
            return "", "", fmt.Errorf("failed to update Helm chart cache: %w", err)
        }
        response, err = getHelmChartEOLData(ctx, con, chartName)
        if err != nil {
            return "", "", fmt.Errorf("failed to retrieve updated Helm chart cache for %s: %w", chartName, err)
        }
    }

    latestVersion := "unknown"
    eolDate := "false"

    for _, entry := range response {
        if entry.Cycle == chartName {
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

// func extractMajorMinor(version string) string {
// 	parts := strings.Split(version, ":")
// 	if len(parts) > 1 {
// 		version = parts[1]
// 	}
//
// 	segments := strings.Split(version, ".")
// 	if len(segments) >= 2 {
// 		return fmt.Sprintf("%s.%s", segments[0], segments[1])
// 	}
// 	return segments[0]
// }

func updateHelmChartEOLCache(ctx context.Context, con *redis.Client) error {
	key := "eol_cache:all_packages"
	ttl := 7 * 24 * time.Hour

	supportedHelmCharts := []string{
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

	cacheDocument := map[string]interface{}{
		"helm_chart": map[string][]EndOfLifeEntry{},
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}
	for _, chartName := range supportedHelmCharts {
		apiURL := fmt.Sprintf("https://endoflife.date/api/%s.json", chartName)
		entries, err := fetchEOLEntries(httpClient, apiURL)
		if err != nil {
			continue
		}
		cacheDocument["helm_chart"].(map[string][]EndOfLifeEntry)[chartName] = entries
	}

	data, err := json.Marshal(cacheDocument)
	if err != nil {
		return fmt.Errorf("failed to marshal updated Helm chart cache: %w", err)
	}

	err = con.Set(ctx, key, data, ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to update Helm chart cache in Redis: %w", err)
	}

	return nil
}

// func fetchEOLEntries(client *http.Client, url string) ([]EndOfLifeEntry, error) {
// 	resp, err := client.Get(url)
// 	if err != nil {
// 		return nil, err
// 	}
// 	defer resp.Body.Close()
//
// 	var entries []EndOfLifeEntry
// 	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
// 		return nil, err
// 	}
// 	return entries, nil
// }

func getHelmChartEOLData(ctx context.Context, con *redis.Client, chartName string) ([]EndOfLifeEntry, error) {
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

	helmCharts, ok := cacheDocument["helm_chart"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid cache format: missing 'helm_chart' key")
	}

	rawData, exists := helmCharts[chartName]
	if !exists {
		return nil, fmt.Errorf("no data found for Helm chart: %s", chartName)
	}

	rawBytes, _ := json.Marshal(rawData)
	var result []EndOfLifeEntry

	if err := json.Unmarshal(rawBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal raw Helm chart data: %w", err)
	}

	return result, nil
}

// func isVersionExpired(current, newest string) bool {
// 	parseVersion := func(label, version string) (int, int) {
// 		segments := strings.Split(version, ".")
// 		major, err := strconv.Atoi(segments[0])
// 		if err != nil {
// 			log.Printf("Can't parse %s version major segment %q: %v", label, version, err)
// 		}
// 		minor := 0
// 		if len(segments) > 1 {
// 			minor, err = strconv.Atoi(segments[1])
// 			if err != nil {
// 				log.Printf("Can't parse %s version minor segment %q: %v", label, version, err)
// 			}
// 		}
// 		return major, minor
// 	}
//
// 	currentMajor, currentMinor := parseVersion("current", current)
// 	newestMajor, newestMinor := parseVersion("newest", newest)
//
// 	if currentMajor < newestMajor {
// 		return true
// 	} else if currentMajor == newestMajor && currentMinor < newestMinor {
// 		return true
// 	}
// 	return false
// }
