package gomtr

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gogather/com"
)

// parsed ttl item data
type TTLData struct {
	TTLID        int
	status       string
	ipType       string
	ip           string
	time         int64
	raw          string
	err          error
	receivedTime time.Time
}

// task
type MtrTask struct {
	id              int64
	callback        func(interface{})
	c               int
	target          string
	ttlData         *SafeMap // item is ttlData, key is ttl
	SendTime        time.Time
	CostTime        int64
	timeout         time.Duration
	probeTimeoutMs  int
	packetSizeBytes int
	maxHops         int
	protocol        string
	TotalPackets    int
}

func (mt *MtrTask) save(ttl int, data *TTLData) {
	mt.ttlData.Put(fmt.Sprintf("%d", ttl), data)
}

func (mt *MtrTask) send(in *io.WriteCloser, id int64, ip string, c int) {
	defer func() {
		recover()
	}()

	maxHops := mt.maxHops

	if c > 100 {
		c = 99
	} else if c < 1 {
		c = 1
	}

	writer := *in

	mt.SendTime = time.Now()

	lastReply := 0
	didComplete := false
	probes := 0
	for i := 1; i <= c; i++ {
		if time.Since(mt.SendTime) > mt.timeout {
			mt.c = i - 1
			break
		}
		sendId := id*10000 + int64(i)*100
		var prevRid int64 = 0
		var idx = 1
		if i > 1 {
			if !didComplete {
				// Test did not complete within maxHops. Reduce TTL length to last responsive hop
				maxHops = lastReply
			}
		}
		lastReply = 0
		didComplete = false
		for idx = 1; idx <= maxHops; idx++ {
			// get reality id
			rid := sendId + int64(idx)

			// the 1st one does not check
			if idx > 1 {
				// sync check status, it will block until ready, and return status
				// ready:
				//       0  reached destination
				//       1  host is alive, but not destination. continue
				//		 2  no reply, host is dead
				check := mt.checkLoop(prevRid)
				if check == 0 {
					didComplete = true
					lastReply = idx
					break
				}
				if check == 1 {
					lastReply = idx
				}
			}

			prevRid = rid

			command := fmt.Sprintf(
				"%d send-probe ip-4 %s ttl %d size %d protocol %s timeout %d\n",
				rid,
				ip,
				idx,
				mt.packetSizeBytes,
				mt.protocol,
				int(math.Ceil(float64(mt.probeTimeoutMs)/1000.0))) // Round timeout in ms up to next full second

			writer.Write([]byte(command))

			time.Sleep(time.Millisecond * time.Duration(mt.probeTimeoutMs))
		}
		probes += idx
	}

	time.Sleep(time.Millisecond * 500)

	mt.CostTime = time.Now().UnixNano() - mt.SendTime.UnixNano()
	mt.TotalPackets = probes

	// callback
	mt.callback(mt)
	mt.clear()

}

// check latest ttl is replied
//
//	0  [    returned]  ready and get replied
//	1  [    returned]  ready but not replied, go on
//	2  [    returned]  no reply
//
// [-1]  [not returned]  not ready, should block
func (mt *MtrTask) checkLoop(rid int64) int {
	// get ttlID
	ttlID := getTTLID(rid)

	for {
		// check ready
		d, ok := mt.ttlData.Get(fmt.Sprintf("%d", ttlID))
		if !ok || d == nil {
			// not ready, continue
			break
		} else {
			data, ok := d.(*TTLData)
			if !ok || data == nil {
				// not ready, continue
				break
			} else {
				// ready, check replied
				if data.status == "reply" {
					// get replied
					return 0
				} else if strings.Contains(data.raw, "no-reply") {
					return 2
				} else if data.status == "ttl-expired" || data.err != nil {
					// not get replied
					return 1
				}
			}
		}
	}

	mt.save(ttlID, &TTLData{err: errors.New("no-reply"), TTLID: ttlID})
	return 2
}

func (mt *MtrTask) clear() {
	mt.ttlData.Lock()
	mt.ttlData.M = make(map[string]interface{})
	mt.ttlData.Unlock()
}

func (mt *MtrTask) GetResultMap() map[int]map[int]int64 {
	results := map[int]map[int]int64{}
	mt.ttlData.RLock()
	defer mt.ttlData.RUnlock()
	for key := range mt.ttlData.GetMap() {
		item, ok := mt.ttlData.Get(key)
		if ok {
			itemData, ok := item.(*TTLData)
			if ok && itemData != nil {
				ttlid := itemData.TTLID
				cid := ttlid / 100
				ttl := ttlid % 100
				_, ok := results[ttl]
				if !ok {
					results[ttl] = map[int]int64{}
				}
				if itemData.err != nil {
					results[ttl][cid] = -1
				} else {
					results[ttl][cid] = itemData.time
				}
			}
		}
	}
	return results
}

func (mt *MtrTask) GetSummary() map[int]map[string]string {
	results := map[int][]*TTLData{}

	var keys []int
	mt.ttlData.RLock()
	for ks := range mt.ttlData.GetMap() {
		k, _ := strconv.Atoi(ks)
		keys = append(keys, k)
	}
	mt.ttlData.RUnlock()
	sort.Ints(keys)

	for _, key := range keys {
		item, ok := mt.ttlData.Get(fmt.Sprintf("%d", key))
		if ok {
			itemData, ok := item.(*TTLData)
			if ok && itemData != nil {
				ttlid := itemData.TTLID
				ttl := ttlid % 100

				// put data
				array, ok := results[ttl]
				if ok {
					results[ttl] = append(array, itemData)
				} else {
					results[ttl] = []*TTLData{itemData}
				}
			}
		}
	}

	// get last ttl key
	var ttlKeys []int
	for k := range results {
		ttlKeys = append(ttlKeys, k)
	}
	sort.Ints(ttlKeys)
	lastTTLKey := 0
	if len(ttlKeys) > 0 {
		lastTTLKey = ttlKeys[len(ttlKeys)-1]
	}

	summarys := map[int]map[string]string{}
	for i := 1; i <= lastTTLKey; i++ {
		// summary
		value, ok := results[i]
		if ok {
			summarys[i] = map[string]string{
				"Loss":  fmt.Sprintf("%.01f%%", (1-float32(sortSntReality(value))/float32(mt.c))*100),
				"Last":  fmtNumber(sortLast(value)),
				"Avg":   fmtNumber(sortAvg(value)),
				"Best":  fmtNumber(sortBest(value)),
				"Wrst":  fmtNumber(sortWorst(value)),
				"StDev": fmtNumber(sortSTDev(value)),
				"Snt":   fmt.Sprintf("%d", mt.c),
				"IP":    sortIP(value),
				"ttl":   fmt.Sprintf("%d", i),
				"raw":   sortLastTTLData(value).raw,
			}
		} else {
			summarys[i] = map[string]string{
				"Loss":  "100%",
				"Last":  "0.0",
				"Avg":   "0.0",
				"Best":  "0.0",
				"Wrst":  "0.0",
				"StDev": "0.0",
				"Snt":   fmt.Sprintf("%d", mt.c),
				"IP":    "???",
				"ttl":   fmt.Sprintf("%d", i),
				"raw":   "",
			}
		}

		if sortHasReply(value) {
			break
		}

	}

	clearSummary(summarys)

	return summarys
}

func (mt *MtrTask) GetSummaryString() string {
	data := mt.GetSummary()

	var keys []int
	for k := range data {
		keys = append(keys, k)
	}
	sort.Ints(keys)

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "127.0.0.1"
	}

	summary := fmt.Sprintf("%-28s %9s %4s %6s %6s %6s %6s %6s\n", "HOST: "+com.SubString(hostname, 0, 20), "Loss%", "Snt", "Last", "Avg", "Best", "Wrst", "StDev")

	for _, key := range keys {
		item := data[key]
		summary = summary + fmt.Sprintf("%3s.|-- %-20s %9s %4s %6s %6s %6s %6s %6s\n", item["ttl"], item["IP"], item["Loss"], item["Snt"], item["Last"], item["Avg"], item["Best"], item["Wrst"], item["StDev"])
	}

	return summary
}

func (mt *MtrTask) GetSummaryDecorateString() string {
	table := mt.GetSummaryString()

	summary := fmt.Sprintf("[ID] %d [target] %s\n", mt.id, mt.target) + table + "\n"
	return summary
}
