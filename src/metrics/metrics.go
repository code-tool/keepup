package metrics

import (
	"fmt"
	"keepup/src/handler"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	ID                   = "id"
	OsId                 = "os_id"
	OsVersionCodename    = "version_codename"
	OsVersion            = "version"
	OsVersionId          = "version_id"
	DataCenter           = "data_center"
	HostIP               = "host_ip"
	osReleaseMetricValue = float64(1)
	osReleseMetricDesc   = prometheus.NewDesc(
		"os_release_info",
		"count of OS by version",
		[]string{
			ID,
			OsId,
			OsVersionCodename,
			OsVersion,
			OsVersionId,
			DataCenter,
			HostIP,
		}, nil,
	)
)

type OsReleaseCollector struct {
	RelInfo *handler.OsReleasesMiddleware
}

func (cc OsReleaseCollector) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(cc, ch)
}

func (cc OsReleaseCollector) Collect(ch chan<- prometheus.Metric) {
	rels, _ := cc.RelInfo.OsReleases.Scan(cc.RelInfo.Context, cc.RelInfo.Client)

	for id, rel := range rels.Items {
		if rel.Version == "" {
			continue
		}
		ch <- prometheus.MustNewConstMetric(
			osReleseMetricDesc,
			prometheus.CounterValue,
			osReleaseMetricValue,
			fmt.Sprint(id),
			rel.OsId,
			rel.VersionCodename,
			rel.Version,
			rel.VersionId,
			rel.DataCenter,
			rel.HostIP,
		)
	}
}
