package main

import (
	"bufio"
	"bytes"
	//"fmt"
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

	// we only want to support ethtool version >= 4 currently.
	// version 3.x gives whackadoo output that I do not want to
	// parse... sorry.
	out, err := exec.Command(ethtool, "--version").Output()
	if err != nil {
		log.Fatalf(err.Error())
	}
	ethtoolVersion, err := strconv.ParseFloat(strings.TrimSpace(strings.Split(string(out), " ")[2]), 64)
	if err != nil {
		log.Fatalf(err.Error())
	}

	if ethtoolVersion <= 3.15 {
		log.Fatalf("I do not want to support ethtool <= 3.15. Don't want to parse that format today.")
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

func formatMetricName(m string) string {
	var metric string
	// strip leading space
	metric = strings.TrimSpace(m)
	// replace spaces
	metric = strings.Replace(metric, " ", "_", -1)
	return metric
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
			metrics[formatMetricName(entries[0])] = m
		}
		for k, v := range metrics {
			ch <- prometheus.MustNewConstMetric(
				prometheus.NewDesc(
					prometheus.BuildFQName(namespace, k, "total"),
					"interface statistics",
					[]string{"interface"},
					nil,
				),
				prometheus.GaugeValue,
				v,
				entry.Name())
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
	kingpin.Version(version.Print("ethtool_exporter"))
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
