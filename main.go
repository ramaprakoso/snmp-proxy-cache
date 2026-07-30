package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gosnmp/gosnmp"
)

// CapturedPacket represents the structured packet data logged to stdout.
type CapturedPacket struct {
	Timestamp      string   `json:"timestamp"`
	SrcAddress     string   `json:"src_address"`
	ListenPort     int      `json:"listen_port"`
	SNMPVersion    string   `json:"snmp_version"`
	Community      string   `json:"community"`
	PDUType        string   `json:"pdu_type"`
	RequestID      uint32   `json:"request_id"`
	OIDs           []string `json:"oids"`
	NonRepeaters   uint32   `json:"non_repeaters,omitempty"`
	MaxRepetitions uint32   `json:"max_repetitions,omitempty"`
	RawBytesCount  int      `json:"raw_bytes_count"`
}

func main() {
	log.Println("==================================================================")
	log.Println("     SNMP UDP Capture & Print Tool (OPNsense / NMS Analyzer)      ")
	log.Println("==================================================================")

	portsEnv := os.Getenv("LISTEN_PORTS")
	if portsEnv == "" {
		portsEnv = "21001,21002,21003,21004,21005"
	}

	portsStr := strings.Split(portsEnv, ",")
	for _, pStr := range portsStr {
		port, err := strconv.Atoi(strings.TrimSpace(pStr))
		if err != nil {
			log.Printf("[WARNING] Invalid port %s, skipping: %v\n", pStr, err)
			continue
		}
		go listenUDPPort(port)
	}

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)
	log.Println("[READY] Sniffer is listening for incoming UDP packets from OPNsense/NMS...")
	<-stopChan
	log.Println("[INFO] Sniffer shutting down.")
}

func listenUDPPort(port int) {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		log.Printf("[ERROR] Failed to listen on UDP %s: %v", addr, err)
		return
	}
	defer conn.Close()

	log.Printf("[LISTENING] Listening on UDP %s\n", addr)
	buf := make([]byte, 65535)

	for {
		n, srcAddr, err := conn.ReadFrom(buf)
		if err != nil {
			log.Printf("[ERROR] Read error on port %d: %v\n", port, err)
			continue
		}

		packetBytes := make([]byte, n)
		copy(packetBytes, buf[:n])

		go parseAndPrintPacket(srcAddr.String(), port, packetBytes)
	}
}

func parseAndPrintPacket(srcAddr string, listenPort int, data []byte) {
	packet, err := decodeSNMPPacket(data)

	cap := CapturedPacket{
		Timestamp:     time.Now().UTC().Format(time.RFC3339Nano),
		SrcAddress:    srcAddr,
		ListenPort:    listenPort,
		RawBytesCount: len(data),
	}

	if err != nil {
		cap.PDUType = "RAW_DECODE_ERROR"
		logCapturedPacket(cap, fmt.Sprintf("SNMP Decode Error: %v", err))
		return
	}

	cap.SNMPVersion = packet.Version.String()
	cap.Community = packet.Community
	cap.PDUType = packet.PDUType.String()
	cap.RequestID = packet.RequestID
	cap.NonRepeaters = uint32(packet.NonRepeaters)
	cap.MaxRepetitions = uint32(packet.MaxRepetitions)

	for _, variable := range packet.Variables {
		cap.OIDs = append(cap.OIDs, variable.Name)
	}

	logCapturedPacket(cap, "")
}

func decodeSNMPPacket(data []byte) (*gosnmp.SnmpPacket, error) {
	g := &gosnmp.GoSNMP{
		Target:  "127.0.0.1",
		Port:    161,
		Version: gosnmp.Version2c,
		Timeout: 2 * time.Second,
		Retries: 1,
	}

	return g.SnmpDecodePacket(data)
}

func logCapturedPacket(cap CapturedPacket, extraMsg string) {
	jsonBytes, err := json.MarshalIndent(cap, "", "  ")
	if err != nil {
		log.Printf("[ERROR] Failed to format JSON log: %v\n", err)
		return
	}

	fmt.Println("==================================================================")
	fmt.Printf(" [SNMP PACKET CAPTURED] Port: %d | From: %s | PDU: %s\n", cap.ListenPort, cap.SrcAddress, cap.PDUType)
	fmt.Println("==================================================================")
	fmt.Println(string(jsonBytes))
	if extraMsg != "" {
		fmt.Printf("[NOTE] %s\n", extraMsg)
	}
	fmt.Println("------------------------------------------------------------------\n")
}
