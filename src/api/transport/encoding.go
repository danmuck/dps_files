package transport

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"google.golang.org/protobuf/proto"
)

type Coder interface {
	Encode(*RPC) ([]byte, error)
	Decode(io.Reader) (*RPC, error)
}

type DefaultCoder struct{}

func (c DefaultCoder) Encode(rpc *RPC) ([]byte, error) {
	fmt.Printf("Encode(default: Google Protobuf): %+v \n", rpc)
	out, err := proto.Marshal(rpc)
	if err != nil {
		return nil, err
	}
	hdr := make([]byte, 2)
	binary.BigEndian.PutUint16(hdr, uint16(len(out)))
	out = append(hdr, out...)

	return out, nil
}

func (c DefaultCoder) Decode(r io.Reader) (*RPC, error) {
	fmt.Printf("Decode(default: Google Protobuf) \n")
	// Get the header if the connection is valid, and convert to uint16
	headerBuf := make([]byte, 2)
	_, err := io.ReadFull(r, headerBuf)
	if err != nil {
		if err.Error() != "EOF" {
			fmt.Fprintf(os.Stderr, "!Decode(error): %v \n", err.Error())
		}
		return nil, err
	}

	// Get the message using the header value
	msgLength := binary.BigEndian.Uint16(headerBuf[:])
	msgBuf := make([]byte, int(msgLength))
	_, err = io.ReadFull(r, msgBuf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "!Decode(error): %v \n", err.Error())
		return nil, err
	}

	// declare an RPC, unmarshal it, receive it
	rpc := &RPC{}
	err = proto.Unmarshal(msgBuf, rpc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "!Decode(error): %v \n", err.Error())
		return nil, err
	}
	fmt.Printf("Decode(done): %+v \n", rpc)

	return rpc, nil
}
