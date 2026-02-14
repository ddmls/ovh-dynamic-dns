package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ovh/go-ovh/ovh"
)

// Config represents the top-level configuration file.
type Config struct {
	OVH     OVHConfig      `json:"ovh"`
	Domains []DomainConfig `json:"domains"`
}

// OVHConfig holds OVH API credentials.
type OVHConfig struct {
	Endpoint          string `json:"endpoint"`
	ApplicationKey    string `json:"application_key"`
	ApplicationSecret string `json:"application_secret"`
	ConsumerKey       string `json:"consumer_key"`
}

// DomainConfig holds a single domain entry to update.
type DomainConfig struct {
	Zone      string `json:"zone"`
	Subdomain string `json:"subdomain"`
	TTL       int    `json:"ttl"`
}

// DNSRecord represents an OVH DNS record.
type DNSRecord struct {
	ID        int    `json:"id"`
	FieldType string `json:"fieldType"`
	SubDomain string `json:"subDomain"`
	Target    string `json:"target"`
	TTL       int    `json:"ttl"`
}

// OVHDynamicDNS wraps the OVH API client for DNS operations.
type OVHDynamicDNS struct {
	client *ovh.Client
}

// NewOVHDynamicDNS creates a new OVH API client.
func NewOVHDynamicDNS(endpoint, appKey, appSecret, consumerKey string) (*OVHDynamicDNS, error) {
	client, err := ovh.NewClient(endpoint, appKey, appSecret, consumerKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create OVH client: %w", err)
	}
	return &OVHDynamicDNS{client: client}, nil
}

// getPublicIP tries multiple services to detect the current public IPv4 address.
func getPublicIP() (string, error) {
	services := []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
		"https://icanhazip.com",
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}

	for _, svc := range services {
		resp, err := httpClient.Get(svc)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			continue
		}

		ip := strings.TrimSpace(string(body))
		parsed := net.ParseIP(ip)
		if parsed == nil || parsed.To4() == nil {
			fmt.Fprintf(os.Stderr, "Error: Invalid IPv4 address detected: %s\n", ip)
			return "", fmt.Errorf("invalid IPv4 address: %s", ip)
		}
		return ip, nil
	}

	fmt.Fprintln(os.Stderr, "Error: Could not determine public IP address")
	return "", fmt.Errorf("could not determine public IP address")
}

// getDNSRecords fetches A records for the given zone and subdomain.
func (d *OVHDynamicDNS) getDNSRecords(zone, subdomain string) ([]DNSRecord, error) {
	path := fmt.Sprintf("/domain/zone/%s/record?fieldType=A&subDomain=%s",
		url.PathEscape(zone), url.QueryEscape(subdomain))

	var recordIDs []int
	if err := d.client.Get(path, &recordIDs); err != nil {
		return nil, fmt.Errorf("error getting DNS records: %w", err)
	}

	records := make([]DNSRecord, 0, len(recordIDs))
	for _, id := range recordIDs {
		var rec DNSRecord
		recPath := fmt.Sprintf("/domain/zone/%s/record/%d", url.PathEscape(zone), id)
		if err := d.client.Get(recPath, &rec); err != nil {
			return nil, fmt.Errorf("error getting DNS record %d: %w", id, err)
		}
		records = append(records, rec)
	}
	return records, nil
}

// createDNSRecord creates a new A record and refreshes the zone.
func (d *OVHDynamicDNS) createDNSRecord(zone, subdomain, target string, ttl int) error {
	path := fmt.Sprintf("/domain/zone/%s/record", url.PathEscape(zone))
	body := struct {
		FieldType string `json:"fieldType"`
		SubDomain string `json:"subDomain"`
		Target    string `json:"target"`
		TTL       int    `json:"ttl"`
	}{
		FieldType: "A",
		SubDomain: subdomain,
		Target:    target,
		TTL:       ttl,
	}

	if err := d.client.Post(path, &body, nil); err != nil {
		return fmt.Errorf("error creating DNS record: %w", err)
	}

	refreshPath := fmt.Sprintf("/domain/zone/%s/refresh", url.PathEscape(zone))
	if err := d.client.Post(refreshPath, nil, nil); err != nil {
		return fmt.Errorf("error refreshing zone: %w", err)
	}
	return nil
}

// updateDNSRecord updates an existing record and refreshes the zone.
func (d *OVHDynamicDNS) updateDNSRecord(zone string, recordID int, target string, ttl int) error {
	path := fmt.Sprintf("/domain/zone/%s/record/%d", url.PathEscape(zone), recordID)
	body := struct {
		Target string `json:"target"`
		TTL    int    `json:"ttl"`
	}{
		Target: target,
		TTL:    ttl,
	}

	if err := d.client.Put(path, &body, nil); err != nil {
		return fmt.Errorf("error updating DNS record: %w", err)
	}

	refreshPath := fmt.Sprintf("/domain/zone/%s/refresh", url.PathEscape(zone))
	if err := d.client.Post(refreshPath, nil, nil); err != nil {
		return fmt.Errorf("error refreshing zone: %w", err)
	}
	return nil
}

// deleteDNSRecord deletes a record and refreshes the zone.
func (d *OVHDynamicDNS) deleteDNSRecord(zone string, recordID int) error {
	path := fmt.Sprintf("/domain/zone/%s/record/%d", url.PathEscape(zone), recordID)

	if err := d.client.Delete(path, nil); err != nil {
		return fmt.Errorf("error deleting DNS record: %w", err)
	}

	refreshPath := fmt.Sprintf("/domain/zone/%s/refresh", url.PathEscape(zone))
	if err := d.client.Post(refreshPath, nil, nil); err != nil {
		return fmt.Errorf("error refreshing zone: %w", err)
	}
	return nil
}

// updateDynamicDNS orchestrates the DNS update for a single domain entry.
func (d *OVHDynamicDNS) updateDynamicDNS(zone, subdomain string, ttl int, currentIP string) error {
	records, err := d.getDNSRecords(zone, subdomain)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting DNS records: %v\n", err)
		return err
	}

	label := zone
	if subdomain != "" {
		label = subdomain + "." + zone
	}

	if len(records) == 0 {
		fmt.Printf("Creating new A record for %s\n", label)
		return d.createDNSRecord(zone, subdomain, currentIP, ttl)
	}

	record := records[0]
	if record.Target != currentIP {
		fmt.Printf("Updating A record for %s\n", label)
		fmt.Printf("Old IP: %s -> New IP: %s\n", record.Target, currentIP)
		return d.updateDNSRecord(zone, record.ID, currentIP, ttl)
	}

	fmt.Printf("IP address unchanged for %s\n", label)
	return nil
}

// loadConfig reads and validates the JSON configuration file.
func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			exe, _ := os.Executable()
			exeDir := filepath.Dir(exe)
			return nil, fmt.Errorf("config file not found: %s\nCopy the example and edit it:\n  cp %s %s",
				path, filepath.Join(exeDir, "config.json.example"), path)
		}
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("invalid JSON in %s: %w", path, err)
	}

	// Validate OVH credentials
	if config.OVH.Endpoint == "" {
		return nil, fmt.Errorf("config 'ovh' section missing 'endpoint'")
	}
	if config.OVH.ApplicationKey == "" {
		return nil, fmt.Errorf("config 'ovh' section missing 'application_key'")
	}
	if config.OVH.ApplicationSecret == "" {
		return nil, fmt.Errorf("config 'ovh' section missing 'application_secret'")
	}
	if config.OVH.ConsumerKey == "" {
		return nil, fmt.Errorf("config 'ovh' section missing 'consumer_key'")
	}

	// Validate domains
	if len(config.Domains) == 0 {
		return nil, fmt.Errorf("config must have a non-empty 'domains' list")
	}
	for i := range config.Domains {
		if config.Domains[i].Zone == "" {
			return nil, fmt.Errorf("domain entry %d missing 'zone'", i)
		}
		if config.Domains[i].TTL == 0 {
			config.Domains[i].TTL = 300
		}
	}

	return &config, nil
}

// getCachePath returns the path for the IP cache file.
func getCachePath() string {
	if stateDir := os.Getenv("STATE_DIRECTORY"); stateDir != "" {
		return filepath.Join(stateDir, "last_ip")
	}
	exe, err := os.Executable()
	if err != nil {
		return ".last_ip"
	}
	return filepath.Join(filepath.Dir(exe), ".last_ip")
}

// readCachedIP reads the cached IP from disk, or returns "" on any error.
func readCachedIP() string {
	data, err := os.ReadFile(getCachePath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// writeCachedIP writes the current IP to the cache file.
func writeCachedIP(ip string) {
	path := getCachePath()
	if err := os.WriteFile(path, []byte(ip), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Could not write IP cache to %s: %v\n", path, err)
	}
}

func main() {
	exe, _ := os.Executable()
	defaultConfig := filepath.Join(filepath.Dir(exe), "config.json")

	configPath := flag.String("c", defaultConfig, "Path to config file")
	flag.StringVar(configPath, "config", defaultConfig, "Path to config file")
	flag.Parse()

	config, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	dns, err := NewOVHDynamicDNS(
		config.OVH.Endpoint,
		config.OVH.ApplicationKey,
		config.OVH.ApplicationSecret,
		config.OVH.ConsumerKey,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	currentIP, err := getPublicIP()
	if err != nil {
		os.Exit(1)
	}
	fmt.Printf("Current public IP: %s\n", currentIP)

	cachedIP := readCachedIP()
	if cachedIP == currentIP {
		fmt.Println("IP unchanged, nothing to do")
		os.Exit(0)
	}

	allOK := true
	for _, domainConfig := range config.Domains {
		zone := domainConfig.Zone
		subdomain := domainConfig.Subdomain
		ttl := domainConfig.TTL

		label := zone
		if subdomain != "" {
			label = subdomain + "." + zone
		}
		fmt.Printf("Updating %s...\n", label)

		if err := dns.updateDynamicDNS(zone, subdomain, ttl, currentIP); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to update %s\n", label)
			allOK = false
		}
	}

	if allOK {
		writeCachedIP(currentIP)
		fmt.Println("All DNS records updated successfully!")
		os.Exit(0)
	} else {
		fmt.Fprintln(os.Stderr, "Some updates failed, not caching IP (will retry next run)")
		os.Exit(1)
	}
}
