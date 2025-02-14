package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/go-redis/redis/v9"
	"github.com/google/uuid"
)

type KubernetesCluster struct {
	ID          uuid.UUID       `json:"id"`
	ClusterName string          `json:"cluster_name"`
	KubeVersion string          `json:"kube_version"`
	HelmCharts  []HelmChartData `json:"helm_charts"`
	UpdatedAt   string          `json:"updated_at"`
}

type HelmChartData struct {
	ChartName string `json:"chart_name"`
	Version   string `json:"version"`
	Namespace string `json:"namespace"`
}

type KubernetesClusters struct {
	Items map[uuid.UUID]KubernetesCluster
}

var (
	ErrClusterInsertFailed  = errors.New("Cluster insert failed")
	ErrClusterMarshalFailed = errors.New("Cluster marshal failed")
	ErrClusterNotFound      = errors.New("Cluster ID not found")
)

func (c *KubernetesClusters) InsertClusterData(cluster KubernetesCluster, ctx context.Context, con *redis.Client, ttl int) (uuid.UUID, error) {

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
	var clusters = KubernetesClusters{
		Items: make(map[uuid.UUID]KubernetesCluster),
	}

	iter := con.Scan(ctx, 0, "*", 0).Iterator()
	for iter.Next(ctx) {
		uid, err := uuid.Parse(iter.Val())
		if err != nil {
			if iter.Val() != "eol_cache:all_packages" {
				log.Printf("Cannot parse UUID: %s, %v", iter.Val(), err)
				continue
			}
			continue
		}

		clusters.Items[uid], _ = c.RetrieveCluster(uid, ctx, con)
	}

	if err := iter.Err(); err != nil {
		log.Printf("Error scanning clusters: %v", err)
	}
	return clusters, nil
}

func UUIDFromClusterName(clusterName string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceDNS, []byte(clusterName))
}
