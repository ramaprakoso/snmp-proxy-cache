package proxy

import (
	"fmt"
	"log"
	"net"

	"github.com/gosnmp/gosnmp"
	"snmp-proxy-cache/internal/config"
)

// Server manages native Go UDP SNMP Proxy listeners.
type Server struct {
	cfg     *config.Config
	handler *LazyHandler
}

// NewServer initializes Server.
func NewServer(cfg *config.Config, handler *LazyHandler) *Server {
	return &Server{
		cfg:     cfg,
		handler: handler,
	}
}

// StartListeners launches UDP listeners for all enabled devices.
func (s *Server) StartListeners() {
	for port, device := range s.cfg.DeviceByPort {
		go s.listenPort(port, device)
	}
}

func (s *Server) listenPort(port int, device config.DeviceConfig) {
	addr := fmt.Sprintf("%s:%d", s.cfg.ListenAddress, port)
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		log.Printf("[ERROR] Failed to listen on UDP %s (%s): %v", addr, device.Hostname, err)
		return
	}
	defer conn.Close()

	log.Printf("[PROXY STARTED] Listening on UDP %s -> Target Device %s (%s)", addr, device.DeviceID, device.IPAddress)

	buf := make([]byte, 65535)

	for {
		n, srcAddr, err := conn.ReadFrom(buf)
		if err != nil {
			log.Printf("[ERROR] Read error on port %d: %v", port, err)
			continue
		}

		packetBytes := make([]byte, n)
		copy(packetBytes, buf[:n])

		go func(raw []byte, replyAddr net.Addr) {
			// Decode request
			g := &gosnmp.GoSNMP{Version: gosnmp.Version2c}
			reqPacket, err := g.SnmpDecodePacket(raw)
			if err != nil {
				log.Printf("[WARNING] SNMP Decode Error on port %d from %s: %v", port, replyAddr, err)
				return
			}

			// Process via Lazy Handler
			respPacket, err := s.handler.ProcessPacket(device, reqPacket)
			if err != nil {
				log.Printf("[ERROR] Handler error on port %d: %v", port, err)
				return
			}

			// Encode response
			outBytes, err := respPacket.MarshalMsg()
			if err != nil {
				log.Printf("[ERROR] SNMP Marshal Error on port %d: %v", port, err)
				return
			}

			// Send SNMP response PDU back to OPNsense / caller
			_, _ = conn.WriteTo(outBytes, replyAddr)
		}(packetBytes, srcAddr)
	}
}
