package main

import (
	"math/rand"
	"time"

	"github.com/danmuck/dps_files/cmd/internal/logcfg"
	"github.com/danmuck/dps_files/src/api/nodes"
	"github.com/danmuck/dps_files/src/api/transport"
	logs "github.com/danmuck/smplog"
)

func handleInbound(h transport.TCPHandler) {
	for {
		rpc := <-h.ProcessRPC()
		// ch <- rpc
		if rpc != nil {
			logs.Debugf("handleInbound(%s)", rpc.Sender.Address)
			continue
		}
		logs.Debugf("handleInbound(): exiting")
		break
	}
}

func GenerateKey() []byte {
	i := 20
	b := make([]byte, 0, i)
	for i > 0 {
		i--
		r := rand.Intn(256)
		b = append(b, byte(r))
	}

	return b
}
func main() {
	logs.Configure(logcfg.Load())

	n, err := nodes.NewDefaultNode(GenerateKey(), "localhost:3000", 5, 5)
	if err != nil {
		logs.Errorf(err, "failed to create node")
		return
	}
	r := n.Router
	logs.Infof("Router: %+v", r)

	go n.Start()

	time.Sleep(20 * time.Second)

	err = n.Shutdown()
	if err != nil {
		logs.Errorf(err, "shutdown error")
	}
}
