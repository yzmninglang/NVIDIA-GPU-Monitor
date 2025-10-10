package main

import (
	"embed"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed index.html
var indexHTML embed.FS

// NodeConfig represents a node configuration
type NodeConfig struct {
	Name  string `json:"name"`
	Host  string `json:"host"`
	Port  int    `json:"port"`
	Alias string `json:"alias"`
}

// AggregatorConfig represents the aggregator configuration
type AggregatorConfig struct {
	Nodes      []NodeConfig `json:"nodes"`
	Aggregator struct {
		Port int `json:"port"`
	} `json:"aggregator"`
}

// GPUInfo represents the information of a single GPU
type GPUInfo struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Utilization   float64       `json:"utilization"`
	MemoryUsed    uint64        `json:"memory_used"`
	MemoryTotal   uint64        `json:"memory_total"`
	Temperature   uint32        `json:"temperature"`
	PowerUsage    uint64        `json:"power_usage"`
	PowerLimit    uint64        `json:"power_limit"`
	Processes     []ProcessInfo `json:"processes"`
}

// ProcessInfo represents information about a process using GPU
type ProcessInfo struct {
	PID  uint32 `json:"pid"`
	Name string `json:"name"`
	Used uint64 `json:"used"`
}

// NodeInfo represents the information of a node
type NodeInfo struct {
	NodeName    string    `json:"node_name"`
	Timestamp   time.Time `json:"timestamp"`
	GPUs        []GPUInfo `json:"gpus"`
}

// NodeStatus represents the status of a node
type NodeStatus struct {
	NodeConfig
	LastUpdate time.Time `json:"last_update"`
	Status     string    `json:"status"` // "online", "offline", "error"
	Data       *NodeInfo `json:"data,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// Aggregator holds the state of the aggregator
type Aggregator struct {
	config  AggregatorConfig
	nodes   map[string]*NodeStatus
	mutex   sync.RWMutex
	client  *http.Client
}

// SMIOutput represents the structure of nvidia-smi XML output
type SMIOutput struct {
	AttachedGPUs int   `xml:"attached_gpus"`
	GPUs         []GPU `xml:"gpu"`
}

// GPU represents a single GPU device
type GPU struct {
	ID          string    `xml:"id,attr"`
	ProductName string    `xml:"product_name"`
	FBMemory    Memory    `xml:"fb_memory_usage"`
	Utilization Util      `xml:"utilization"`
	Temperature Temp      `xml:"temperature"`
	Power       Power     `xml:"gpu_power_readings"`
	Processes   Processes `xml:"processes"`
}

// Memory represents GPU memory usage
type Memory struct {
	Total string `xml:"total"`
	Used  string `xml:"used"`
	Free  string `xml:"free"`
}

// Util represents GPU utilization
type Util struct {
	GPU string `xml:"gpu_util"`
}

// Temp represents GPU temperature
type Temp struct {
	GPUTemp string `xml:"gpu_temp"`
}

// Power represents GPU power usage
type Power struct {
	PowerDraw string `xml:"instant_power_draw"`
	PowerLimit string `xml:"current_power_limit"`
}

// Processes represents running processes
type Processes struct {
	ProcessInfo []Process `xml:"process_info"`
}

// Process represents a single process
type Process struct {
	PID         string `xml:"pid"`
	ProcessName string `xml:"process_name"`
	UsedMemory  string `xml:"used_memory"`
}

func main() {
	// Define command line flags
	mode := flag.String("mode", "aggregator", "Run mode: 'server' or 'aggregator'")
	port := flag.String("port", "", "Port to listen on (overrides config)")
	configFile := flag.String("config", "config.json", "Path to config file")
	flag.Parse()

	switch *mode {
	case "server":
		runServer(*port)
	case "aggregator":
		runAggregator(*configFile, *port)
	default:
		log.Fatalf("Invalid mode: %s. Use 'server' or 'aggregator'", *mode)
	}
}

// runServer runs the GPU info server
func runServer(port string) {
	if port == "" {
		port = "8081"
	}

	http.HandleFunc("/gpu-info", gpuInfoHandler)
	http.HandleFunc("/health", healthHandler)

	fmt.Printf("GPU Server starting on port %s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// runAggregator runs the aggregator server
func runAggregator(configFile, portOverride string) {
	// Load configuration
	config, err := loadConfig(configFile)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Override port if specified
	if portOverride != "" {
		config.Aggregator.Port, err = parsePort(portOverride)
		if err != nil {
			log.Fatalf("Invalid port: %v", err)
		}
	} else if config.Aggregator.Port == 0 {
		config.Aggregator.Port = 8080
	}

	// Create aggregator
	aggregator := &Aggregator{
		config: *config,
		nodes:  make(map[string]*NodeStatus),
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}

	// Initialize node statuses
	for _, node := range config.Nodes {
		aggregator.nodes[node.Name] = &NodeStatus{
			NodeConfig: node,
			Status:     "unknown",
		}
	}

	// Start background polling
	go aggregator.pollNodes()

	// Start HTTP server
	addr := fmt.Sprintf(":%d", config.Aggregator.Port)
	http.HandleFunc("/api/nodes", aggregator.nodesHandler)
	http.HandleFunc("/api/nodes/", aggregator.nodeHandler)
	http.Handle("/", http.FileServer(http.FS(indexHTML)))

	fmt.Printf("Aggregator server starting on %s\n", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func loadConfig(filename string) (*AggregatorConfig, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	var config AggregatorConfig
	err = json.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func parsePort(portStr string) (int, error) {
	if portStr == "" {
		return 0, fmt.Errorf("empty port string")
	}
	
	var port int
	_, err := fmt.Sscanf(portStr, "%d", &port)
	if err != nil {
		return 0, fmt.Errorf("invalid port format: %v", err)
	}
	
	if port <= 0 || port > 65535 {
		return 0, fmt.Errorf("port out of range: %d", port)
	}
	
	return port, nil
}

// GPU Server functions
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func gpuInfoHandler(w http.ResponseWriter, r *http.Request) {
	// Get GPU info using nvidia-smi
	gpus, err := getGPUInfoFromNvidiaSmi()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get GPU info: %v", err), http.StatusInternalServerError)
		return
	}

	nodeInfo := NodeInfo{
		NodeName:  getHostname(),
		Timestamp: time.Now(),
		GPUs:      gpus,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodeInfo)
}

func getGPUInfoFromNvidiaSmi() ([]GPUInfo, error) {
	// Run nvidia-smi command to get GPU information in XML format
	cmd := exec.Command("nvidia-smi", "-q", "-x")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run nvidia-smi: %v", err)
	}

	// Parse the XML output
	var smiOutput SMIOutput
	err = xml.Unmarshal(output, &smiOutput)
	if err != nil {
		return nil, fmt.Errorf("failed to parse nvidia-smi XML output: %v", err)
	}

	// Convert to our GPUInfo format
	gpus := make([]GPUInfo, len(smiOutput.GPUs))
	for i, gpu := range smiOutput.GPUs {
		// Parse utilization
		utilization := 0.0
		if strings.HasSuffix(gpu.Utilization.GPU, " %") {
			utilStr := strings.TrimSuffix(gpu.Utilization.GPU, " %")
			utilization, _ = strconv.ParseFloat(utilStr, 64)
		}
		
		// Parse memory
		memoryUsed := parseMemoryValue(gpu.FBMemory.Used)
		memoryTotal := parseMemoryValue(gpu.FBMemory.Total)
		
		// Parse temperature
		temperature := uint32(0)
		if strings.HasSuffix(gpu.Temperature.GPUTemp, " C") {
			tempStr := strings.TrimSuffix(gpu.Temperature.GPUTemp, " C")
			tempVal, _ := strconv.ParseUint(tempStr, 10, 32)
			temperature = uint32(tempVal)
		}
		
		// Parse power
		powerUsage := parsePowerValue(gpu.Power.PowerDraw)
		powerLimit := parsePowerValue(gpu.Power.PowerLimit)
		
		// Convert processes
		processes := make([]ProcessInfo, len(gpu.Processes.ProcessInfo))
		for j, proc := range gpu.Processes.ProcessInfo {
			usedMemory := parseMemoryValue(proc.UsedMemory)
			pid, _ := strconv.ParseUint(proc.PID, 10, 32)
			
			processes[j] = ProcessInfo{
				PID:  uint32(pid),
				Name: proc.ProcessName,
				Used: usedMemory,
			}
		}
		
		gpus[i] = GPUInfo{
			ID:          gpu.ID,
			Name:        gpu.ProductName,
			Utilization: utilization,
			MemoryUsed:  memoryUsed,
			MemoryTotal: memoryTotal,
			Temperature: temperature,
			PowerUsage:  powerUsage,
			PowerLimit:  powerLimit,
			Processes:   processes,
		}
	}
	
	return gpus, nil
}

func parseMemoryValue(value string) uint64 {
	// Parse memory value like "1024 MiB" or "1 GiB"
	value = strings.TrimSpace(value)
	
	// Handle MiB
	if strings.HasSuffix(value, "MiB") {
		numStr := strings.TrimSuffix(value, " MiB")
		num, _ := strconv.ParseFloat(numStr, 64)
		return uint64(num * 1024 * 1024)
	}
	
	// Handle GiB
	if strings.HasSuffix(value, "GiB") {
		numStr := strings.TrimSuffix(value, " GiB")
		num, _ := strconv.ParseFloat(numStr, 64)
		return uint64(num * 1024 * 1024 * 1024)
	}
	
	// Handle KiB
	if strings.HasSuffix(value, "KiB") {
		numStr := strings.TrimSuffix(value, " KiB")
		num, _ := strconv.ParseFloat(numStr, 64)
		return uint64(num * 1024)
	}
	
	// Handle bytes
	if strings.HasSuffix(value, "B") && !strings.Contains(value, "iB") {
		numStr := strings.TrimSuffix(value, "B")
		num, _ := strconv.ParseFloat(numStr, 64)
		return uint64(num)
	}
	
	return 0
}

func parsePowerValue(value string) uint64 {
	// Parse power value like "250.00 W"
	value = strings.TrimSpace(value)
	
	if strings.HasSuffix(value, "W") {
		numStr := strings.TrimSuffix(value, " W")
		num, _ := strconv.ParseFloat(numStr, 64)
		return uint64(num * 1000) // Convert to milliwatts
	}
	
	// Handle "N/A" or empty values
	if value == "N/A" || value == "" {
		return 0
	}
	
	// Try to parse as a number directly
	num, err := strconv.ParseFloat(value, 64)
	if err == nil {
		return uint64(num * 1000) // Assume it's in watts, convert to milliwatts
	}
	
	return 0
}

func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown-host"
	}
	return hostname
}

// Aggregator functions
func (a *Aggregator) pollNodes() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		a.updateNodeStatuses()
		<-ticker.C
	}
}

func (a *Aggregator) updateNodeStatuses() {
	var wg sync.WaitGroup

	for _, node := range a.config.Nodes {
		wg.Add(1)
		go func(node NodeConfig) {
			defer wg.Done()
			a.updateNodeStatus(node)
		}(node)
	}

	wg.Wait()
}

func (a *Aggregator) updateNodeStatus(node NodeConfig) {
	url := fmt.Sprintf("http://%s:%d/gpu-info", node.Host, node.Port)
	
	// Create request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		a.updateNodeError(node.Name, fmt.Sprintf("Failed to create request: %v", err))
		return
	}

	// Make request
	resp, err := a.client.Do(req)
	if err != nil {
		a.updateNodeError(node.Name, fmt.Sprintf("Failed to connect: %v", err))
		return
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		a.updateNodeError(node.Name, fmt.Sprintf("HTTP error: %d", resp.StatusCode))
		return
	}

	// Parse response
	var nodeInfo NodeInfo
	err = json.NewDecoder(resp.Body).Decode(&nodeInfo)
	if err != nil {
		a.updateNodeError(node.Name, fmt.Sprintf("Failed to parse response: %v", err))
		return
	}

	// Update node status
	a.mutex.Lock()
	if status, exists := a.nodes[node.Name]; exists {
		status.Status = "online"
		status.LastUpdate = time.Now()
		status.Data = &nodeInfo
		status.Error = ""
	}
	a.mutex.Unlock()
}

func (a *Aggregator) updateNodeError(nodeName, errorMsg string) {
	a.mutex.Lock()
	if status, exists := a.nodes[nodeName]; exists {
		status.Status = "offline"
		status.LastUpdate = time.Now()
		status.Data = nil
		status.Error = errorMsg
	}
	a.mutex.Unlock()
}

func (a *Aggregator) nodesHandler(w http.ResponseWriter, r *http.Request) {
	a.mutex.RLock()
	nodes := make([]*NodeStatus, 0, len(a.nodes))
	for _, node := range a.nodes {
		nodes = append(nodes, node)
	}
	a.mutex.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}

func (a *Aggregator) nodeHandler(w http.ResponseWriter, r *http.Request) {
	nodeName := r.URL.Path[len("/api/nodes/"):]
	
	a.mutex.RLock()
	node, exists := a.nodes[nodeName]
	a.mutex.RUnlock()

	if !exists {
		http.Error(w, "Node not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(node)
}