package gomtr

import (
	"bufio"
	"errors"
	"fmt"
	"github.com/gogather/com"
	"github.com/gogather/safemap"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const maxttls = 50

// service
type MtrService struct {
	taskQueue *safemap.SafeMap
	flag      int64
	index     int64
	in        io.WriteCloser
	out       io.ReadCloser
	outChan   chan string
}

func NewMtrService() *MtrService {
	return &MtrService{
		taskQueue: safemap.New(),
		flag:      102400,
		index:     1,
		in:        nil,
		out:       nil,
		outChan:   make(chan string, 1000),
	}
}

// start service and wait mtr-packet stdio
func (ms *MtrService) Start() {
	go ms.startup()
	time.Sleep(time.Second)
}

func (ms *MtrService) startup() {

	cmd := exec.Command("./mtr-packet")

	var e error

	ms.out, e = cmd.StdoutPipe()
	if e != nil {
		fmt.Println(e)
	}

	ms.in, e = cmd.StdinPipe()
	if e != nil {
		fmt.Println(e)
	}

	err, e := cmd.StderrPipe()
	if e != nil {
		fmt.Println(e)
	}

	// start sub process
	if e := cmd.Start(); nil != e {
		fmt.Printf("ERROR: %v\n", e)
	}

	// read data and put into result chan
	go func() {
		for {
			// read lines
			bio := bufio.NewReader(ms.out)
			for {
				output, isPrefix, err := bio.ReadLine()
				if err != nil {
					break
				}

				if string(output) != "" {
					ms.outChan <- string(output)
				}

				if isPrefix {
					break
				}
			}

		}
	}()

	// get result from chan and parse
	go func() {
		for {
			select {
			case result := <-ms.outChan:
				{
					ms.parseTTLData(result)
				}
			}

		}
	}()

	// error output
	go func() {
		for {
			var readBytes []byte = make([]byte, 100)
			err.Read(readBytes)
			time.Sleep(1)
		}
	}()

	// wait sub process
	if e := cmd.Wait(); nil != e {
		fmt.Printf("ERROR: %v\n", e)
	}

}

func (ms *MtrService) Request(ip string, c int, callback func(interface{})) {

	task := &MtrTask{
		id:       ms.index,
		callback: callback,
		c:        c,
		ttlData:  safemap.New(),
	}

	ms.taskQueue.Put(fmt.Sprintf("%d", ms.index), task)

	task.send(ms.in, ms.index, ip, c)

	ms.index++

	if ms.index > ms.flag {
		ms.index = 1
	}

}

func (ms *MtrService) parseTTLData(data string) {
	segs := strings.Split(data, "\n")

	for i := 0; i < len(segs); i++ {
		item := strings.TrimSpace(segs[i])
		if len(item) > 0 {
			ms.parseTTLDatum(item)
		}
	}
}

func (ms *MtrService) parseTTLDatum(data string) {

	hasNewline := strings.Contains(data, "\n")
	if hasNewline {
		fmt.Println(hasNewline)
	}

	segments := strings.Split(data, " ")

	var ttlData *TTLData
	var fullID int64
	var ttlTime int64
	var ttlerr error
	var status string
	var ipType string
	var ip string

	if len(segments) <= 1 {
		return
	}

	if len(segments) > 1 {
		idInt, err := strconv.Atoi(segments[0])
		if err != nil {
			idInt = 0
		}
		fullID = int64(idInt)
	}

	if len(segments) > 2 {

		switch segments[1] {
		case "command-parse-error":
		case "no-reply":
		case "probes-exhausted":
		case "network-down":
		case "permission-denied":
		case "no-route":
		case "invalid-argument":
		case "feature-support":
			{
				ttlerr = errors.New(segments[1])
				break
			}
		case "ttl-expired":
			{
				status = segments[1]
				break
			}
		case "reply":
			{
				status = segments[1]
				break
			}
		}

	}

	if len(segments) > 2 {
		ipType = segments[2]
	}

	if len(segments) > 3 {
		ip = segments[3]
	}

	if len(segments) >= 6 {
		ttlTimeInt, err := strconv.Atoi(segments[5])
		if err != nil {
			ttlTimeInt = 0
		} else {
			ttlTime = int64(ttlTimeInt)
		}
	}

	ttlData = &TTLData{
		TTLID:  getTTLID(fullID),
		ipType: ipType,
		ip:     ip,
		err:    ttlerr,
		status: status,
		raw:    data,
		time:   ttlTime,
	}

	// store
	taskID := fmt.Sprintf("%d", getRealID(fullID))
	taskRaw, ok := ms.taskQueue.Get(taskID)
	var task *MtrTask = nil
	if ok && taskRaw != nil {
		task = taskRaw.(*MtrTask)
		ttlID := getTTLID(fullID)
		task.save(ttlID, ttlData)
	} else {
		return
	}

}

func getTTLID(fullID int64) int {
	idStr := fmt.Sprintf("%d", fullID)
	length := len(idStr)
	ttlStr := com.SubString(idStr, length-4, 4)
	ttl, e := strconv.Atoi(ttlStr)
	if e != nil {
		ttl = 0
	}
	return ttl
}

func getRealID(fullID int64) int64 {
	return fullID / 10000
}
