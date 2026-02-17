package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/danmuck/dps_files/src/key_store"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	storageDir := flag.String("storage", "local/storage", "storage directory")
	flag.Parse()

	ks, err := key_store.InitKeyStore(*storageDir)
	if err != nil {
		log.Fatalf("failed to init keystore: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /files/{name}", handleUpload(ks))
	mux.HandleFunc("GET /files/hash/{hex}", handleDownloadByHash(ks))
	mux.HandleFunc("DELETE /files/hash/{hex}", handleDeleteByHash(ks))
	mux.HandleFunc("GET /files/{name}", handleDownloadByName(ks))
	mux.HandleFunc("GET /files", handleListFiles(ks))

	fmt.Printf("HTTP file server listening on %s (storage: %s)\n", *addr, *storageDir)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
