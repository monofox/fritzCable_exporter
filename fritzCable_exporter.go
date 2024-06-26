package main

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/log"
	"github.com/prometheus/common/version"
	"gopkg.in/alecthomas/kingpin.v2"
)

const (
	exporterName = "fritzCable_exporter"
	namespace    = "fritzCable"
)

func main() {
	var (
		listenAddress               = kingpin.Flag("web.listen-address", "Address to listen on for web interface and telemetry.").Default(":9623").String()
		metricsPath                 = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").String()
		clientScrapeURI             = kingpin.Flag("client.scrape-uri", "Base URI on which to scrape Fritzbox Cable.").Default("http://192.168.178.1/").String()
		clientUsername              = kingpin.Flag("client.username", "Username of Fritzbox Cable.").Default("admin").String()
		clientPassword              = kingpin.Flag("client.password", "Password of Fritzbox Cable.").Default("totalsecure").String()
		clientTimeout               = kingpin.Flag("client.timeout", "Timeout for HTTP requests to Fritzbox.").Default("50s").Duration()
		clientDisableCertValidation = kingpin.Flag("client.disable-cert", "Ignore SSL certificate valiation").Bool()
	)

	log.AddFlags(kingpin.CommandLine)
	kingpin.Version(version.Print(exporterName))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	log.Infoln("Starting", exporterName, version.Info())
	log.Infoln("Build context", version.BuildContext())

	exporter, err := NewExporter(*clientScrapeURI, *clientUsername, *clientPassword, *clientTimeout, *clientDisableCertValidation)
	if err != nil {
		log.Fatal(err)
	}
	prometheus.MustRegister(exporter)
	prometheus.MustRegister(version.NewCollector(exporterName))

	log.Infoln("Listening on", *listenAddress)
	http.Handle(*metricsPath, promhttp.Handler())
	http.Handle("/", http.RedirectHandler(*metricsPath, http.StatusFound))
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
