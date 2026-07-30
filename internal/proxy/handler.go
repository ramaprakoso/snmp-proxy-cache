package proxy

import (
	"context"
	"fmt"
	"log"
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

	responseVars := make([]gosnmp.SnmpPDU, len(packet.Variables))
	missedOIDs := make([]string, 0)
	missedIndexes := make([]int, 0)

	// 1. Check Redis Cache for all requested OIDs (Cache Hit / Miss check)
	for i, v := range packet.Variables {
		entry, found := h.redis.GetCachedVarBind(ctx, device.DeviceID, v.Name)
		if found {
			// Cache HIT
			responseVars[i] = gosnmp.SnmpPDU{
				Name:  v.Name,
				Type:  parseAsn1Type(entry.DataType),
				Value: entry.Value,
			}
		} else {
			// Cache MISS
			missedOIDs = append(missedOIDs, v.Name)
			missedIndexes = append(missedIndexes, i)
		}
	}

	// 2. If any OID missed, execute upstream fetch via Singleflight (preventing duplicate polls)
	if len(missedOIDs) > 0 {
		sfKey := fmt.Sprintf("%s:%v", device.DeviceID, missedOIDs)
		resChan := h.requestGroup.DoChan(sfKey, func() (interface{}, error) {
			upstream := snmp.NewUpstreamClient(device)
			return upstream.PollOIDs(missedOIDs)
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

		// Save fetched results to Redis Cache and populate response
		for idx, pdu := range fetchedPDUs {
			valStr := fmt.Sprintf("%v", pdu.Value)
			dataTypeStr := pdu.Type.String()

			_ = h.redis.SetCachedVarBind(ctx, device.DeviceID, pdu.Name, valStr, dataTypeStr)

			if idx < len(missedIndexes) {
				origIndex := missedIndexes[idx]
				responseVars[origIndex] = pdu
			}
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
