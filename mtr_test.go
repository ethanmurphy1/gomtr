package gomtr

import (
	"fmt"
	"testing"
	"time"

	"github.com/gogather/com/log"
)

func Test_Mtr(t *testing.T) {
	mtr := NewMtrService("./mtr-packet", time.Second*60, 1000, 1000)
	mtrAlive := make(chan bool)
	go mtr.Start(mtrAlive)

	settings := MTRSettings{
		MaxHops:    50,
		Protocol:   "icmp",
		TTLTimeout: 2,
		PacketSize: 64,
	}

	time.Sleep(time.Second * 10)

	fmt.Println(mtr.GetServiceStartupTime())

	iplist := []string{"4.4.4.4", "183.131.7.130", "127.0.0.1", "114.215.151.25", "111.13.101.208"}

	for i := 0; i < len(iplist); i++ {
		go mtr.Request(iplist[i], 10, settings, func(response interface{}) {
			task := response.(*MtrTask)
			log.Bluef("[ID] %d cost: %d ms\n", task.id, task.CostTime/1000000)
			fmt.Println(task.GetSummaryDecorateString())
		})
	}

	for {
		time.Sleep(time.Second)
	}
}
