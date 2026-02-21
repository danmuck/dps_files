package main

import (
	"flag"
	"net/http"

	"github.com/danmuck/dps_files/cmd/internal/logcfg"
	"github.com/danmuck/dps_files/src/key_store"
	logs "github.com/danmuck/smplog"
)

func main() {
	logs.Configure(logcfg.Load())

	addr := flag.String("addr", ":8080", "HTTP listen address")
	storageDir := flag.String("storage", "local/storage", "storage directory")
	flag.Parse()

	ks, err := key_store.InitKeyStore(*storageDir)
	if err != nil {
		logs.Fatalf(err, "failed to init keystore")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /files/{name}", handleUpload(ks))
	mux.HandleFunc("GET /files/hash/{hex}", handleDownloadByHash(ks))
	mux.HandleFunc("DELETE /files/hash/{hex}", handleDeleteByHash(ks))
	mux.HandleFunc("GET /files/{name}", handleDownloadByName(ks))
	mux.HandleFunc("GET /files", handleListFiles(ks))

	logs.Infof("HTTP file server listening on %s (storage: %s)", *addr, *storageDir)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		logs.Fatal(err, "server exited")
	}
}
