package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/danmuck/dps_files/cmd/internal/logcfg"
	"github.com/danmuck/dps_files/src/api/nodes"
	"github.com/danmuck/dps_files/src/key_store"
	logs "github.com/danmuck/smplog"
)

func main() {
	logs.Configure(logcfg.Load())

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
		logs.Fatalf(err, "Failed to ensure upload directory %s", cfg.UploadDirectory)
	}

	if err := createDirPath(cfg.KeyStore.StorageDir); err != nil {
		logs.Fatalf(err, "Failed to ensure storage directory %s", cfg.KeyStore.StorageDir)
	}

	// Create a ClientNode with local storage.
	id := make([]byte, 20)
	if _, err := rand.Read(id); err != nil {
		logs.Fatalf(err, "Failed to generate node ID")
	}

	var clientOpts []nodes.ClientOption
	clientOpts = append(clientOpts, nodes.WithLocalStorage(cfg.KeyStore.StorageDir))

	// If remotes are configured, add them.
	if cfg.RemoteAddr != "" {
		clientOpts = append(clientOpts, nodes.WithRemotes(cfg.RemoteAddr))
	}

	cn, err := nodes.NewClientNode(id, clientOpts...)
	if err != nil {
		logs.Fatalf(err, "Failed to create client node")
	}
	if err := cn.Start(); err != nil {
		logs.Fatalf(err, "Failed to start client node")
	}
	defer cn.Shutdown()

	// Extract the KeyStore from the local server for TUI operations.
	keystore := cn.LocalServer().RawKeyStore()
	if keystore == nil {
		logs.Fatalf(nil, "Failed to access local KeyStore from client node")
	}

	// Override the keystore config with the CLI-parsed config.
	// The ClientNode created its own KeyStore with defaults from the storage dir,
	// but we need to apply CLI settings (verbose, TTL, etc.).
	keystore.ApplyConfig(cfg.KeyStore)

	logs.Printf("KeyStore initialized: %d file(s) loaded.\n", len(keystore.ListKnownFiles()))

	if cfg.CleanKDHTOnExit {
		defer func() {
			if err := keystore.CleanupKDHT(); err != nil {
				logs.Warnf("CleanupKDHT failed: %v", err)
			}
		}()
	}

	remotesCfg, remErr := loadRemotesConfig("./local/remotes.toml")
	if remErr != nil {
		logs.Warnf("could not load remotes config: %v", remErr)
	} else {
		cfg.KnownRemotes = remotesCfg.Remotes
	}
	if cfg.Mode == ModeRemote && cfg.RemoteAddr == "" && len(cfg.KnownRemotes) > 0 {
		cfg.RemoteAddr = cfg.KnownRemotes[0].Address
	}

	if shouldRunInteractiveSession(cfg, os.Stdin) {
		if err := runInteractiveSession(cfg, keystore, os.Stdin); err != nil {
			logs.Fatalf(err, "Interactive session failed")
		}
		return
	}

	metadataCount, err := refreshMenuContext(cfg, keystore)
	if err != nil {
		logs.Fatalf(err, "Failed to prepare runtime context")
	}

	action, actionSource, err := promptAction(os.Stdin, &cfg, metadataCount)
	if errors.Is(err, errMenuExit) {
		return
	}
	if err != nil {
		logs.Fatalf(err, "Failed to select action")
	}
	cfg.Action = action

	printRuntimeSummary(cfg, actionSource)
	if err := executeActionOnce(cfg, keystore, os.Stdin); err != nil {
		if errors.Is(err, errMenuBack) {
			logs.Println("Action cancelled.")
			return
		}
		logs.Fatalf(err, "Action %q failed", cfg.Action)
	}
}

func shouldRunInteractiveSession(cfg RuntimeConfig, input io.Reader) bool {
	return !cfg.ActionProvided && isInteractiveReader(input)
}

func runInteractiveSession(cfg RuntimeConfig, keystore *key_store.KeyStore, input io.Reader) error {
	reader := getBufferedReader(input)
	clearTerminalIfInteractive(input)

	for {
		metadataCount, err := refreshMenuContext(cfg, keystore)
		if err != nil {
			return err
		}

		action, actionSource, err := promptAction(reader, &cfg, metadataCount)
		if errors.Is(err, errMenuExit) {
			clearTerminalIfInteractive(input)
			logs.Println("Exited keystore menu.")
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to select action: %w", err)
		}

		cfg.Action = action
		clearTerminalIfInteractive(input)
		printRuntimeSummary(cfg, actionSource)
		err = executeActionOnce(cfg, keystore, reader)
		if err != nil && !errors.Is(err, errMenuBack) {
			logs.Printf("\nAction %q failed: %v\n", cfg.Action, err)
		}
		if errors.Is(err, errMenuBack) {
			clearTerminalIfInteractive(input)
			continue
		}
	}
}

func refreshMenuContext(cfg RuntimeConfig, keystore *key_store.KeyStore) (int, error) {
	if err := keystore.ReloadLocalState(); err != nil {
		return 0, fmt.Errorf("failed to reload keystore state: %w", err)
	}
	return len(keystore.ListKnownFiles()), nil
}

func printRuntimeSummary(cfg RuntimeConfig, actionSource string) {
	logs.Printf("\n")
	logs.Field("Execution mode", cfg.Mode)
	logs.Printf("\n")
	logs.Field("TTL seconds", cfg.TTLSeconds)
	logs.Printf("\n")
	logs.Field("Reassembly enabled", cfg.ReassembleEnabled)
	logs.Printf("\n")
	logs.Field("Action", actionSource)
	logs.Printf("\n")
	logs.Field("Storage root path", cfg.KeyStore.StorageDir)
	logs.Printf("\n")
}

func executeActionOnce(cfg RuntimeConfig, keystore *key_store.KeyStore, input io.Reader) error {
	switch cfg.Action {
	case ActionClean:
		if cfg.Mode == ModeRemote {
			return executeRemoteClean(cfg, false)
		}
		removed, err := cleanupAllKDHTFiles(cfg.KeyStore.StorageDir)
		if err != nil {
			return fmt.Errorf("failed to clean .kdht files: %w", err)
		}
		logs.Printf("Clean complete: removed %d .kdht file(s) from %s\n", removed, filepath.Join(cfg.KeyStore.StorageDir, "data"))
		return nil
	case ActionDeepClean:
		if cfg.Mode == ModeRemote {
			return executeRemoteClean(cfg, true)
		}
		result, err := deepCleanStorage(cfg.KeyStore.StorageDir)
		if err != nil {
			return fmt.Errorf("failed to deep clean storage: %w", err)
		}
		logs.Printf("Deep clean complete: removed %d .kdht, %d metadata file(s), %d cache file(s).\n",
			result.RemovedKDHT,
			result.RemovedMetadata,
			result.RemovedCache,
		)
		return nil
	case ActionStats:
		if err := executeStatsAction(cfg); err != nil {
			return fmt.Errorf("failed to collect stats: %w", err)
		}
		return nil
	case ActionVerify:
		if cfg.Mode == ModeRemote {
			return executeRemoteVerify(cfg)
		}
		return executeVerifyAction(cfg, keystore)
	case ActionDelete:
		return executeDeleteAction(cfg, keystore, input)
	case ActionExpire:
		if cfg.Mode == ModeRemote {
			return executeRemoteExpire(cfg)
		}
		return executeExpireAction(cfg, keystore)
	case ActionDownload:
		return executeDownloadAction(cfg, keystore, input)
	case ActionUpload:
		resolvedPath, isDir, resolveErr := promptUploadPath(input, cfg)
		if resolveErr != nil {
			return resolveErr
		}

		if isDir {
			if cfg.Mode == ModeRemote {
				return executeRemoteUploadDirAction(cfg, input, resolvedPath)
			}
			confirmed, confirmErr := confirmDirectoryUpload(input, resolvedPath)
			if confirmErr != nil {
				return confirmErr
			}
			if !confirmed {
				return errMenuBack
			}
			logs.Printf("\nUploading directory %q...\n", resolvedPath)
			dirHash, storeErr := keystore.StoreDirectory(resolvedPath)
			if storeErr != nil {
				return fmt.Errorf("store directory: %w", storeErr)
			}
			logs.Printf("Directory stored. Root hash: %x\n", dirHash)
			return nil
		}

		// File path. Check for the "all" sentinel (upload dir itself was returned).
		var filePaths []string
		if resolvedPath == filepath.Clean(cfg.UploadDirectory) {
			entries, entErr := getUploadDirEntries(cfg.UploadDirectory)
			if entErr != nil {
				return fmt.Errorf("index upload dir: %w", entErr)
			}
			for _, e := range entries {
				if !e.IsDir {
					filePaths = append(filePaths, filepath.Join(cfg.UploadDirectory, e.Name))
				}
			}
			if len(filePaths) == 0 {
				logs.Println("No files found in upload directory.")
				return nil
			}
		} else {
			filePaths = []string{resolvedPath}
		}

		if cfg.CleanCopyFiles {
			if err := cleanupCopyFiles(cfg.KeyStore.StorageDir); err != nil {
				logs.Warnf("cleanup copy files: %v", err)
			}
		}
		return executeStoreTargets(cfg, keystore, filePaths)
	case ActionView:
		if err := executeViewAction(cfg, keystore, input); err != nil {
			return fmt.Errorf("view action failed: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported action: %s", cfg.Action)
	}
}

func clearTerminalIfInteractive(input io.Reader) {
	if !isInteractiveReader(input) {
		return
	}
	fmt.Print("\033[H\033[2J")
}

func executeRemoteUploadDirAction(cfg RuntimeConfig, input io.Reader, dirPath string) error {
	if cfg.RemoteAddr == "" {
		return fmt.Errorf("remote mode requires an address; use %s or configure remotes", REMOTE_ADDR_FLAG)
	}
	confirmed, confirmErr := confirmDirectoryUpload(input, dirPath)
	if confirmErr != nil {
		return confirmErr
	}
	if !confirmed {
		return errMenuBack
	}
	client, err := NewGRPCClient(cfg.RemoteAddr)
	if err != nil {
		return fmt.Errorf("connect to remote: %w", err)
	}
	defer client.Close()
	logs.Printf("\nUploading directory %q to remote %s...\n", dirPath, cfg.RemoteAddr)
	rootHash, err := executeRemoteUploadDir(client, dirPath, dirPath)
	if err != nil {
		return fmt.Errorf("remote dir upload: %w", err)
	}
	logs.Printf("Directory upload complete. Root hash: %x\n", rootHash)
	return nil
}

func executeRemoteVerify(cfg RuntimeConfig) error {
	if cfg.RemoteAddr == "" {
		return fmt.Errorf("remote mode requires an address")
	}
	client, err := NewGRPCClient(cfg.RemoteAddr)
	if err != nil {
		return fmt.Errorf("connect to remote: %w", err)
	}
	defer client.Close()
	issues, err := client.Verify()
	if err != nil {
		return fmt.Errorf("remote verify: %w", err)
	}
	if len(issues) == 0 {
		logs.StatusInfo("Remote: all chunks verified — healthy.")
		logs.Printf("\n")
		return nil
	}
	logs.Printf("Remote found %d integrity error(s):\n", len(issues))
	for _, iss := range issues {
		logs.MenuItem(int(iss.ChunkIndex), iss.FileName+" — "+iss.Err, false)
		logs.Printf("\n")
	}
	return nil
}

func executeRemoteExpire(cfg RuntimeConfig) error {
	if cfg.RemoteAddr == "" {
		return fmt.Errorf("remote mode requires an address")
	}
	client, err := NewGRPCClient(cfg.RemoteAddr)
	if err != nil {
		return fmt.Errorf("connect to remote: %w", err)
	}
	defer client.Close()
	removed, err := client.Expire()
	if err != nil {
		return fmt.Errorf("remote expire: %w", err)
	}
	logs.Printf("Remote expire complete: %d file(s) removed.\n", removed)
	return nil
}

func executeRemoteClean(cfg RuntimeConfig, deep bool) error {
	if cfg.RemoteAddr == "" {
		return fmt.Errorf("remote mode requires an address")
	}
	client, err := NewGRPCClient(cfg.RemoteAddr)
	if err != nil {
		return fmt.Errorf("connect to remote: %w", err)
	}
	defer client.Close()
	result, err := client.Clean(deep)
	if err != nil {
		return fmt.Errorf("remote clean: %w", err)
	}
	if deep {
		logs.Printf("Remote deep clean: removed %d .kdht, %d metadata, %d cache file(s).\n",
			result.RemovedKDHT, result.RemovedMetadata, result.RemovedCache)
	} else {
		logs.Printf("Remote clean: removed %d .kdht file(s).\n", result.RemovedKDHT)
	}
	return nil
}
