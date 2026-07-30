package proxy

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
	"golang.org/x/sync/singleflight"

	"snmp-proxy-cache/internal/cache"
	"snmp-proxy-cache/internal/config"
	"snmp-proxy-cache/internal/snmp"
)

// LazyHandler handles incoming SNMP PDUs with Redis Lazy Caching and Singleflight deduplication.
type LazyHandler struct {
	cfg          *config.Config
	redis        *cache.RedisStore
	requestGroup singleflight.Group
}

// NewLazyHandler initializes LazyHandler.
func NewLazyHandler(cfg *config.Config, redisStore *cache.RedisStore) *LazyHandler {
	return &LazyHandler{
		cfg:   cfg,
		redis: redisStore,
	}
}

// ProcessPacket processes an incoming raw SNMP PDU from OPNsense.
func (h *LazyHandler) ProcessPacket(device config.DeviceConfig, packet *gosnmp.SnmpPacket) (*gosnmp.SnmpPacket, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	// 1. Canonicalize requested OIDs & Update OID Registry (Pattern Shift Detection)
	reqOIDs := make([]string, len(packet.Variables))
	for i, v := range packet.Variables {
		reqOIDs[i] = v.Name
	}

	// Fetch current union registry for this device
	registeredOIDs, _ := h.redis.GetOIDRegistry(ctx, device.DeviceID)
	regMap := make(map[string]bool)
	for _, ro := range registeredOIDs {
		regMap[ro] = true
	}

	var newOIDs []string
	for _, roid := range reqOIDs {
		if !regMap[roid] {
			newOIDs = append(newOIDs, roid)
		}
	}

	// Pattern Shift: if new OIDs detected, add them to Registry Memory
	if len(newOIDs) > 0 {
		log.Printf("[PATTERN SHIFT DETECTED] Device %s: New OID(s) %v requested. Updating OID Registry Memory...", device.DeviceID, newOIDs)
		_ = h.redis.AddToOIDRegistry(ctx, device.DeviceID, newOIDs...)
	}

	// 2. Check Redis Cache for requested OIDs
	responseVars := make([]gosnmp.SnmpPDU, len(packet.Variables))
	hasMiss := false

	for i, v := range packet.Variables {
		entry, found := h.redis.GetCachedVarBind(ctx, device.DeviceID, v.Name)
		if found {
			// Cache HIT
			asnType := parseAsn1Type(entry.DataType)
			responseVars[i] = gosnmp.SnmpPDU{
				Name:  v.Name,
				Type:  asnType,
				Value: parseAsn1Value(entry.Value, asnType),
			}
		} else {
			// Cache MISS
			hasMiss = true
		}
	}

	// 3. If any OID missed, execute Upstream Fetch using 100% UNION OID LIST from Registry (Prefetch All)
	if hasMiss {
		// Get full Union Registry OID list (Zabbix + Cacti + Observium combined)
		unionOIDs, err := h.redis.GetOIDRegistry(ctx, device.DeviceID)
		if err != nil || len(unionOIDs) == 0 {
			unionOIDs = reqOIDs
		}

		sfKey := fmt.Sprintf("poll:%s:%v", device.DeviceID, unionOIDs)
		resChan := h.requestGroup.DoChan(sfKey, func() (interface{}, error) {
			log.Printf("[CACHE MISS / REFRESH] Device %s: Prefetching 100%% Union OID List (%d OIDs)...", device.DeviceID, len(unionOIDs))
			upstream := snmp.NewUpstreamClient(device)
			return upstream.PollOIDs(packet.PDUType, unionOIDs)
		})

		res := <-resChan
		if res.Err != nil {
			log.Printf("[ERROR] Singleflight upstream fetch failed for %s: %v", device.DeviceID, res.Err)
			return nil, res.Err
		}

		fetchedPDUs, ok := res.Val.([]gosnmp.SnmpPDU)
		if !ok {
			return nil, fmt.Errorf("unexpected PDU result type")
		}

		// Save ALL 100% Union fetched results into Redis Cache
		fetchedMap := make(map[string]gosnmp.SnmpPDU)
		for _, pdu := range fetchedPDUs {
			// Skip saving invalid/tree-prefix error types into Redis
			if pdu.Type == gosnmp.NoSuchObject || pdu.Type == gosnmp.NoSuchInstance || pdu.Type == gosnmp.Null {
				log.Printf("[SKIP CACHE] OID %s returned invalid type %s, skipping Redis save.", pdu.Name, pdu.Type.String())
				continue
			}

			valStr := formatPDUValue(pdu.Value)
			dataTypeStr := pdu.Type.String()
			_ = h.redis.SetCachedVarBind(ctx, device.DeviceID, pdu.Name, valStr, dataTypeStr)
			fetchedMap[pdu.Name] = pdu
		}

		// Populate response for the specific requested OIDs of this client
		for i, v := range packet.Variables {
			varName := ensureLeadingDot(v.Name)
			if pdu, found := fetchedMap[v.Name]; found {
				pdu.Name = ensureLeadingDot(pdu.Name)
				responseVars[i] = pdu
			} else if pdu, found := fetchedMap[varName]; found {
				pdu.Name = ensureLeadingDot(pdu.Name)
				responseVars[i] = pdu
			} else {
				responseVars[i] = gosnmp.SnmpPDU{
					Name:  varName,
					Type:  gosnmp.NoSuchObject,
					Value: nil,
				}
			}
		}
	}

	// Ensure all response OIDs have leading dot formatting
	for i, pdu := range responseVars {
		if pdu.Name == "" && i < len(packet.Variables) {
			responseVars[i].Name = ensureLeadingDot(packet.Variables[i].Name)
			responseVars[i].Type = gosnmp.NoSuchObject
		} else {
			responseVars[i].Name = ensureLeadingDot(pdu.Name)
		}
	}

	// Build SNMP Response Packet
	respPacket := &gosnmp.SnmpPacket{
		Version:        packet.Version,
		Community:      packet.Community,
		PDUType:        gosnmp.GetResponse,
		RequestID:      packet.RequestID,
		Error:          gosnmp.NoError,
		ErrorIndex:     0,
		Variables:      responseVars,
		NonRepeaters:   packet.NonRepeaters,
		MaxRepetitions: packet.MaxRepetitions,
	}

	return respPacket, nil
}

func ensureLeadingDot(oid string) string {
	if oid == "" {
		return "."
	}
	if !strings.HasPrefix(oid, ".") {
		return "." + oid
	}
	return oid
}

func formatPDUValue(val interface{}) string {
	switch v := val.(type) {
	case []byte:
		return string(v)
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

func parseAsn1Value(valStr string, asnType gosnmp.Asn1BER) interface{} {
	switch asnType {
	case gosnmp.OctetString:
		return []byte(valStr)
	case gosnmp.Integer:
		var n int64
		if _, err := fmt.Sscanf(valStr, "%d", &n); err == nil {
			return int(n)
		}
		return valStr
	case gosnmp.Counter32, gosnmp.Gauge32, gosnmp.TimeTicks:
		var n uint64
		if _, err := fmt.Sscanf(valStr, "%d", &n); err == nil {
			return uint32(n)
		}
		return valStr
	case gosnmp.Counter64:
		var n uint64
		if _, err := fmt.Sscanf(valStr, "%d", &n); err == nil {
			return uint64(n)
		}
		return valStr
	default:
		return valStr
	}
}

func parseAsn1Type(typeStr string) gosnmp.Asn1BER {
	switch typeStr {
	case "Integer", "Integer32":
		return gosnmp.Integer
	case "Counter32":
		return gosnmp.Counter32
	case "Counter64":
		return gosnmp.Counter64
	case "Gauge32":
		return gosnmp.Gauge32
	case "TimeTicks":
		return gosnmp.TimeTicks
	case "OctetString":
		return gosnmp.OctetString
	default:
		return gosnmp.OctetString
	}
}
