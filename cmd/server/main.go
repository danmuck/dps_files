package main

import (
	"crypto/rand"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/danmuck/dps_files/cmd/internal/logcfg"
	"github.com/danmuck/dps_files/src/api/nodes"
	logs "github.com/danmuck/smplog"
)

func main() {
	logs.Configure(logcfg.Load())

	addr := flag.String("addr", ":9000", "TCP listen address")
	httpAddr := flag.String("http", "", "HTTP listen address (optional, e.g. :8080)")
	storageDir := flag.String("storage", "local/storage", "storage directory")
	flag.Parse()

	id := make([]byte, 20)
	if _, err := rand.Read(id); err != nil {
		logs.Fatalf(err, "failed to generate node ID")
	}

	var opts []nodes.ServerOption
	if *httpAddr != "" {
		opts = append(opts, nodes.WithHTTP(*httpAddr))
	}

	sn, err := nodes.NewServerNode(id, *addr, *storageDir, opts...)
	if err != nil {
		logs.Fatalf(err, "failed to create server node")
	}

	if err := sn.Start(); err != nil {
		logs.Fatalf(err, "failed to start server node")
	}

	logs.Infof("Server node listening on %s (storage: %s)", sn.Addr(), *storageDir)
	if *httpAddr != "" {
		logs.Infof("HTTP server on %s", *httpAddr)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	logs.Infof("Shutting down...")
	if err := sn.Shutdown(); err != nil {
		logs.Errorf(err, "shutdown error")
	}
}
