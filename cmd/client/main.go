package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/danmuck/dps_files/cmd/internal/logcfg"
	"github.com/danmuck/dps_files/src/api/transport"
	logs "github.com/danmuck/smplog"

	"google.golang.org/protobuf/proto"
)

func main() {
	logs.Configure(logcfg.Load())

	address := "localhost:3000" // Replace with your server address
	logs.Infof("Connecting to server at %s...", address)

	conn, err := net.Dial("tcp", address)
	if err != nil {
		logs.Errorf(err, "Failed to connect to server")
		return
	}
	defer conn.Close()

	fmt.Println("Connected. Type your message and press Enter to send. Type 'exit' to quit.")

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		scanner.Scan()
		input := scanner.Text()

		if input == "exit" {
			fmt.Println("Exiting...")
			break
		}

		// Create a Protobuf message
		msg := &transport.RPC{
			Meta: &transport.RPCT{
				Command:  transport.Command_PING,
				Protocol: transport.Protocol_Kademlia,
			},
			Sender: &transport.NodeInfo{
				Address: conn.LocalAddr().String(),
				Id:      []byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14},
				Time:    time.Now().UnixNano(),
			},
			Payload: []byte(input),
		}

		// Serialize the Protobuf message
		data, err := proto.Marshal(msg)
		if err != nil {
			logs.Errorf(err, "Failed to serialize message")
			continue
		}

		hdr := make([]byte, 2)
		binary.BigEndian.PutUint16(hdr, uint16(len(data)))
		data = append(hdr, data...)

		// Write the serialized data to the connection
		_, err = conn.Write(data)
		if err != nil {
			logs.Errorf(err, "Failed to send message")
			break
		}

		logs.Infof("Message sent. Length: %v", len(data))
	}
}
