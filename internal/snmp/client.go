package snmp

import (
	"fmt"
	"time"

	"github.com/gosnmp/gosnmp"
	"snmp-proxy-cache/internal/config"
)

// UpstreamClient wraps GoSNMP for querying physical target network devices.
type UpstreamClient struct {
	targetIP  string
	community string
	timeout   time.Duration
	retries   int
}

// NewUpstreamClient creates a client targeting a device IP.
func NewUpstreamClient(device config.DeviceConfig) *UpstreamClient {
	comm := device.Community
	if comm == "" {
		comm = "public"
	}
	return &UpstreamClient{
		targetIP:  device.IPAddress,
		community: comm,
		timeout:   5 * time.Second,
		retries:   2,
	}
}

// PollOIDs dispatches a Get SNMP request for the provided OIDs.
func (uc *UpstreamClient) PollOIDs(oids []string) ([]gosnmp.SnmpPDU, error) {
	params := &gosnmp.GoSNMP{
		Target:    uc.targetIP,
		Port:      161,
		Community: uc.community,
		Version:   gosnmp.Version2c,
		Timeout:   uc.timeout,
		Retries:   uc.retries,
	}

	if err := params.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to upstream %s: %w", uc.targetIP, err)
	}
	defer params.Conn.Close()

	result, err := params.Get(oids)
	if err != nil {
		return nil, fmt.Errorf("upstream SNMP GET failed (%s): %w", uc.targetIP, err)
	}

	return result.Variables, nil
}
