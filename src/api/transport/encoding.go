package transport

import (
	"encoding/binary"
	"io"

	logs "github.com/danmuck/smplog"
	"google.golang.org/protobuf/proto"
)

type Coder interface {
	Encode(*RPC) ([]byte, error)
	Decode(io.Reader) (*RPC, error)
}

////////////////////////////////////////////////////////////////////////////////////////////
////////////////////////////////////////////////////////////////////////////////////////////
////////////////////////////////////////////////////////////////////////////////////////////
////////////////////////////////////////////////////////////////////////////////////////////

type DefaultCoder struct{}

func (c DefaultCoder) Encode(rpc *RPC) ([]byte, error) {
	logs.Debugf("Encode(default: Google Protobuf): %+v", rpc)
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
	logs.Debugf("Decode(default: Google Protobuf)")
	// Get the header if the connection is valid, and convert to uint16
	headerBuf := make([]byte, 2)
	_, err := io.ReadFull(r, headerBuf)
	if err != nil {
		if err.Error() != "EOF" {
			logs.Errorf(err, "Decode error")
		}
		return nil, err
	}

	// Get the message using the header value
	msgLength := binary.BigEndian.Uint16(headerBuf[:])
	msgBuf := make([]byte, int(msgLength))
	_, err = io.ReadFull(r, msgBuf)
	if err != nil {
		logs.Errorf(err, "Decode error")
		return nil, err
	}

	// declare an RPC, unmarshal it, receive it
	rpc := &RPC{}
	err = proto.Unmarshal(msgBuf, rpc)
	if err != nil {
		logs.Errorf(err, "Decode error")
		return nil, err
	}
	logs.Debugf("Decode(done): %+v", rpc)

	return rpc, nil
}
