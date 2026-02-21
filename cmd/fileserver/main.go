package main

import (
	"flag"
	"net"

	"github.com/danmuck/dps_files/cmd/internal/logcfg"
	"github.com/danmuck/dps_files/src/key_store"
	logs "github.com/danmuck/smplog"
)

func main() {
	logs.Configure(logcfg.Load())

	addr := flag.String("addr", ":9000", "TCP listen address")
	storageDir := flag.String("storage", "local/storage", "storage directory")
	flag.Parse()

	ks, err := key_store.InitKeyStore(*storageDir)
	if err != nil {
		logs.Fatalf(err, "failed to init keystore")
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		logs.Fatalf(err, "failed to listen")
	}
	defer ln.Close()

	logs.Infof("TCP file server listening on %s (storage: %s)", *addr, *storageDir)

	for {
		conn, err := ln.Accept()
		if err != nil {
			logs.Warnf("accept error: %v", err)
			continue
		}
		go handleConn(ks, conn)
	}
}
