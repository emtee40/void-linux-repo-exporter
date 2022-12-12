package main

import (
	"hash/crc32"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gregjones/httpcache"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	namespace = "repo"
)

var (
	hc *http.Client
)

func doProbe(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	if target == "" {
		http.Error(w, "Target parameter is missing", http.StatusBadRequest)
		return
	}
	arch := r.URL.Query().Get("arch")
	if arch == "" {
		http.Error(w, "Arch parameter is missing", http.StatusBadRequest)
		return
	}

	repodata, _, err := fetch("http://" + target + "/" + arch + "-repodata")
	if err != nil {
		log.Printf("Error fetching repodata: %s", err)
		http.Error(w, "Error fetching repodata: "+err.Error(), http.StatusPreconditionFailed)
	}

	_, stagedataStatusCode, err := fetch("http://" + target + "/" + arch + "-stagedata")
	if err != nil {
		log.Printf("Error fetching stagedata: %s", err)
		http.Error(w, "Error fetching stagedata: "+err.Error(), http.StatusPreconditionFailed)
	}

	otimes, c, err := fetch("http://" + target + "/otime")
	if err != nil {
		log.Println("Error fetching origin timestamp file: %s", err)
		http.Error(w, "Error fetching origin time: "+err.Error(), http.StatusPreconditionFailed)
	}
	var otime float64
	if c == 200 {
		// If this fails it will just stay at zero; acceptable.
		otime, err = strconv.ParseFloat(strings.TrimSpace(string(otimes)), 64)
		if err != nil {
			log.Println("Error parsing otime", err)
		}
	}

	stimeStarts, c, err := fetch("http://" + target + "/stime-start")
	if err != nil {
		http.Error(w, "Error fetching origin time: "+err.Error(), http.StatusPreconditionFailed)
	}
	var stimeStart float64
	if c == 200 {
		// If this fails it will just stay at zero; acceptable.
		stimeStart, err = strconv.ParseFloat(strings.TrimSpace(string(stimeStarts)), 64)
		if err != nil {
			log.Println("Error parsing stimeStart", err)
		}
	}
	stimeEnds, c, err := fetch("http://" + target + "/stime-end")
	if err != nil {
		http.Error(w, "Error fetching origin time: "+err.Error(), http.StatusPreconditionFailed)
	}
	var stimeEnd float64
	if c == 200 {
		// If this fails it will just stay at zero; acceptable.
		stimeEnd, err = strconv.ParseFloat(strings.TrimSpace(string(stimeEnds)), 64)
		if err != nil {
			log.Println("Error parsing stimeEnd", err)
		}
	}

	var (
		rdatachecksum = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: prometheus.BuildFQName(namespace, "", "repodata_checksum"),
				Help: "CRC32 of the repodata",
			},
		)

		repostaged = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: prometheus.BuildFQName(namespace, "", "is_staged"),
				Help: "Non-zero if a stagedata file is present on the repo",
			},
		)
		repoOriginTime = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: prometheus.BuildFQName(namespace, "", "origin_time"),
				Help: "A Unix Timestamp updated every minute on the origin",
			},
		)
		repoSyncStartTime = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: prometheus.BuildFQName(namespace, "", "sync_start_time"),
				Help: "A Unix timestamp written by the mirror when it last started a sync",
			},
		)
		repoSyncEndTime = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: prometheus.BuildFQName(namespace, "", "sync_end_time"),
				Help: "A Unix timestamp written by the mirror when it last finished a sync",
			},
		)
	)

	rdatachecksum.Set(float64(crc32.ChecksumIEEE(repodata)))
	if stagedataStatusCode == 200 {
		repostaged.Set(1)
	}
	repoOriginTime.Set(otime)
	repoSyncStartTime.Set(stimeStart)
	repoSyncEndTime.Set(stimeEnd)

	registry := prometheus.NewRegistry()
	registry.MustRegister(rdatachecksum, repostaged, repoOriginTime)
	if stimeStart > 0 && stimeEnd > 0 {
		registry.Register(repoSyncStartTime)
		registry.Register(repoSyncEndTime)
	}
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(w, r)
}

func fetch(url string) ([]byte, int, error) {
	resp, err := hc.Get(url)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	bytes, err := ioutil.ReadAll(resp.Body)
	return bytes, resp.StatusCode, err
}

func main() {
	transport := httpcache.NewMemoryCacheTransport()
	hc = transport.Client()
	hc.Timeout = time.Second * 10

	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/probe", doProbe)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>
                <head><title>XBPS Repo Exporter</title></head>
                <body>
                <h1>XBPS Repo Exporter</h1>
                </body>
                </html>`))
	})

	http.ListenAndServe(":1234", nil)
}
