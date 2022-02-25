package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	//"strings"
	"sync"
	"time"
	"strings"
	"crypto/md5"
	//"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"io/ioutil"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/text/encoding/unicode"
	//"golang.org/x/crypto/pbkdf2"
	"github.com/prometheus/common/log"
)

var (
	channelLabelNames   = []string{"channel"}
	interfaceLabelNames = []string{"interface"}
)

func newChannelMetric(subsystemName, metricName, docString string, extraLabels ...string) *prometheus.Desc {
	return prometheus.NewDesc(prometheus.BuildFQName(namespace, subsystemName, metricName), docString, append(channelLabelNames, extraLabels...), nil)
}

type metrics map[int]*prometheus.Desc

var (
	targetUpMetric = prometheus.NewDesc(prometheus.BuildFQName(namespace, "", "up"), "Was the last scrape of Fritzbox succesful.", nil, nil)
	// FIXME: not parsed yet.
	networkMetrics = metrics{
		1: prometheus.NewDesc(prometheus.BuildFQName(namespace, "network", "receive_bytes_total"), "", []string{"interface"}, nil),
		2: prometheus.NewDesc(prometheus.BuildFQName(namespace, "network", "receive_packets_total"), "", []string{"interface"}, nil),
		3: prometheus.NewDesc(prometheus.BuildFQName(namespace, "network", "receive_errs_total"), "", []string{"interface"}, nil),
		4: prometheus.NewDesc(prometheus.BuildFQName(namespace, "network", "receive_drop_total"), "", []string{"interface"}, nil),
		5: prometheus.NewDesc(prometheus.BuildFQName(namespace, "network", "transmit_bytes_total"), "", []string{"interface"}, nil),
		6: prometheus.NewDesc(prometheus.BuildFQName(namespace, "network", "transmit_packets_total"), "", []string{"interface"}, nil),
		7: prometheus.NewDesc(prometheus.BuildFQName(namespace, "network", "transmit_errs_total"), "", []string{"interface"}, nil),
		8: prometheus.NewDesc(prometheus.BuildFQName(namespace, "network", "transmit_drop_total"), "", []string{"interface"}, nil),
	}

	downstreamChannelMetrics = metrics{
		2:  newChannelMetric("downstream", "locked", "Downstream Lock Status"),
		3:  newChannelMetric("downstream", "channel_type", "Downstream Channel Type", "type"),
		4:  newChannelMetric("downstream", "bonded", "Downstream Bonding Status"),
		5:  newChannelMetric("downstream", "center_frequency_hz", "Downstream Center Frequency"),
		6:  newChannelMetric("downstream", "width_hz", "Downstream Width"),
		7:  newChannelMetric("downstream", "snr_threshold_db", "Downstream SNR/MER Threshold Value"),
		8:  newChannelMetric("downstream", "receive_level_dbmv", "Downstream Receive Level"),
		9:  newChannelMetric("downstream", "modulation", "Downstream Modulation/Profile ID", "modulation"),
		10: newChannelMetric("downstream", "codewords_unerrored_total", "Downstream Unerrored Codewords"),
		11: newChannelMetric("downstream", "codewords_corrected_total", "Downstream Corrected Codewords"),
		12: newChannelMetric("downstream", "codewords_uncorrectable_total", "Downstream Uncorrectable Codewords"),
		13: newChannelMetric("downstream", "latency", "Downstream latency"),
		14: newChannelMetric("downstream", "modulation_clear", "Downstream Modulation in clear value"),
	}

	upstreamChannelMetrics = metrics{
		2: newChannelMetric("upstream", "locked", "Upstream Lock Status"),
		3: newChannelMetric("upstream", "channel_type", "Downstream Channel Type", "type"),
		4: newChannelMetric("upstream", "bonded", "Upstream Bonding Status"),
		5: newChannelMetric("upstream", "center_frequency_hz", "Upstream Center Frequency"),
		6: newChannelMetric("upstream", "width_hz", "Upstream Width"),
		7: newChannelMetric("upstream", "transmit_level_dbmv", "Upstream Transmit Level"),
		8: newChannelMetric("upstream", "modulation", "Upstream Modulation/Profile ID", "modulation"),
		9: newChannelMetric("upstream", "modulation_clear", "Upstream Modulation in clear value"),
	}
)

type SessionInfo struct {
	Sid string `xml:"SID"`
	Challenge string `xml:"Challenge"`
	BlockTime string `xml:"BlockTime"`
}

type FritzRoot struct {
	Data FritzData `json:"data"`
	Hide FritzHide `json:"hide"`
	pid string `json:"pid"`
	sid string `json:"sid"`
}

type FritzData struct {
	ChannelsDown FritzChannelD `json:"channelDs"`
	ChannelsUp FritzChannelUs `json:"channelUs"`
	Oem string `json:"oem"`
}

type FritzChannelD struct{
	Channel30 []FritzChannel `json:"docsis30"`
	Channel31 []FritzChannel `json:"docsis31"`
}
type FritzChannelUs struct{
	Channel30 []FritzChannel `json:"docsis30"`
	Channel31 []FritzChannel `json:"docsis31"`
}

type FritzChannel struct{
	Channel int `json:"channel`
	ChannelId int `json:"channelID`
	CorrErrors int `json:"corrErrors`
	Frequency string `json:"frequency"`
	Latency float32 `json:"latency"`
	Mse string `json:"mse"`
	NonCorrErrors int `json:"nonCorrErrors"`
	Powerlevel string `json:"powerLevel"`
	Multiplex string `json:"multiplex"`
	Type string `json:"type"`
}

type FritzHide struct {
	Chan bool `json:"chan"`
	Dectmail bool `json:"dectMail"`
	Dectmoni bool `json:"dectMoni"`
	Dectmoniex bool `json:"dectMoniEx"`
	Dvbset bool `json:"dvbSet"`
	Dvbsig bool `json:"dvbSig"`
	Dvbradio bool `json:"dvbradio"`
	Faxset bool `json:"faxSet"`
	Liveimg bool `json:"liveImg"`
	Livetv bool `json:"liveTv"`
	Mobile bool `json:"mobile"`
	Rss bool `json:"rss"`
	Shareusb bool `json:"shareUsb"`
	Ssoset bool `json:"ssoSet"`
	Tvhd bool `json:"tvhd"`
	Tvsd bool `json:"tvsd"`
	Wguest bool `json:"wGuest"`
	Wkey bool `json:"wKey"`
	Wlanmesh bool `json:"wlanmesh"`
	Wps bool `json:"wps"`
}

type Exporter struct {
	baseURL string
	username string
	password string
	sid string
	client  *http.Client
	mutex   sync.RWMutex

	totalScrapes          prometheus.Counter
	parseFailures         *prometheus.CounterVec
	clientRequestCount    *prometheus.CounterVec
	clientRequestDuration *prometheus.HistogramVec
}

func NewExporter(uri string, username string, password string, timeout time.Duration) (*Exporter, error) {
	client := &http.Client{}
	client.Timeout = timeout

	clientRequestCount := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "exporter_client_requests_total",
		Help:      "HTTP requests to Fritzbox Cable",
	}, []string{"code", "method"})

	clientRequestDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "exporter_client_request_duration_seconds",
		Help:      "Histogram of Fritzbox Cable HTTP request latencies.",
	}, []string{"code", "method"})

	client.Transport = promhttp.InstrumentRoundTripperCounter(clientRequestCount,
		promhttp.InstrumentRoundTripperDuration(clientRequestDuration, http.DefaultTransport))

	return &Exporter{
		baseURL: uri,
		client:  client,
		username: username,
		password: password,
		totalScrapes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "exporter_scrapes_total",
			Help:      "Current total Fritzbox Cable scrapes.",
		}),
		parseFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "exporter_parse_errors_total",
			Help:      "Number of errors while parsing statistics.",
		}, []string{"file"}),
		clientRequestCount:    clientRequestCount,
		clientRequestDuration: clientRequestDuration,
	}, nil
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	for _, m := range networkMetrics {
		ch <- m
	}
	for _, m := range downstreamChannelMetrics {
		ch <- m
	}
	for _, m := range upstreamChannelMetrics {
		ch <- m
	}

	ch <- targetUpMetric
	ch <- e.totalScrapes.Desc()
	e.parseFailures.Describe(ch)
	e.clientRequestCount.Describe(ch)
	e.clientRequestDuration.Describe(ch)
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	up := e.scrape(ch)
	ch <- prometheus.MustNewConstMetric(targetUpMetric, prometheus.GaugeValue, up)

	ch <- e.totalScrapes
	e.parseFailures.Collect(ch)
	e.clientRequestCount.Collect(ch)
	e.clientRequestDuration.Collect(ch)
}

func (e *Exporter) fetch(filename string) (io.ReadCloser, error) {
	u, err := url.Parse(e.baseURL)
	if err != nil {
		return nil, err
	}
	u.Path = path.Join(u.Path, filename)

	resp, err := e.client.PostForm(u.String(), url.Values{
		"xhr": {"1"},
		"sid": {e.sid},
		"lang": {"de"},
		"page": {"docInfo"},
		"xhrId": {"all"}})
	if err != nil {
		return nil, err
	}
	if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		resp.Body.Close()
		return nil, fmt.Errorf("Scraping %s failed: HTTP status %d", u.String(), resp.StatusCode)
	}
	return resp.Body, nil
}

func (e *Exporter) login() (error) {
	u, err := url.Parse(e.baseURL)
	if err != nil {
		return err
	}
	u.Path = path.Join(u.Path, "login_sid.lua")
	resp, err := e.client.Get(u.String())
	if err != nil {
		return err
	}

	byteValue, _ := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	var sessionInfo SessionInfo
	xml.Unmarshal(byteValue, &sessionInfo)
	fmt.Println(sessionInfo.Challenge)
	if string(sessionInfo.Challenge[0:2]) == "2$" {
		// SHA256, pbkdf2 hash
		//string(sessionInfo.Challenge[0:2]))
		//salt := []byte("asdf")
		//dk := pbkdf2.Key([]byte(e.password), salt, 256, 32, sha256.New)
		//fmt.Println(dk)
		fmt.Println("Not supported")
	}
	utf16 := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
	prepPassword := sessionInfo.Challenge + "-" + e.password
	encoded, err := utf16.NewEncoder().String(prepPassword)
	if err != nil {
		fmt.Println(err)
		return err
	}
	md5Hex := md5.Sum([]byte(encoded))
	md5Challenge := sessionInfo.Challenge + "-" + hex.EncodeToString(md5Hex[:])
	fmt.Println(md5Challenge)
	// no login via Post
	resp, err = e.client.PostForm(u.String(), url.Values{
		"username": {e.username},
		"response": {md5Challenge}})
	if err != nil {
		return err
	}
	if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		resp.Body.Close()
		return fmt.Errorf("Scraping %s failed: HTTP status %d", u.String(), resp.StatusCode)
	}
	byteValue, _ = ioutil.ReadAll(resp.Body)
	xml.Unmarshal(byteValue, &sessionInfo)
	e.sid = sessionInfo.Sid
	resp.Body.Close()
	return nil
}

func parseDownstreamChannels(ch chan<- prometheus.Metric, e *Exporter, channelType string, cableChan FritzChannel) {
	fmt.Println("ChannelId: " + strconv.Itoa(cableChan.ChannelId))
	channelLabel := ""
	if channelType == "OFDM" {
		channelLabel = "O"
	}
	channelLabel = channelLabel + fmt.Sprintf("%02d", cableChan.ChannelId)

	for i, metric := range downstreamChannelMetrics {
		var err error = nil
		var value float64
		var valueInt int64
		var labelValues = []string{channelLabel}
		switch i {
		case 2:
			// We have no knowledge about locked / ... in fritzbox.
			value = 1
		case 3:
			labelValues = append(labelValues, channelType)
			value = 1
		case 4:
			// We have no knowledge about bonded / ... in fritzbox.
			value = 1
		case 5:
			if channelType == "OFDM" {
				// Its a range for OFDM ?
				freqSplit := strings.Split(cableChan.Frequency, " - ")
				valueInt, err = strconv.ParseInt(freqSplit[0], 10, 64)
			} else {
				valueInt, err = strconv.ParseInt(cableChan.Frequency, 10, 64)
			}
			// Fritzbox shows it always in Mhz
			valueInt = valueInt * 1000000
			value = float64(valueInt)
		case 6:
			value = 0.0
		case 7:
			value, err = strconv.ParseFloat(cableChan.Mse, 64)
			// For OFDM, error is fine!
			if channelType == "OFDM" && err != nil {
				value = 0.0
				err = nil
			}
		case 8:
			value, err = strconv.ParseFloat(cableChan.Powerlevel, 64)
		case 9:
			labelValues = append(labelValues, cableChan.Type)
			value = 1
		case 10:
			value = 0
		case 11:
			value = float64(cableChan.CorrErrors)
		case 12:
			value = float64(cableChan.NonCorrErrors)
		case 13:
			value = float64(cableChan.Latency)
		case 14:
                        numval := strings.Replace(cableChan.Type, "QAM", "", -1)
			ofdmK := false
			if (strings.Contains(numval, "K")) {
				numval = strings.Replace(numval, "K", "", -1)
				ofdmK = true
			}
			convval, _ := strconv.ParseInt(numval, 10, 0)
			if (ofdmK) {
				convval = convval * 1024
			}
			value = float64(convval)
		default:
			continue
		}
		if err != nil {
			log.Errorln(err)
			e.parseFailures.WithLabelValues("docInfo").Inc()
			continue
		}
		ch <- prometheus.MustNewConstMetric(metric, prometheus.CounterValue, value, labelValues...)
	}
}

func parseUpstreamChannels(ch chan<- prometheus.Metric, e *Exporter, channelType string, cableChan FritzChannel) {
	fmt.Println("ChannelId: " + strconv.Itoa(cableChan.ChannelId))
	channelLabel := ""
	if channelType == "OFDM" {
		channelLabel = "O"
	}
	channelLabel = channelLabel + fmt.Sprintf("%02d", cableChan.ChannelId)

	for i, metric := range upstreamChannelMetrics {
		var err error = nil
		var value float64
		var valueInt int64
		var labelValues = []string{channelLabel}
		switch i {
		case 2:
			// We have no knowledge about locked / ... in fritzbox.
			value = 1
		case 3:
			labelValues = append(labelValues, channelType)
			value = 1
		case 4:
			// We have no knowledge about bonded / ... in fritzbox.
			value = 1
		case 5:
			if channelType == "OFDM" {
				// Its a range for OFDM ?
				freqSplit := strings.Split(cableChan.Frequency, " - ")
				valueInt, err = strconv.ParseInt(freqSplit[0], 10, 64)
			} else {
				valueInt, err = strconv.ParseInt(cableChan.Frequency, 10, 64)
			}
			// Fritzbox shows it always in Mhz
			valueInt = valueInt * 1000000
			value = float64(valueInt)
		case 6:
			value = 0.0
		case 7:
			value, err = strconv.ParseFloat(cableChan.Powerlevel, 64)
		case 8:
			labelValues = append(labelValues, cableChan.Type)
			value = 1
		case 9:
                        numval := strings.Replace(cableChan.Type, "QAM", "", -1)
			ofdmK := false
			if (strings.Contains(numval, "K")) {
				numval = strings.Replace(numval, "K", "", -1)
				ofdmK = true
			}
			convval, _ := strconv.ParseInt(numval, 10, 0)
			if (ofdmK) {
				convval = convval * 1024
			}
			value = float64(convval)
		default:
			continue
		}
		if err != nil {
			log.Errorln(err)
			e.parseFailures.WithLabelValues("docInfo").Inc()
			continue
		}
		ch <- prometheus.MustNewConstMetric(metric, prometheus.CounterValue, value, labelValues...)
	}
}

func (e *Exporter) scrape(ch chan<- prometheus.Metric) (up float64) {
	e.totalScrapes.Inc()
	e.login()

	body, err := e.fetch("data.lua")
	if err == nil {
		var docInfo FritzRoot
		byteValue, _ := ioutil.ReadAll(body)
		json.Unmarshal(byteValue, &docInfo)
		for i := 0; i < len(docInfo.Data.ChannelsDown.Channel30); i++ {
			fmt.Println("ChannelId: " + strconv.Itoa(docInfo.Data.ChannelsDown.Channel30[i].ChannelId))
		}
		body.Close()

		// downstreamChannelMetrics
		for i := 0; i < len(docInfo.Data.ChannelsDown.Channel30); i++ {
			parseDownstreamChannels(ch, e, "SC-QAM", docInfo.Data.ChannelsDown.Channel30[i])
		}
		for i := 0; i < len(docInfo.Data.ChannelsDown.Channel31); i++ {
			parseDownstreamChannels(ch, e, "OFDM", docInfo.Data.ChannelsDown.Channel31[i])
		}
		for i := 0; i < len(docInfo.Data.ChannelsUp.Channel30); i++ {
			parseUpstreamChannels(ch, e, "ATDMA", docInfo.Data.ChannelsUp.Channel30[i])
		}
		for i := 0; i < len(docInfo.Data.ChannelsUp.Channel31); i++ {
			parseUpstreamChannels(ch, e, "OFDM", docInfo.Data.ChannelsUp.Channel31[i])
		}
	}
	return 1
}
