package main

import (
	"fmt"
	"time"

	"math/rand"

	"github.com/danmuck/dps_files/src/api/nodes"
	"github.com/danmuck/dps_files/src/api/transport"
)

func handleInbound(h transport.TCPHandler) {
	for {
		rpc := <-h.ProcessRPC()
		// ch <- rpc
		if rpc != nil {
			fmt.Printf("handleInbound(%s) \n", rpc.Sender.Address)
			continue
		}
		fmt.Printf("handleInbound(): exiting \n")
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
	n, err := nodes.NewDefaultNode(GenerateKey(), "localhost:3000", 5, 5)
	if err != nil {
		fmt.Println(err)
		return
	}
	r := n.Router
	fmt.Printf("Router: %+v \n", r)

	go n.Start()

	time.Sleep(20 * time.Second)

	err = n.Shutdown()
	fmt.Println(err)

}
