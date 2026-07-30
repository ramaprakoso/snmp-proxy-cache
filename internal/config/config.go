package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

// DeviceConfig defines single target device mapping.
type DeviceConfig struct {
	DeviceID    string `yaml:"device_id" json:"device_id"`
	Hostname    string `yaml:"hostname" json:"hostname"`
	IPAddress   string `yaml:"ip_address" json:"ip_address"`
	CustomPort  int    `yaml:"custom_port" json:"custom_port"`
	Community   string `yaml:"community" json:"community"`
	SNMPVersion string `yaml:"snmp_version" json:"snmp_version"`
	Profile     string `yaml:"profile" json:"profile"`
	Site        string `yaml:"site" json:"site"`
	Enabled     bool   `yaml:"enabled" json:"enabled"`
}

// Config represents full app configuration.
type Config struct {
	RedisURL      string               `json:"redis_url"`
	RedisPassword string               `json:"redis_password"`
	CacheTTLSec   int                  `json:"cache_ttl_sec"`
	ListenAddress string               `json:"listen_address"`
	Devices       []DeviceConfig       `json:"devices"`
	DeviceByPort  map[int]DeviceConfig `json:"-"`
}

// LoadConfig loads environment variables and devices.yaml.
func LoadConfig() (*Config, error) {
	redisURL := getEnv("REDIS_URL", "redis://localhost:6379")
	redisPass := getEnv("REDIS_PASSWORD", "snmpcache")
	listenAddr := getEnv("SNMP_LISTEN_ADDRESS", "0.0.0.0")

	cacheTTLStr := getEnv("CACHE_DEFAULT_FRESHNESS_SECONDS", "120")
	cacheTTL, err := strconv.Atoi(cacheTTLStr)
	if err != nil {
		cacheTTL = 120
	}

	devicesPath := getEnv("DEVICES_YAML_PATH", "")
	if devicesPath == "" {
		configDir := getEnv("CONFIG_DIR", "./config")
		// Priority 1: OPNsense exported file name: snmp_devices_cache.yaml
		devicesPath = filepath.Join(configDir, "snmp_devices_cache.yaml")
		if _, err := os.Stat(devicesPath); os.IsNotExist(err) {
			// Priority 2: Standard devices.yaml
			devicesPath = filepath.Join(configDir, "devices.yaml")
		}
	}

	cfg := &Config{
		RedisURL:      redisURL,
		RedisPassword: redisPass,
		CacheTTLSec:   cacheTTL,
		ListenAddress: listenAddr,
		DeviceByPort:  make(map[int]DeviceConfig),
	}

	devices, err := loadDevicesYAML(devicesPath)
	if err != nil {
		fmt.Printf("[CONFIG WARNING] Could not load %s (%v), using empty devices list\n", devicesPath, err)
	} else {
		cfg.Devices = devices
		for _, dev := range devices {
			if dev.Enabled {
				cfg.DeviceByPort[dev.CustomPort] = dev
			}
		}
	}

	return cfg, nil
}

func loadDevicesYAML(path string) ([]DeviceConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var wrapper struct {
		Devices []DeviceConfig `yaml:"devices"`
	}
	if err := yaml.Unmarshal(data, &wrapper); err != nil {
		return nil, err
	}

	return wrapper.Devices, nil
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
