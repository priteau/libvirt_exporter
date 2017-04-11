// Copyright 2017 Kumina, https://kumina.nl/
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"encoding/xml"
	"flag"
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"

	libvirt "github.com/libvirt/libvirt-go"

	"./libvirt_schema"
)

var (
	libvirtUpDesc = prometheus.NewDesc(
		prometheus.BuildFQName("libvirt", "", "up"),
		"Whether scraping libvirt's metrics was successful.",
		nil,
		nil)

	libvirtBlockRdBytesDesc = prometheus.NewDesc(
		prometheus.BuildFQName("libvirt", "block_stats", "read_bytes_total"),
		"Number of bytes read from a block device, in bytes.",
		[]string{"domain", "source_file", "target_device"},
		nil)
	libvirtBlockRdReqDesc = prometheus.NewDesc(
		prometheus.BuildFQName("libvirt", "block_stats", "read_requests_total"),
		"Number of read requests from a block device.",
		[]string{"domain", "source_file", "target_device"},
		nil)
	libvirtBlockRdTotalTimesDesc = prometheus.NewDesc(
		prometheus.BuildFQName("libvirt", "block_stats", "read_seconds_total"),
		"Amount of time spent reading from a block device, in seconds.",
		[]string{"domain", "source_file", "target_device"},
		nil)
	libvirtBlockWrBytesDesc = prometheus.NewDesc(
		prometheus.BuildFQName("libvirt", "block_stats", "write_bytes_total"),
		"Number of bytes written from a block device, in bytes.",
		[]string{"domain", "source_file", "target_device"},
		nil)
	libvirtBlockWrReqDesc = prometheus.NewDesc(
		prometheus.BuildFQName("libvirt", "block_stats", "write_requests_total"),
		"Number of write requests from a block device.",
		[]string{"domain", "source_file", "target_device"},
		nil)
	libvirtBlockWrTotalTimesDesc = prometheus.NewDesc(
		prometheus.BuildFQName("libvirt", "block_stats", "write_seconds_total"),
		"Amount of time spent writing from a block device, in seconds.",
		[]string{"domain", "source_file", "target_device"},
		nil)
	libvirtBlockFlushReqDesc = prometheus.NewDesc(
		prometheus.BuildFQName("libvirt", "block_stats", "flush_requests_total"),
		"Number of flush requests from a block device.",
		[]string{"domain", "source_file", "target_device"},
		nil)
	libvirtBlockFlushTotalTimesDesc = prometheus.NewDesc(
		prometheus.BuildFQName("libvirt", "block_stats", "flush_seconds_total"),
		"Amount of time spent flushing of a block device, in seconds.",
		[]string{"domain", "source_file", "target_device"},
		nil)
)

// CollectDomain extracts Prometheus metrics from a libvirt domain.
func CollectDomain(ch chan<- prometheus.Metric, domain *libvirt.Domain) error {
	domainName, err := domain.GetName()
	if err != nil {
		return err
	}

	xmlDesc, err := domain.GetXMLDesc(0)
	if err != nil {
		return err
	}

	var desc libvirt_schema.Domain
	err = xml.Unmarshal([]byte(xmlDesc), &desc)
	if err != nil {
		return err
	}

	// Report block device statistics.
	for _, disk := range desc.Devices.Disks {
		blockStats, err := domain.BlockStats(disk.Target.Device)
		if err != nil {
			return err
		}
		if blockStats.RdBytesSet {
			ch <- prometheus.MustNewConstMetric(
				libvirtBlockRdBytesDesc,
				prometheus.CounterValue,
				float64(blockStats.RdBytes),
				domainName,
				disk.Source.File,
				disk.Target.Device)
		}
		if blockStats.RdReqSet {
			ch <- prometheus.MustNewConstMetric(
				libvirtBlockRdReqDesc,
				prometheus.CounterValue,
				float64(blockStats.RdReq),
				domainName,
				disk.Source.File,
				disk.Target.Device)
		}
		if blockStats.RdTotalTimesSet {
			ch <- prometheus.MustNewConstMetric(
				libvirtBlockRdTotalTimesDesc,
				prometheus.CounterValue,
				float64(blockStats.RdTotalTimes) / 1e9,
				domainName,
				disk.Source.File,
				disk.Target.Device)
		}
		if blockStats.WrBytesSet {
			ch <- prometheus.MustNewConstMetric(
				libvirtBlockWrBytesDesc,
				prometheus.CounterValue,
				float64(blockStats.WrBytes),
				domainName,
				disk.Source.File,
				disk.Target.Device)
		}
		if blockStats.WrReqSet {
			ch <- prometheus.MustNewConstMetric(
				libvirtBlockWrReqDesc,
				prometheus.CounterValue,
				float64(blockStats.WrReq),
				domainName,
				disk.Source.File,
				disk.Target.Device)
		}
		if blockStats.WrTotalTimesSet {
			ch <- prometheus.MustNewConstMetric(
				libvirtBlockWrTotalTimesDesc,
				prometheus.CounterValue,
				float64(blockStats.WrTotalTimes) / 1e9,
				domainName,
				disk.Source.File,
				disk.Target.Device)
		}
		if blockStats.FlushReqSet {
			ch <- prometheus.MustNewConstMetric(
				libvirtBlockFlushReqDesc,
				prometheus.CounterValue,
				float64(blockStats.FlushReq),
				domainName,
				disk.Source.File,
				disk.Target.Device)
		}
		if blockStats.FlushTotalTimesSet {
			ch <- prometheus.MustNewConstMetric(
				libvirtBlockFlushTotalTimesDesc,
				prometheus.CounterValue,
				float64(blockStats.FlushTotalTimes) / 1e9,
				domainName,
				disk.Source.File,
				disk.Target.Device)
		}
		// Skip "Errs", as the documentation does not clearly
		// explain what this means.
	}

	return nil
}

// CollectFromLibvirt obtains Prometheus metrics from all domains in a
// libvirt setup.
func CollectFromLibvirt(ch chan<- prometheus.Metric, uri string) error {
	conn, err := libvirt.NewConnect(uri)
	if err != nil {
		return err
	}
	defer conn.Close()

	// First attempt to get a list of active domains using
	// ListAllDomains(). If this fails, the remote side is using a version
	// of libvirt older than 0.9.13. In that case, fall back to using
	// ListDomains() in combination with LookupDomainById().
	domains, err := conn.ListAllDomains(libvirt.CONNECT_LIST_DOMAINS_ACTIVE)
	if err == nil {
		for _, domain := range domains {
			defer domain.Free()
		}
		for _, domain := range domains {
			err = CollectDomain(ch, &domain)
			if err != nil {
				return err
			}
		}
	} else {
		domainIds, err := conn.ListDomains()
		if err != nil {
			return err
		}
		for _, id := range domainIds {
			domain, err := conn.LookupDomainById(id)
			if err == nil {
				err = CollectDomain(ch, domain)
				domain.Free()
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// LibvirtExporter implements a Prometheus exporter for libvirt state.
type LibvirtExporter struct {
	uri string
}

// NewLibvirtExporter creates a new Prometheus exporter for libvirt.
func NewLibvirtExporter(uri string) (*LibvirtExporter, error) {
	return &LibvirtExporter{
		uri: uri,
	}, nil
}

// Describe returns metadata for all Prometheus metrics that may be exported.
func (e *LibvirtExporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- libvirtUpDesc

	ch <- libvirtBlockRdBytesDesc
	ch <- libvirtBlockRdReqDesc
	ch <- libvirtBlockRdTotalTimesDesc
	ch <- libvirtBlockWrBytesDesc
	ch <- libvirtBlockWrReqDesc
	ch <- libvirtBlockWrTotalTimesDesc
	ch <- libvirtBlockFlushReqDesc
	ch <- libvirtBlockFlushTotalTimesDesc
}

// Collect scrapes Prometheus metrics from libvirt.
func (e *LibvirtExporter) Collect(ch chan<- prometheus.Metric) {
	err := CollectFromLibvirt(ch, e.uri)
	if err == nil {
		ch <- prometheus.MustNewConstMetric(
			libvirtUpDesc,
			prometheus.GaugeValue,
			1.0)
	} else {
		log.Printf("Failed to scrape metrics: %s", err)
		ch <- prometheus.MustNewConstMetric(
			libvirtUpDesc,
			prometheus.GaugeValue,
			0.0)
	}
}

func main() {
	var (
		listenAddress = flag.String("web.listen-address", ":9167", "Address to listen on for web interface and telemetry.")
		metricsPath   = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
		libvirtURI    = flag.String("libvirt.uri", "qemu:///system", "Libvirt URI from which to extract metrics.")
	)
	flag.Parse()

	exporter, err := NewLibvirtExporter(*libvirtURI)
	if err != nil {
		panic(err)
	}
	prometheus.MustRegister(exporter)

	http.Handle(*metricsPath, prometheus.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`
			<html>
			<head><title>Libvirt Exporter</title></head>
			<body>
			<h1>Libvirt Exporter</h1>
			<p><a href='` + *metricsPath + `'>Metrics</a></p>
			</body>
			</html>`))
	})
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}