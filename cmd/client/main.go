package main

import (
	"bufio"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/danmuck/dps_files/cmd/internal/logcfg"
	"github.com/danmuck/dps_files/cmd/internal/tuicfg"
	"github.com/danmuck/dps_files/src/api/nodes"
	tui "github.com/danmuck/tui_go"
	logs "github.com/danmuck/smplog"
)

func main() {
	logs.Configure(logcfg.Load())
	tui.Configure(tuicfg.Load())

	cfg, err := parseCLI(os.Args[1:], defaultRuntimeConfig)
	cfg.TUI = tui.NewTUI(os.Stdout)
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

	// Determine the server address: use embedded server by default, CLI remote overrides.
	cfg.ServerAddr = cn.LocalServer().Addr()
	cfg.ServerLabel = fmt.Sprintf("local @ %s", cfg.ServerAddr)

	if cfg.RemoteAddr != "" {
		cfg.ServerAddr = cfg.RemoteAddr
		cfg.ServerLabel = fmt.Sprintf("remote @ %s", cfg.ServerAddr)
	}

	// Create the unified gRPC client.
	client, err := NewGRPCClient(cfg.ServerAddr)
	if err != nil {
		logs.Fatalf(err, "Failed to connect to server %s", cfg.ServerAddr)
	}
	defer client.Close()

	remotesCfg, remErr := loadRemotesConfig("./local/remotes.toml")
	if remErr != nil {
		logs.Warnf("could not load remotes config: %v", remErr)
	} else {
		cfg.KnownRemotes = remotesCfg.Remotes
	}

	if shouldRunInteractiveSession(cfg, os.Stdin) {
		if err := runInteractiveSession(cfg, client, os.Stdin); err != nil {
			logs.Fatalf(err, "Interactive session failed")
		}
		return
	}

	metadataCount, err := refreshMenuContext(cfg, client)
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
	if err := executeActionOnce(cfg, client, os.Stdin); err != nil {
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

func runInteractiveSession(cfg RuntimeConfig, client *GRPCClient, input io.Reader) error {
	reader := getBufferedReader(input)
	clearTerminalIfInteractive(input)

	currentAddr := cfg.ServerAddr
	for {
		if cfg.ServerAddr != currentAddr {
			client.Close()
			var err error
			client, err = NewGRPCClient(cfg.ServerAddr)
			if err != nil {
				return fmt.Errorf("reconnect to %s: %w", cfg.ServerAddr, err)
			}
			currentAddr = cfg.ServerAddr
		}

		metadataCount, err := refreshMenuContext(cfg, client)
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

		// Reconnect if server was switched during the prompt loop.
		if cfg.ServerAddr != currentAddr {
			client.Close()
			var reconnErr error
			client, reconnErr = NewGRPCClient(cfg.ServerAddr)
			if reconnErr != nil {
				return fmt.Errorf("reconnect to %s: %w", cfg.ServerAddr, reconnErr)
			}
			currentAddr = cfg.ServerAddr
		}

		clearTerminalIfInteractive(input)
		printRuntimeSummary(cfg, actionSource)
		err = executeActionOnce(cfg, client, reader)
		if err != nil && !errors.Is(err, errMenuBack) {
			logs.Printf("\nAction %q failed: %v\n", cfg.Action, err)
		}
		if !errors.Is(err, errMenuBack) {
			waitForEnter(reader, input)
		}
		clearTerminalIfInteractive(input)
	}
}

func refreshMenuContext(cfg RuntimeConfig, client *GRPCClient) (int, error) {
	entries, err := client.List()
	if err != nil {
		return 0, fmt.Errorf("list files: %w", err)
	}
	return len(entries), nil
}

func printRuntimeSummary(cfg RuntimeConfig, actionSource string) {
	logs.Printf("\n")
	cfg.TUI.FieldFU("Server", cfg.ServerLabel)
	logs.Printf("\n")
	cfg.TUI.FieldFU("TTL seconds", cfg.TTLSeconds)
	logs.Printf("\n")
	cfg.TUI.FieldFU("Action", actionSource)
	logs.Printf("\n")
	cfg.TUI.FieldFU("Storage root path", cfg.KeyStore.StorageDir)
	logs.Printf("\n")
}

func executeActionOnce(cfg RuntimeConfig, client *GRPCClient, input io.Reader) error {
	switch cfg.Action {
	case ActionClean:
		result, err := client.Clean(false)
		if err != nil {
			return fmt.Errorf("clean: %w", err)
		}
		logs.Printf("Clean complete: removed %d .kdht file(s).\n", result.RemovedKDHT)
		return nil
	case ActionDeepClean:
		result, err := client.Clean(true)
		if err != nil {
			return fmt.Errorf("deep clean: %w", err)
		}
		logs.Printf("Deep clean complete: removed %d .kdht, %d metadata, %d cache file(s).\n",
			result.RemovedKDHT, result.RemovedMetadata, result.RemovedCache)
		return nil
	case ActionStats:
		if err := executeStatsAction(cfg, client); err != nil {
			return fmt.Errorf("failed to collect stats: %w", err)
		}
		return nil
	case ActionVerify:
		return executeVerifyAction(cfg, client, input)
	case ActionDelete:
		return executeDeleteAction(cfg, client, input)
	case ActionExpire:
		return executeExpireAction(cfg, client)
	case ActionDownload:
		return executeDownloadAction(cfg, client, input)
	case ActionUpload:
		resolvedPath, isDir, resolveErr := promptUploadPath(input, cfg)
		if resolveErr != nil {
			return resolveErr
		}

		if isDir {
			return executeUploadDirAction(cfg, client, input, resolvedPath)
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

		return executeStoreTargets(cfg, client, filePaths)
	case ActionView:
		if err := executeViewAction(cfg, client, input); err != nil {
			return fmt.Errorf("view action failed: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported action: %s", cfg.Action)
	}
}

func waitForEnter(reader *bufio.Reader, input io.Reader) {
	if !isInteractiveReader(input) {
		return
	}
	fmt.Print("\nPress Enter to continue...")
	reader.ReadString('\n')
}

func clearTerminalIfInteractive(input io.Reader) {
	if !isInteractiveReader(input) {
		return
	}
	fmt.Print("\033[H\033[2J")
}

func executeUploadDirAction(cfg RuntimeConfig, client *GRPCClient, input io.Reader, dirPath string) error {
	confirmed, confirmErr := confirmDirectoryUpload(input, cfg, dirPath)
	if confirmErr != nil {
		return confirmErr
	}
	if !confirmed {
		return errMenuBack
	}
	logs.Printf("\nUploading directory %q to %s...\n", dirPath, cfg.ServerAddr)
	rootHash, totalSize, err := executeRemoteUploadDir(client, dirPath, dirPath)
	if err != nil {
		return fmt.Errorf("dir upload: %w", err)
	}
	logs.Printf("Directory upload complete. Root hash: %x  total size: %s\n", rootHash, formatBytes(totalSize))
	return nil
}
