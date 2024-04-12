package main

import (
	"fmt"
	"time"

	"github.com/ethanmurphy1/gomtr"
	"github.com/gogather/com/log"
)

func main() {
	mtr := gomtr.NewMtrService("/opt/services/sbnetwork-monitor/mtr-packet", time.Second*60, 1000, 1000)
	go mtr.Start()

	settings := gomtr.MTRSettings{
		MaxHops:    50,
		Protocol:   "icmp",
		TTLTimeout: 2,
		PacketSize: 64,
	}

	time.Sleep(time.Second * 5)

	fmt.Println(mtr.GetServiceStartupTime())

	iplist := []string{"4.4.4.4", "127.0.0.1", "8.8.8.8", "127.0.0.1"}

	for {
		for i := 0; i < len(iplist); i++ {
			id := i
			go mtr.Request(iplist[i], 10, settings, func(response interface{}) {
				task := response.(*gomtr.MtrTask)
				log.Bluef("[ID] %d cost: %d ms\n", id, task.CostTime/1000000)
				fmt.Println(task.GetSummaryDecorateString())
			})
		}
		mtr.ClearQueue()
		time.Sleep(time.Second * 5)
	}
}
