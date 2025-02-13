package metrics

import (
	"fmt"
	"keepup/src/handler"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// Label names for cluster metrics
	IDCluster              = "id"
	ClusterName            = "cluster_name"
	KubeVersion            = "kube_version"
	ChartName              = "chart_name"
	ChartVersion           = "chart_version"
	ChartNamespace         = "chart_namespace"
	HelmReleaseMetricValue = float64(1)
	// Metric description for Kubernetes cluster metrics
	kubernetesClusterMetricDesc = prometheus.NewDesc(
		"kubernetes_cluster_info",
		"Information about Kubernetes clusters and installed Helm charts",
		[]string{
			IDCluster,
			ClusterName,
			KubeVersion,
			ChartName,
			ChartVersion,
			ChartNamespace,
		}, nil,
	)
)

// **KubernetesClusterCollector** collects and exports Kubernetes cluster metrics
type KubernetesClusterCollector struct {
	ClusterInfo *handler.KubernetesClusterMiddleware
}

// **Describe** implements the prometheus.Collector interface
func (kc KubernetesClusterCollector) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(kc, ch)
}

// **Collect** implements the prometheus.Collector interface
func (kc KubernetesClusterCollector) Collect(ch chan<- prometheus.Metric) {
	// Retrieve stored Kubernetes clusters from Redis
	clusters, _ := kc.ClusterInfo.Clusters.ScanClusters(kc.ClusterInfo.Context, kc.ClusterInfo.Client)

	for id, cluster := range clusters.Items {
		// Iterate through Helm charts
		for _, chart := range cluster.HelmCharts {
			ch <- prometheus.MustNewConstMetric(
				kubernetesClusterMetricDesc,
				prometheus.GaugeValue,
				1.0,
				fmt.Sprint(id),
				cluster.ClusterName,
				cluster.KubeVersion,
				chart.ChartName,
				chart.Version,
				chart.Namespace,
			)
		}
	}
}
