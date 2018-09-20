package main

import (
	"bufio"
	"bytes"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/log"
	"github.com/prometheus/common/version"

	"gopkg.in/alecthomas/kingpin.v2"
)

const namespace = "ethtool"

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

/*
func runEthtool(interface string) ([]byte, error) {
	cmd := exec.Command("/usr/sbin/ethtool", "-S", interface)
	out, err := c
}
*/

func collectEthtoolMetrics(ch chan<- prometheus.Metric) error {
	interfaces, err := ioutil.ReadDir("/sys/class/net")
	if err != nil {
		return err
	}

	for _, entry := range interfaces {
		log.Infof("interface=%#v\n", entry)
		if entry.Mode()&os.ModeSymlink != 0 {
			log.Infof("%v is a symlink!", entry.Name())
		}
		//out, err := runEthtool(entry.Name())
		cmd := exec.Command("/usr/sbin/ethtool", "-S", entry.Name())
/*
		err := cmd.Run()
		if err != nil {
			continue
		} 
*/
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(bytes.NewReader(out))
		for scanner.Scan() {
		log.Infof("out=%v", scanner.Text())
		}
	}
	return err
}

type EthtoolExporter struct {
	collectLock sync.Mutex
}

func (e *EthtoolExporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- ethtoolScrapeSuccessDesc
	ch <- ethtoolErrorDesc
}

func (e *EthtoolExporter) Collect(ch chan<- prometheus.Metric) {
	e.collectLock.Lock()
	err := collectEthtoolMetrics(ch)
	_ = err
	e.collectLock.Unlock()
}

func newEthtoolExporter() *EthtoolExporter {
	return &EthtoolExporter{}
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
	_ = exporter

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
