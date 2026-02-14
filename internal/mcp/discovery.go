package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	// ServiceName is the mDNS/DNS-SD service name for GT MCP servers.
	ServiceName = "_gastown._tcp"
	// DiscoveryTimeout is the default timeout for LAN discovery.
	DiscoveryTimeout = 5 * time.Second
)

// ServiceInfo describes a discovered MCP server on the network.
type ServiceInfo struct {
	Host     string            `json:"host"`
	Port     int               `json:"port"`
	URL      string            `json:"url"`
	Metadata map[string]string `json:"metadata,omitempty"` // rig, role, version
}

// Discovery handles finding GT MCP servers on the local network.
// Currently uses a simple HTTP-based probe approach. Can be extended
// to use mDNS/DNS-SD (github.com/hashicorp/mdns) for zero-config LAN discovery.
type Discovery struct {
	mu       sync.Mutex
	services []ServiceInfo
}

// NewDiscovery creates a discovery instance.
func NewDiscovery() *Discovery {
	return &Discovery{}
}

// Probe checks a specific host:port for a GT MCP server.
// This is the simplest discovery method — just check known addresses.
func (d *Discovery) Probe(ctx context.Context, host string, port int) (*ServiceInfo, error) {
	url := fmt.Sprintf("http://%s:%d", host, port)
	healthURL := url + "/mcp/health"

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("probing %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("probe returned %d: %s", resp.StatusCode, string(body))
	}

	var health map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, fmt.Errorf("decoding health: %w", err)
	}

	info := &ServiceInfo{
		Host:     host,
		Port:     port,
		URL:      url,
		Metadata: make(map[string]string),
	}

	// Extract metadata from health response
	if status, ok := health["status"].(string); ok {
		info.Metadata["status"] = status
	}
	if workDir, ok := health["work_dir"].(string); ok {
		info.Metadata["work_dir"] = workDir
	}

	return info, nil
}

// ScanSubnet probes all hosts on a subnet for GT MCP servers.
// Useful for LAN setups: ScanSubnet(ctx, "192.168.1", 9500)
func (d *Discovery) ScanSubnet(ctx context.Context, subnetPrefix string, port int) ([]ServiceInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var mu sync.Mutex
	var results []ServiceInfo
	var wg sync.WaitGroup

	// Scan .1 through .254 concurrently with limited parallelism
	sem := make(chan struct{}, 50) // limit concurrent probes

	for i := 1; i < 255; i++ {
		host := fmt.Sprintf("%s.%d", subnetPrefix, i)
		wg.Add(1)

		go func(h string) {
			defer wg.Done()
			sem <- struct{}{} // acquire
			defer func() { <-sem }()

			info, err := d.Probe(ctx, h, port)
			if err == nil {
				mu.Lock()
				results = append(results, *info)
				mu.Unlock()
			}
		}(host)
	}

	wg.Wait()

	d.mu.Lock()
	d.services = results
	d.mu.Unlock()

	return results, nil
}

// ProbeKnownHosts checks a list of known hosts for MCP servers.
// This is more targeted than subnet scanning.
func (d *Discovery) ProbeKnownHosts(ctx context.Context, hosts []string, port int) ([]ServiceInfo, error) {
	var mu sync.Mutex
	var results []ServiceInfo
	var wg sync.WaitGroup

	for _, host := range hosts {
		wg.Add(1)
		go func(h string) {
			defer wg.Done()
			info, err := d.Probe(ctx, h, port)
			if err == nil {
				mu.Lock()
				results = append(results, *info)
				mu.Unlock()
			}
		}(host)
	}

	wg.Wait()

	d.mu.Lock()
	d.services = results
	d.mu.Unlock()

	return results, nil
}

// LastDiscovered returns the results from the most recent discovery scan.
func (d *Discovery) LastDiscovered() []ServiceInfo {
	d.mu.Lock()
	defer d.mu.Unlock()

	result := make([]ServiceInfo, len(d.services))
	copy(result, d.services)
	return result
}

// LocalIP returns the machine's local network IP address.
// Useful for advertising this machine's MCP server.
func LocalIP() (string, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", fmt.Errorf("detecting local IP: %w", err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String(), nil
}
