package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/danmuck/dps_files/src/key_store"
)

func main() {
	cfg, err := parseCLI(os.Args[1:], defaultRuntimeConfig)
	if err != nil {
		indexedFiles, indexErr := getFilesInDirectory(defaultRuntimeConfig.UploadDirectory)
		if indexErr == nil {
			sort.Strings(indexedFiles)
		}
		fmt.Printf("Error: %v\n\n", err)
		printUsage(indexedFiles, defaultRuntimeConfig)
		if indexErr != nil {
			fmt.Printf("\nIndexing error: %v\n", indexErr)
		}
		os.Exit(1)
	}

	if err := createDirPath(cfg.UploadDirectory); err != nil {
		log.Fatalf("Failed to ensure upload directory %s: %v", cfg.UploadDirectory, err)
	}

	if err := createDirPath(cfg.KeyStore.StorageDir); err != nil {
		log.Fatalf("Failed to ensure storage directory %s: %v", cfg.KeyStore.StorageDir, err)
	}

	indexedFiles, err := getFilesInDirectory(cfg.UploadDirectory)
	if err != nil {
		log.Fatalf("Failed to index files in %s: %v", cfg.UploadDirectory, err)
	}
	sort.Strings(indexedFiles)

	keystore, err := key_store.InitKeyStoreWithConfig(cfg.KeyStore)
	if err != nil {
		log.Fatalf("Failed to initialize keystore: %v", err)
	}

	if cfg.CleanKDHTOnExit {
		defer func() {
			if err := keystore.CleanupKDHT(); err != nil {
				log.Printf("Warning: CleanupKDHT failed: %v", err)
			}
		}()
	}

	metadataCount := len(keystore.ListKnownFiles())
	if metadataCount == 0 {
		metadataCount, err = metadataFileCount(cfg.KeyStore.StorageDir)
		if err != nil {
			log.Fatalf("Failed to count metadata entries: %v", err)
		}
	}

	action, actionSource, err := promptAction(os.Stdin, cfg, indexedFiles, metadataCount)
	if err != nil {
		log.Fatalf("Failed to select action: %v", err)
	}
	cfg.Action = action

	fmt.Printf("\nExecution mode: %s\n", cfg.Mode)
	fmt.Printf("TTL seconds: %d\n", cfg.TTLSeconds)
	fmt.Printf("Reassembly enabled: %v\n", cfg.ReassembleEnabled)
	fmt.Printf("Action: %s\n", actionSource)
	fmt.Printf("Storage root path: %s\n", cfg.KeyStore.StorageDir)

	var selectedTargets []string

	switch cfg.Action {
	case ActionClean:
		removed, err := cleanupAllKDHTFiles(cfg.KeyStore.StorageDir)
		if err != nil {
			log.Fatalf("Failed to clean .kdht files: %v", err)
		}
		fmt.Printf("Clean complete: removed %d .kdht file(s) from %s\n", removed, filepath.Join(cfg.KeyStore.StorageDir, "data"))
		return
	case ActionDeepClean:
		result, err := deepCleanStorage(cfg.KeyStore.StorageDir)
		if err != nil {
			log.Fatalf("Failed to deep clean storage: %v", err)
		}
		fmt.Printf("Deep clean complete: removed %d .kdht, %d metadata file(s), %d cache file(s).\n",
			result.RemovedKDHT,
			result.RemovedMetadata,
			result.RemovedCache,
		)
		return
	case ActionUpload:
		selectedUploads, selection, err := promptUploadSelection(indexedFiles, os.Stdin, cfg)
		if err != nil {
			log.Fatalf("Failed to select upload file(s): %v", err)
		}
		fmt.Printf("Selection: %s\n", selection)

		selectedTargets = make([]string, 0, len(selectedUploads))
		for _, name := range selectedUploads {
			selectedTargets = append(selectedTargets, filepath.Join(cfg.UploadDirectory, name))
		}
	case ActionStore:
		storePath, selection, err := resolveStorePath(os.Stdin, cfg)
		if err != nil {
			log.Fatalf("Failed to resolve store path: %v", err)
		}
		fmt.Printf("Selection: %s\n", selection)
		selectedTargets = []string{storePath}
	}

	switch cfg.Action {
	case ActionUpload:
		if cfg.CleanCopyFiles {
			if err := cleanupCopyFiles(cfg.KeyStore.StorageDir); err != nil {
				log.Printf("Warning: cleanup copy files failed: %v", err)
			}
		}
		if err := executeStoreTargets(cfg, keystore, selectedTargets); err != nil {
			log.Fatalf("Upload action failed: %v", err)
		}
	case ActionStore:
		if cfg.CleanCopyFiles {
			if err := cleanupCopyFiles(cfg.KeyStore.StorageDir); err != nil {
				log.Printf("Warning: cleanup copy files failed: %v", err)
			}
		}
		if err := executeStoreTargets(cfg, keystore, selectedTargets); err != nil {
			log.Fatalf("Store action failed: %v", err)
		}
	case ActionView:
		if err := executeViewAction(cfg, keystore); err != nil {
			log.Fatalf("View action failed: %v", err)
		}
	default:
		log.Fatalf("Unsupported action: %s", cfg.Action)
	}

	kdhtCount, err := countKDHTFiles(cfg.KeyStore.StorageDir)
	if err != nil {
		log.Printf("Warning: failed to count .kdht files: %v", err)
	} else {
		fmt.Printf("\nStored .kdht files currently present: %d\n", kdhtCount)
	}
}
