package main

import (
	"embed"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"net/http"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// GPU a gpu device
type GPU struct {
	ID          string `xml:"id,attr"`
	ProductName string `xml:"product_name"`
	MemoryTotal string `xml:"fb_memory_usage>total"`
	MemoryUsed  string `xml:"fb_memory_usage>used"`
	Utilization string `xml:"utilization>gpu_util"`
	PowerDraw   string `xml:"gpu_power_readings>power_draw"`
	Processes   []Process `xml:"processes>process_info"`
}

// Process represents a process running on a GPU
type Process struct {
	Pid         string `xml:"pid"`
	ProcessName string `xml:"process_name" json:"process_name"`
	UsedMemory  string `xml:"used_memory" json:"used_memory"`
	Username    string `json:"username"`
}

//go:embed index.html
var indexHTML embed.FS

// SMIOutput for nvidia-smi output
type SMIOutput struct {
	AttachedGPUs []GPU `xml:"gpu"`
}

func getGPUInfo() (*SMIOutput, error) {
	cmd := exec.Command("nvidia-smi", "-q", "-x")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to run nvidia-smi: %w, output: %s", err, string(output))
	}

	var smiOutput SMIOutput
	err = xml.Unmarshal(output, &smiOutput)
	if err != nil {
		return nil, fmt.Errorf("failed to parse nvidia-smi xml output: %w", err)
	}

	return &smiOutput, nil
}

func getUsernameFromPid(pid string) string {
	cmd := exec.Command("ps", "-o", "user=", "-p", pid)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "N/A"
	}
	return strings.TrimSpace(string(out))
}

func gpuInfoHandler(w http.ResponseWriter, r *http.Request) {
	gpuInfo, err := getGPUInfo()
	if err != nil {
		http.Error(w, fmt.Sprintf("Error getting GPU info: %v", err), http.StatusInternalServerError)
		return
	}

	for i := range gpuInfo.AttachedGPUs {
		gpu := &gpuInfo.AttachedGPUs[i]
		for j := range gpu.Processes {
			proc := &gpu.Processes[j]
			proc.Username = getUsernameFromPid(proc.Pid)
		}

		sort.Slice(gpu.Processes, func(k, l int) bool {
			memK, _ := strconv.Atoi(strings.Fields(gpu.Processes[k].UsedMemory)[0])
			memL, _ := strconv.Atoi(strings.Fields(gpu.Processes[l].UsedMemory)[0])
			return memK > memL
		})

		if len(gpu.Processes) > 2 {
			gpu.Processes = gpu.Processes[:2]
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(gpuInfo.AttachedGPUs)
}

func main() {
	port := flag.String("port", "8080", "port to serve on")
	flag.Parse()

	addr := ":" + *port

	http.HandleFunc("/api/gpu", gpuInfoHandler)
	http.Handle("/", http.FileServer(http.FS(indexHTML)))

	fmt.Printf("NVIDIA GPU Monitor Starting on %s\n", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		fmt.Printf("Error starting server: %v\n", err)
	}
}