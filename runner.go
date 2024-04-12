package gomtr

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var shutdown chan bool

// service
type MtrService struct {
	taskQueue     *SafeMap
	flag          int64
	index         int64
	in            io.WriteCloser
	out           io.ReadCloser
	outChan       chan string
	mtrPacketPath string
	startAt       string
	timeout       time.Duration
	uid           uint32 // user id to run mtr-packet under
	gid           uint32 // group id to run mtr-packet under
	sync.RWMutex
}

// NewMtrService new a mtr service
// path - mtr-packet executable path
func NewMtrService(path string, timeout time.Duration, uid int, gid int) *MtrService {
	return &MtrService{
		taskQueue:     New(),
		flag:          102400,
		index:         0,
		in:            nil,
		out:           nil,
		outChan:       make(chan string, 1000),
		mtrPacketPath: path,
		timeout:       timeout,
		uid:           uint32(uid),
		gid:           uint32(gid),
	}
}

// Start start service and wait mtr-packet stdio
func (ms *MtrService) Start() {
	shutdown = make(chan bool)
	go ms.startup()
	time.Sleep(time.Second)
}

func (ms *MtrService) startup() {
	ms.Lock()
	cmd := exec.Command(ms.mtrPacketPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	cmd.SysProcAttr.Credential = &syscall.Credential{Uid: ms.uid, Gid: ms.gid}

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
			select {
			case <-shutdown:
				return
			default:
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
		}
	}()

	// get result from chan and parse
	go func() {
		for {
			select {
			case <-shutdown:
				return
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
			select {
			case <-shutdown:
				return
			default:
				var readBytes []byte = make([]byte, 100)
				err.Read(readBytes)
				time.Sleep(time.Second)
			}
		}
	}()

	ms.startAt = fmt.Sprintf("Start: %s", getMtrStartTime())

	ms.Unlock()

	// wait sub process
	if e := cmd.Wait(); nil != e {
		//fmt.Printf("ERROR: %v\n", e)
		return
	}

}

// Request send a task request
// ip       - the test ip
// c        - repeat time, such as mtr tool argument c
// callback - just callback after task ready
func (ms *MtrService) Request(ip string, c int, maxHops int, callback func(interface{})) {
	ms.Lock()
	ms.index++

	taskID := ms.index
	writer := ms.in

	if ms.index > ms.flag {
		ms.index = 1
	}

	if c <= 0 {
		c = 1
	}

	task := &MtrTask{
		id:       taskID,
		callback: callback,
		c:        c,
		ttlData:  New(),
		target:   ip,
		timeout:  ms.timeout,
	}

	ms.taskQueue.Put(fmt.Sprintf("%d", taskID), task)

	ms.Unlock()

	task.send(&writer, taskID, ip, c, maxHops)
}

func (ms *MtrService) ClearQueue() {
	ms.taskQueue.Clear()
}

func (ms *MtrService) Close() {
	close(shutdown)
}

func (ms *MtrService) GetServiceStartupTime() string {
	ms.RLock()
	defer ms.RUnlock()
	return ms.startAt
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

	if len(segments) <= 0 {
		return
	}

	if len(segments) > 0 {
		idInt, err := strconv.Atoi(segments[0])
		if err != nil {
			idInt = 0
		}
		fullID = int64(idInt)
	}

	if len(segments) > 1 {
		switch segments[1] {
		case "command-parse-error", "no-reply", "probes-exhausted", "network-down", "permission-denied", "no-route", "invalid-argument", "feature-support":
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

	if len(segments) > 5 {
		ttlTimeInt, err := strconv.Atoi(segments[5])
		if err != nil {
			ttlTimeInt = 0
		} else {
			ttlTime = int64(ttlTimeInt)
		}
	}

	ttlData = &TTLData{
		TTLID:        getTTLID(fullID),
		ipType:       ipType,
		ip:           ip,
		err:          ttlerr,
		status:       status,
		raw:          data,
		time:         ttlTime,
		receivedTime: time.Now(),
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
