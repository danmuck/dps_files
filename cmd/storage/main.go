package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/danmuck/dps_files/cmd/internal/logcfg"
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

	keystore, err := key_store.InitKeyStoreWithConfig(cfg.KeyStore)
	if err != nil {
		logs.Fatalf(err, "Failed to initialize keystore")
	}
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

	indexedFiles, metadataCount, err := refreshMenuContext(cfg, keystore)
	if err != nil {
		logs.Fatalf(err, "Failed to prepare runtime context")
	}

	action, actionSource, err := promptAction(os.Stdin, &cfg, indexedFiles, metadataCount)
	if errors.Is(err, errMenuExit) {
		return
	}
	if err != nil {
		logs.Fatalf(err, "Failed to select action")
	}
	cfg.Action = action

	printRuntimeSummary(cfg, actionSource)
	if err := executeActionOnce(cfg, keystore, os.Stdin, indexedFiles); err != nil {
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
		indexedFiles, metadataCount, err := refreshMenuContext(cfg, keystore)
		if err != nil {
			return err
		}

		action, actionSource, err := promptAction(reader, &cfg, indexedFiles, metadataCount)
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
		err = executeActionOnce(cfg, keystore, reader, indexedFiles)
		if err != nil && !errors.Is(err, errMenuBack) {
			logs.Printf("\nAction %q failed: %v\n", cfg.Action, err)
		}
		if errors.Is(err, errMenuBack) {
			clearTerminalIfInteractive(input)
			continue
		}
	}
}

func refreshMenuContext(cfg RuntimeConfig, keystore *key_store.KeyStore) ([]string, int, error) {
	if err := keystore.ReloadLocalState(); err != nil {
		return nil, 0, fmt.Errorf("failed to reload keystore state: %w", err)
	}

	indexedFiles, err := getFilesInDirectory(cfg.UploadDirectory)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to index files in %s: %w", cfg.UploadDirectory, err)
	}
	sort.Strings(indexedFiles)

	metadataCount := len(keystore.ListKnownFiles())

	return indexedFiles, metadataCount, nil
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

func executeActionOnce(cfg RuntimeConfig, keystore *key_store.KeyStore, input io.Reader, indexedFiles []string) error {
	var selectedTargets []string

	switch cfg.Action {
	case ActionClean, ActionDeepClean, ActionVerify, ActionExpire:
		if cfg.Mode == ModeRemote {
			logs.Printf("Action %q is local-only. Switch to local mode to use it.\n", cfg.Action)
			return nil
		}
	}

	switch cfg.Action {
	case ActionClean:
		removed, err := cleanupAllKDHTFiles(cfg.KeyStore.StorageDir)
		if err != nil {
			return fmt.Errorf("failed to clean .kdht files: %w", err)
		}
		logs.Printf("Clean complete: removed %d .kdht file(s) from %s\n", removed, filepath.Join(cfg.KeyStore.StorageDir, "data"))
		return nil
	case ActionDeepClean:
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
		return executeVerifyAction(cfg, keystore)
	case ActionDelete:
		return executeDeleteAction(cfg, keystore, input)
	case ActionExpire:
		return executeExpireAction(cfg, keystore)
	case ActionDownload:
		return executeDownloadAction(cfg, keystore, input)
	case ActionUpload:
		selectedUploads, selection, err := promptUploadSelection(indexedFiles, input, cfg)
		if err != nil {
			return err
		}
		logs.Printf("Selection: %s\n", selection)

		selectedTargets = make([]string, 0, len(selectedUploads))
		for _, name := range selectedUploads {
			selectedTargets = append(selectedTargets, filepath.Join(cfg.UploadDirectory, name))
		}
	case ActionStore:
		storePath, selection, err := resolveStorePath(input, cfg)
		if err != nil {
			return err
		}
		logs.Printf("Selection: %s\n", selection)
		selectedTargets = []string{storePath}
	}

	switch cfg.Action {
	case ActionUpload:
		if cfg.CleanCopyFiles {
			if err := cleanupCopyFiles(cfg.KeyStore.StorageDir); err != nil {
				logs.Warnf("cleanup copy files failed: %v", err)
			}
		}
		if err := executeStoreTargets(cfg, keystore, selectedTargets); err != nil {
			return fmt.Errorf("upload action failed: %w", err)
		}
	case ActionStore:
		if cfg.CleanCopyFiles {
			if err := cleanupCopyFiles(cfg.KeyStore.StorageDir); err != nil {
				logs.Warnf("cleanup copy files failed: %v", err)
			}
		}
		if err := executeStoreTargets(cfg, keystore, selectedTargets); err != nil {
			return fmt.Errorf("store action failed: %w", err)
		}
	case ActionView:
		if err := executeViewAction(cfg, keystore, input); err != nil {
			return fmt.Errorf("view action failed: %w", err)
		}
	default:
		return fmt.Errorf("unsupported action: %s", cfg.Action)
	}

	if cfg.Mode == ModeRun {
		kdhtCount, err := countKDHTFiles(cfg.KeyStore.StorageDir)
		if err != nil {
			logs.Warnf("failed to count .kdht files: %v", err)
		} else {
			logs.Printf("\nStored .kdht files currently present: %d\n", kdhtCount)
		}
	}

	return nil
}

func clearTerminalIfInteractive(input io.Reader) {
	if !isInteractiveReader(input) {
		return
	}
	fmt.Print("\033[H\033[2J")
}
