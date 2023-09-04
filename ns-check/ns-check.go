package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultLogFile           = "./ns-check.log"
	defaultResolvConfPath    = "/etc/resolv.conf"
	defaultEndpointURL       = "http://127.0.0.1:5353/nameservers"
	defaultDefaultNameserver = "8.8.8.8,8.8.4.4,1.1.1.1"
	defaultInterval          = 30 * time.Second
	defaultNSTimeout         = 2 * time.Second
	defaultFetchTimeout      = 2 * time.Second
	defaultMaxNameservers    = 3
)

// Config 管理配置项的结构
type Config struct {
	LogFile           string        // 日志文件路径
	ResolvConfPath    string        // resolv.conf 文件路径
	EndpointURL       string        // 获取名字服务器列表的端点 URL
	DefaultNameserver string        // 默认名字服务器列表
	Interval          time.Duration // 检测间隔
	NSTimeout         time.Duration // 名字服务器连接超时
	FetchTimeout      time.Duration // 获取名字服务器列表超时
	MaxNameservers    int           // 最大名字服务器数量
	Options           string        // resolv.conf 中的 options 字段
	Search            string        // resolv.conf 中的 search 字段
}

// NewConfig 创建并初始化配置对象
func NewConfig() *Config {
	return &Config{
		LogFile:           defaultLogFile,
		ResolvConfPath:    defaultResolvConfPath,
		EndpointURL:       defaultEndpointURL,
		DefaultNameserver: defaultDefaultNameserver,
		Interval:          defaultInterval,
		NSTimeout:         defaultNSTimeout,
		FetchTimeout:      defaultFetchTimeout,
		MaxNameservers:    defaultMaxNameservers,
	}
}

// Logger 日志记录器结构
type Logger struct {
	logger *log.Logger
}

// NewLogger 创建并初始化日志记录器
func NewLogger(logFile string) *Logger {
	file, err := os.Create(logFile)
	if err != nil {
		log.Fatalf("Failed to create log file: %v", err)
	}
	return &Logger{
		logger: log.New(file, "ns-check", log.Llongfile),
	}
}

// NameServerDetector 名字服务器检测器结构
type NameServerDetector struct {
	config *Config
}

// NewNameServerDetector 创建并初始化名字服务器检测器
func NewNameServerDetector(config *Config) *NameServerDetector {
	return &NameServerDetector{
		config: config,
	}
}

// Start 启动名字服务器检测器
func (nsd *NameServerDetector) Start(nsManager *NameServerManager, logger *Logger) {
	for {
		// 收集名字服务器
		nameservers, err := nsManager.CollectNameServers()
		if err != nil {
			logger.logger.Printf("Failed to collect nameservers: %v", err)
			continue
		}

		// 检测并排序名字服务器
		sortedNameservers, latencyResults := nsManager.SortNameServers(nameservers)
		bestNameservers := nsManager.GetMaxNameservers(sortedNameservers)

		// 写回 resolv.conf
		err = nsManager.WriteResolvConf(bestNameservers)
		if err != nil {
			logger.logger.Printf("Failed to write resolv.conf: %v", err)
		}

		logger.logger.Printf("Nameserver info %#v", latencyResults)
		logger.logger.Printf("Nameserver detection completed, best nameservers are %v", bestNameservers)

		// 间隔一段时间后再次执行检测
		time.Sleep(nsd.config.Interval)
	}
}

// NameServerManager 名字服务器管理器结构
type NameServerManager struct {
	config *Config
}

// NewNameServerManager 创建并初始化名字服务器管理器
func NewNameServerManager(config *Config) *NameServerManager {
	return &NameServerManager{
		config: config,
	}
}

// CollectNameServers 收集名字服务器的逻辑，包括从文件和网络获取
func (nsm *NameServerManager) CollectNameServers() ([]string, error) {
	nameservers := make([]string, 0)
	nameserverSet := make(map[string]bool)

	// 尝试从 resolv.conf 中读取名字服务器
	fileNameservers, err := nsm.readNameserversFromResolvConf()
	if err == nil && len(fileNameservers) > 0 {
		nameservers = append(nameservers, fileNameservers...)
		nsm.addNameserversToSet(nameservers, nameserverSet)
	} else {
		return nil, fmt.Errorf("failed to read nameservers from resolv.conf: %v", err)
	}

	// 从网络端点获取名字服务器
	endpointNameservers, err := nsm.fetchNameserversFromEndpoint(nsm.config.EndpointURL)
	if err == nil && len(endpointNameservers) > 0 {
		nameservers = append(nameservers, endpointNameservers...)
		nsm.addNameserversToSet(endpointNameservers, nameserverSet)
	} else {
		return nil, fmt.Errorf("failed to fetch nameservers from endpoint URL: %v", err)
	}

	// 添加默认名字服务器
	defaultNameservers := strings.Split(nsm.config.DefaultNameserver, ",")
	nameservers = append(nameservers, defaultNameservers...)
	nsm.addNameserversToSet(nameservers, nameserverSet)

	// 返回去重后的名字服务器列表
	return nsm.getNameserversFromSet(nameserverSet), nil
}

// readNameserversFromResolvConf 从 resolv.conf 文件读取名字服务器
func (nsm *NameServerManager) readNameserversFromResolvConf() ([]string, error) {
	file, err := os.Open(nsm.config.ResolvConfPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	nameservers := make([]string, 0)
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

// fetchNameserversFromEndpoint 从网络端点获取名字服务器
func (nsm *NameServerManager) fetchNameserversFromEndpoint(url string) ([]string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data struct {
		Nameservers []string `json:"nameservers"`
	}

	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		return nil, err
	}

	return data.Nameservers, nil
}

// addNameserversToSet 将名字服务器添加到集合中
func (nsm *NameServerManager) addNameserversToSet(nameservers []string, nameserverSet map[string]bool) {
	for _, ns := range nameservers {
		nameserverSet[ns] = true
	}
}

// getNameserversFromSet 从集合中获取名字服务器列表
func (nsm *NameServerManager) getNameserversFromSet(nameserverSet map[string]bool) []string {
	nameservers := make([]string, 0, len(nameserverSet))
	for ns := range nameserverSet {
		nameservers = append(nameservers, ns)
	}
	return nameservers
}

// SortNameServers 排序名字服务器的逻辑
func (nsm *NameServerManager) SortNameServers(nameservers []string) ([]string, []latencyResult) {
	results := make([]latencyResult, 0)
	latencyResults := make([]latencyResult, 0)

	// 使用 WaitGroup 等待所有 goroutine 完成
	var wg sync.WaitGroup

	for _, ns := range nameservers {
		wg.Add(1)
		go func(nameserver string) {
			defer wg.Done()
			latency, err := nsm.measureLatency(nameserver)
			result := latencyResult{err: err, nameserver: nameserver, latency: latency}
			results = append(results, result)
		}(ns)
	}

	// 等待所有 goroutine 完成
	wg.Wait()

	// 根据延迟排序名字服务器
	sort.Slice(results, func(i, j int) bool {
		return results[i].latency < results[j].latency
	})

	sortedNameservers := make([]string, 0)
	for _, result := range results {
		latencyResults = append(latencyResults, result)
		sortedNameservers = append(sortedNameservers, result.nameserver)
	}

	return sortedNameservers, latencyResults
}

// measureLatency 测量名字服务器的延迟
func (nsm *NameServerManager) measureLatency(nameserver string) (time.Duration, error) {
	startTime := time.Now()

	conn, err := net.DialTimeout("tcp", nameserver+":53", nsm.config.NSTimeout)
	if err != nil {
		return 0, err
	}
	conn.Close()

	return time.Since(startTime), nil
}

// GetMaxNameservers 获取最多指定数量的名字服务器
func (nsm *NameServerManager) GetMaxNameservers(nameservers []string) []string {
	if len(nameservers) >= nsm.config.MaxNameservers {
		return nameservers[:nsm.config.MaxNameservers]
	}
	return nameservers
}

// WriteResolvConf 写入名字服务器配置到 resolv.conf 的逻辑
func (nsm *NameServerManager) WriteResolvConf(nameservers []string) error {
	file, err := os.Create(nsm.config.ResolvConfPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// 写入 nameservers
	for _, ns := range nameservers {
		_, err := file.WriteString("nameserver " + ns + "\n")
		if err != nil {
			return err
		}
	}

	// 写入 options 和 search 字段
	if nsm.config.Options != "" {
		_, err = file.WriteString("options " + nsm.config.Options + "\n")
		if err != nil {
			return err
		}
	}

	if nsm.config.Search != "" {
		_, err = file.WriteString("search " + nsm.config.Search + "\n")
		if err != nil {
			return err
		}
	}

	return nil
}

// latencyResult 包含名字服务器延迟信息的结构
type latencyResult struct {
	err        error
	nameserver string
	latency    time.Duration
}

func main() {
	// 创建一个配置对象，用于管理配置项
	config := NewConfig()

	// 创建一个名字服务器检测器，使用策略模式处理不同类型的名字服务器检测
	nsDetector := NewNameServerDetector(config)

	// 创建一个名字服务器管理器，使用工厂模式创建不同类型的名字服务器
	nsManager := NewNameServerManager(config)

	// 创建一个日志记录器
	logger := NewLogger(config.LogFile)

	// 注册信号处理函数，用于优雅地退出
	registerSignalHandler(logger)

	// 启动名字服务器检测器
	go nsDetector.Start(nsManager, logger)

	// 阻塞主程序
	select {}
}

// registerSignalHandler 注册信号处理函数，用于捕获退出信号
func registerSignalHandler(logger *Logger) {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-signalChan
		logger.logger.Println("Received termination signal. Exiting...")
		os.Exit(0)
	}()
}
