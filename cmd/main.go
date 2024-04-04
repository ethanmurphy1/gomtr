package main

import (
	"fmt"
	"time"

	"github.com/ethanmurphy1/gomtr"
	"github.com/gogather/com/log"
)

func main() {
	mtr := gomtr.NewMtrService("/opt/services/sbnetwork-monitor/mtr-packet", time.Second*60)
	go mtr.Start()

	time.Sleep(time.Second * 5)

	fmt.Println(mtr.GetServiceStartupTime())

	iplist := []string{"4.4.4.4", "127.0.0.1", "8.8.8.8", "127.0.0.1"}

	for {
		for i := 0; i < len(iplist); i++ {
			id := i
			go mtr.Request(iplist[i], 10, 50, func(response interface{}) {
				task := response.(*gomtr.MtrTask)
				log.Bluef("[ID] %d cost: %d ms\n", id, task.CostTime/1000000)
				fmt.Println(task.GetSummaryDecorateString())
			})
		}
		mtr.ClearQueue()
		time.Sleep(time.Second * 5)
	}
}
