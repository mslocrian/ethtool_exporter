package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/log"
	"github.com/prometheus/common/version"

	"gopkg.in/alecthomas/kingpin.v2"
)

const namespace = "ethtool"

var ethtool string

var (
	ethtoolScrapeSuccessDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "scrape", "collector_duration_seconds"),
		"ethtool_exporter: Duration of collector scrape.",
		[]string{"collector"},
		nil,
	)

	ethtoolErrorDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "scrape", "collector_success"),
		"ethtool_exporter: Whether a collector succeeded.",
		[]string{"collector"},
		nil,
	)
)

func init() {
	if _, err := os.Stat("/sbin/ethtool"); err == nil {
		ethtool = "/sbin/ethtool"
	}

	if _, err := os.Stat("/usr/sbin/ethtool"); err == nil {
		ethtool = "/usr/sbin/ethtool"
	}
	if ethtool == "" {
		log.Fatalf("Could not find ethtool executable.")
	}

	prometheus.MustRegister(version.NewCollector("ethtool_exporter"))
}

func handler(w http.ResponseWriter, r *http.Request) {
	registry := prometheus.NewRegistry()

	gatherers := prometheus.Gatherers{
		prometheus.DefaultGatherer,
		registry,
	}

	h := promhttp.InstrumentMetricHandler(
		registry,
		promhttp.HandlerFor(gatherers,
			promhttp.HandlerOpts{
				ErrorLog:      log.NewErrorLogger(),
				ErrorHandling: promhttp.ContinueOnError,
			}),
	)
	h.ServeHTTP(w, r)
}

type EthtoolExporter struct {
	collectLock sync.Mutex
	collectors  map[string]*prometheus.GaugeVec
}

func (e *EthtoolExporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- ethtoolScrapeSuccessDesc
	ch <- ethtoolErrorDesc
}

func (e *EthtoolExporter) Collect(ch chan<- prometheus.Metric) {
	e.collectLock.Lock()
	defer e.collectLock.Unlock()

	// get a list of interfaces
	interfaces, err := ioutil.ReadDir("/sys/class/net")
	if err != nil {
		log.Errorf("Collect(): err=%v", err.Error())
		return
	}

	for _, entry := range interfaces {
		var metrics map[string]float64
		metrics = make(map[string]float64)

		cmd := exec.Command(ethtool, "-S", entry.Name())
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(bytes.NewReader(out))
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "NIC statistics") {
				continue
			}
			entries := strings.Split(scanner.Text(), ":")
			m, err := strconv.ParseFloat(strings.TrimSpace(entries[1]), 64)
			if err != nil {
				log.Errorf("Could not convert ascii metric to integer %#v", entries)
				continue
			}
			metrics[strings.TrimSpace(entries[0])] = m
		}
		for k, v := range metrics {
			if _, ok := e.collectors[k]; !ok {
				e.collectors[k] = prometheus.NewGaugeVec(prometheus.GaugeOpts{
					Name: fmt.Sprintf("%s_%s", namespace, k),
					Help: "interface statistics",
				}, []string{"interface"})
				prometheus.MustRegister(e.collectors[k])
				e.collectors[k].With(prometheus.Labels{"interface": entry.Name()}).Set(v)
			} else {
				e.collectors[k].With(prometheus.Labels{"interface": entry.Name()}).Set(v)
			}
		}
	}
}

func entryIntfExistsInCollector(s string, intfs []os.FileInfo) bool {
	for _, i := range intfs {
		if i.Name() == s {
			return true
		}
	}
	return false
}
func newEthtoolExporter() *EthtoolExporter {
	c := make(map[string]*prometheus.GaugeVec)
	return &EthtoolExporter{collectors: c}
}

func main() {
	var (
		listenAddress = kingpin.Flag("web.listen-address", "Address on which to expose metrics and web interface.").Default(":9490").String()
		metricsPath   = kingpin.Flag("web.telemetry-path", "Path under which to exposxe metrics").Default("/metrics").String()
	)

	log.AddFlags(kingpin.CommandLine)
	kingpin.Version(version.Print("node_exporter"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	log.Infoln("Starting ethtool_exporter", version.Info())
	log.Infoln("Build context", version.BuildContext())

	exporter := newEthtoolExporter()
	prometheus.MustRegister(exporter)

	http.HandleFunc(*metricsPath, handler)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
			<head><title>Ethtool Exporter</title></head>
			<body>
			<h1>Ethtool Exporter<h1>
			<p><a href="` + *metricsPath + `">Metrics</a></p>
			</body>
			</html>`))
	})

	log.Infoln("Listening on", *listenAddress)
	err := http.ListenAndServe(*listenAddress, nil)
	if err != nil {
		log.Fatal(err)
	}
}
