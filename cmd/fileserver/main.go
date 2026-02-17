package main

import (
	"flag"
	"fmt"
	"log"
	"net"

	"github.com/danmuck/dps_files/src/key_store"
)

func main() {
	addr := flag.String("addr", ":9000", "TCP listen address")
	storageDir := flag.String("storage", "local/storage", "storage directory")
	flag.Parse()

	ks, err := key_store.InitKeyStore(*storageDir)
	if err != nil {
		log.Fatalf("failed to init keystore: %v", err)
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	fmt.Printf("TCP file server listening on %s (storage: %s)\n", *addr, *storageDir)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("accept error: %v", err)
			continue
		}
		go handleConn(ks, conn)
	}
}
