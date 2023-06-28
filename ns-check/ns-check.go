package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"
)

const (
	defaultResolvConfPath    = "/etc/resolv.conf"
	defaultEndpointURL       = "http://127.0.0.1:5353/nameservers"
	defaultDefaultNameserver = "8.8.8.8,8.8.4.4,1.1.1.1"
	defaultInterval          = 30 * time.Second
	defaultNSTimeout         = 2 * time.Second
	defaultFetchTimeout      = 2 * time.Second
	defaultMaxNameservers    = 3
)

var (
	resolvConfPath    string
	endpointURL       string
	defaultNameserver string
	interval          time.Duration
	nsTimeout         time.Duration
	fetchTimeout      time.Duration
	maxNameservers    int
	options           = "timeout:1 attempts:1"
	search            = "localhost"
	httpClient        http.Client
)

type latencyResult struct {
	err        error
	nameserver string
	latency    time.Duration
}

func main() {
	// 解析命令行参数
	parseFlags()

	// 监听系统信号，用于优雅地退出
	setupSignalHandler()

	// 启动循环检测
	run()

	// 等待信号，阻塞主程序
	waitForSignal()
}

func parseFlags() {
	flag.StringVar(&resolvConfPath, "resolv-conf", defaultResolvConfPath, "Path to resolv.conf file")
	flag.StringVar(&endpointURL, "endpoint-url", defaultEndpointURL, "URL for fetching nameservers if resolv.conf is unavailable")
	flag.StringVar(&defaultNameserver, "default-nameserver", defaultDefaultNameserver, "Default nameserver fallback")
	flag.DurationVar(&interval, "interval", defaultInterval, "Interval between each round of detection")
	flag.DurationVar(&nsTimeout, "ns-check-timeout", defaultNSTimeout, "Timeout for nameserver connectivity check")
	flag.DurationVar(&fetchTimeout, "fetch-timeout", defaultNSTimeout, "Timeout for fetch data from endpoint url")
	flag.IntVar(&maxNameservers, "max-nameservers", defaultMaxNameservers, "Maximum number of nameservers to write back to resolv.conf")
	flag.StringVar(&options, "options", options, "Options field in resolv.conf")
	flag.StringVar(&search, "search", search, "Search field in resolv.conf")

	flag.Parse()
}

func setupSignalHandler() {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-signalChan
		log.Println("Received termination signal. Exiting...")
		os.Exit(0)
	}()
}

func run() {
	httpClient = http.Client{
		Timeout: fetchTimeout,
	}
	for {
		// 收集nameservers
		nameservers, err := collectNameservers()
		log.Println("Collect nameservers are", nameservers)
		if err != nil {
			log.Println("Failed to collect nameservers:", err)
			continue
		}

		// 检测并排序nameservers
		sortedNameservers, latencyResults := sortNameservers(nameservers)
		bestNameservers := getMaxNameservers(sortedNameservers)

		// 写回resolv.conf
		err = writeResolvConf(bestNameservers)
		if err != nil {
			log.Println("Failed to write resolv.conf:", err)
		}
		log.Printf("Nameserver info %#v", latencyResults)
		log.Println("Nameserver detection completed, best nameservers are", bestNameservers)

		// 间隔一段时间后再次执行检测
		time.Sleep(interval)
	}
}

func addNameservers(nameservers []string, nameserverSet map[string]bool) {
	for _, ns := range nameservers {
		nameserverSet[ns] = true
	}
}

func getNameservers(nameserverSet map[string]bool) []string {
	nameservers := make([]string, 0, len(nameserverSet))
	for ns := range nameserverSet {
		nameservers = append(nameservers, ns)
	}
	return nameservers
}

func getDefaultNameservers(defaultNameserver string) []string {
	return strings.Split(defaultNameserver, ",")
}

func collectNameservers() ([]string, error) {
	var nameserverSet = make(map[string]bool)
	// 尝试从resolv.conf中读取nameservers
	nameservers, err := readNameserversFromResolvConf()
	if err == nil && len(nameservers) > 0 {
		log.Println("Collect nameservers from resolv.conf are", nameservers)
		addNameservers(nameservers, nameserverSet)
	} else {
		log.Println("Collect nameserver from resolv.conf failed:", err)
	}

	// 从endpointURL获取nameservers
	lastEndpointURL := endpointURL
	nameservers, endpointURL, err = fetchNameserversFromEndpoint(endpointURL)
	if err == nil && len(nameservers) > 0 {
		log.Printf("Collect nameservers from endpoint url %s are %v, new endpoint url is %s", lastEndpointURL, nameservers, endpointURL)
		addNameservers(nameservers, nameserverSet)
	} else {
		log.Println("Collect nameserver from endpoint url failed:", err)
	}

	defaultNameservers := getDefaultNameservers(defaultNameserver)
	log.Println("Collect nameservers from default are", defaultNameservers)
	addNameservers(defaultNameservers, nameserverSet)
	// 返回默认的fallback nameserver
	return getNameservers(nameserverSet), nil
}

func readNameserversFromResolvConf() ([]string, error) {
	file, err := os.Open(resolvConfPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var nameservers []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "nameserver") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				nameservers = append(nameservers, fields[1])
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return nameservers, nil
}

func fetchNameserversFromEndpoint(url string) ([]string, string, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, endpointURL, err
	}
	defer resp.Body.Close()

	var data struct {
		Nameservers []string `json:"nameservers"`
		EndpointURL string   `json:"endpointURL"`
	}

	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		return nil, endpointURL, err
	}
	if data.EndpointURL == "" {
		data.EndpointURL = endpointURL
	}

	return data.Nameservers, data.EndpointURL, nil
}

func sortNameservers(nameservers []string) ([]string, []latencyResult) {

	results := make([]latencyResult, 0, len(nameservers))
	latencyResults := make([]latencyResult, 0)

	// 并发检测nameserver延迟
	resultChan := make(chan latencyResult, len(nameservers))
	for _, ns := range nameservers {
		go func(nameserver string) {
			latency, err := measureLatency(nameserver)
			resultChan <- latencyResult{err: err, nameserver: nameserver, latency: latency}
		}(ns)
	}

	for range nameservers {
		result := <-resultChan
		if result.err == nil {
			results = append(results, result)
		}
	}

	// 根据延迟排序nameservers
	sort.Slice(results, func(i, j int) bool {
		return results[i].latency < results[j].latency
	})

	sortedNameservers := make([]string, 0, len(results))
	for _, result := range results {
		latencyResults = append(latencyResults, result)
		sortedNameservers = append(sortedNameservers, result.nameserver)
	}

	return sortedNameservers, latencyResults
}

func getMaxNameservers(nameservers []string) []string {
	if len(nameservers) >= maxNameservers {
		return nameservers[:maxNameservers]
	}
	return nameservers
}

func measureLatency(nameserver string) (time.Duration, error) {
	startTime := time.Now()

	conn, err := net.DialTimeout("tcp", nameserver+":53", nsTimeout)
	if err != nil {
		// conn.Close()
		log.Printf("Nameserver %s healthy check: %v", nameserver, err)
		return math.MaxInt64, err
	}
	conn.Close()
	return time.Since(startTime), nil
}

func writeResolvConf(nameservers []string) error {
	file, err := os.Create(resolvConfPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// 写入nameservers
	for _, ns := range nameservers {
		_, err := file.WriteString("nameserver " + ns + "\n")
		if err != nil {
			return err
		}
	}

	// 写入options和search字段
	if options != "" {
		_, err = file.WriteString("options " + options + "\n")
		if err != nil {
			return err
		}
	}

	if search != "" {
		_, err = file.WriteString("search " + search + "\n")
		if err != nil {
			return err
		}
	}

	return nil
}

func waitForSignal() {
	select {}
}
