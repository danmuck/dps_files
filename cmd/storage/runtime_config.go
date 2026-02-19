package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/danmuck/dps_files/src/key_store"
)

type MenuAction string

const (
	ModeRun    = "run"
	ModeRemote = "remote"

	ActionUpload    MenuAction = "upload"
	ActionStore     MenuAction = "store"
	ActionClean     MenuAction = "clean"
	ActionDeepClean MenuAction = "deep-clean"
	ActionView      MenuAction = "view"
	ActionStats     MenuAction = "stats"
	ActionVerify    MenuAction = "verify"
	ActionDelete    MenuAction = "delete"
	ActionExpire    MenuAction = "expire"
	ActionStream    MenuAction = "stream"
)

const defaultRuntimeTTLSeconds uint64 = 300

type RuntimeConfig struct {
	UploadDirectory   string
	RunAll            bool
	DefaultFileIndex  int
	Mode              string
	ReassembleEnabled bool
	CleanCopyFiles    bool
	CleanKDHTOnExit   bool
	Action            MenuAction
	ActionProvided    bool
	StoreFilePath     string
	TTLSeconds        uint64
	KeyStore          key_store.KeyStoreConfig
}

func defaultConfig() RuntimeConfig {
	ksCfg := key_store.DefaultConfig("./local/storage")
	ksCfg.DefaultTTLSeconds = defaultRuntimeTTLSeconds
	return RuntimeConfig{
		UploadDirectory:   "./local/upload/",
		RunAll:            false,
		DefaultFileIndex:  0,
		Mode:              ModeRun,
		ReassembleEnabled: false,
		CleanCopyFiles:    true,
		CleanKDHTOnExit:   false,
		Action:            ActionUpload,
		ActionProvided:    false,
		StoreFilePath:     "",
		TTLSeconds:        defaultRuntimeTTLSeconds,
		KeyStore:          ksCfg,
	}
}

var defaultRuntimeConfig = defaultConfig()

const REASSEMBLE_FLAG = "--reassemble"
const TTL_SECONDS_FLAG = "--ttl-seconds"
const STORE_PATH_FLAG = "--store-path"
const VERBOSE_FLAG = "--verbose"

func parseCLI(args []string, cfg RuntimeConfig) (RuntimeConfig, error) {
	runtimeCfg := cfg
	modeProvided := false
	actionProvided := false

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == VERBOSE_FLAG {
			runtimeCfg.KeyStore.Verbose = true
			continue
		}

		if arg == REASSEMBLE_FLAG {
			runtimeCfg.ReassembleEnabled = true
			continue
		}

		if arg == TTL_SECONDS_FLAG {
			if i+1 >= len(args) {
				return runtimeCfg, fmt.Errorf("missing value after %q", TTL_SECONDS_FLAG)
			}
			i++
			parsed, err := strconv.ParseUint(strings.TrimSpace(args[i]), 10, 64)
			if err != nil {
				return runtimeCfg, fmt.Errorf("invalid %s value %q: %w", TTL_SECONDS_FLAG, args[i], err)
			}
			if parsed == 0 {
				return runtimeCfg, fmt.Errorf("%s must be >= 1", TTL_SECONDS_FLAG)
			}
			runtimeCfg.TTLSeconds = parsed
			runtimeCfg.KeyStore.DefaultTTLSeconds = parsed
			continue
		}

		if after, ok := strings.CutPrefix(arg, TTL_SECONDS_FLAG+"="); ok {
			raw := strings.TrimSpace(after)
			parsed, err := strconv.ParseUint(raw, 10, 64)
			if err != nil {
				return runtimeCfg, fmt.Errorf("invalid %s value %q: %w", TTL_SECONDS_FLAG, raw, err)
			}
			if parsed == 0 {
				return runtimeCfg, fmt.Errorf("%s must be >= 1", TTL_SECONDS_FLAG)
			}
			runtimeCfg.TTLSeconds = parsed
			runtimeCfg.KeyStore.DefaultTTLSeconds = parsed
			continue
		}

		if arg == STORE_PATH_FLAG {
			if i+1 >= len(args) {
				return runtimeCfg, fmt.Errorf("missing value after %q", STORE_PATH_FLAG)
			}
			i++
			runtimeCfg.StoreFilePath = strings.TrimSpace(args[i])
			continue
		}

		if after, ok := strings.CutPrefix(arg, STORE_PATH_FLAG+"="); ok {
			runtimeCfg.StoreFilePath = strings.TrimSpace(after)
			continue
		}

		normalized := strings.ToLower(strings.TrimSpace(arg))
		switch normalized {
		case ModeRun, ModeRemote:
			if modeProvided {
				return runtimeCfg, fmt.Errorf("multiple modes provided: %q", arg)
			}
			runtimeCfg.Mode = normalized
			modeProvided = true
		case string(ActionUpload):
			if actionProvided {
				return runtimeCfg, fmt.Errorf("multiple actions provided: %q", arg)
			}
			runtimeCfg.Action = ActionUpload
			runtimeCfg.ActionProvided = true
			actionProvided = true
		case string(ActionStore):
			if actionProvided {
				return runtimeCfg, fmt.Errorf("multiple actions provided: %q", arg)
			}
			runtimeCfg.Action = ActionStore
			runtimeCfg.ActionProvided = true
			actionProvided = true
		case string(ActionClean):
			if actionProvided {
				return runtimeCfg, fmt.Errorf("multiple actions provided: %q", arg)
			}
			runtimeCfg.Action = ActionClean
			runtimeCfg.ActionProvided = true
			actionProvided = true
		case string(ActionDeepClean), "deepclean", "deep_clean":
			if actionProvided {
				return runtimeCfg, fmt.Errorf("multiple actions provided: %q", arg)
			}
			runtimeCfg.Action = ActionDeepClean
			runtimeCfg.ActionProvided = true
			actionProvided = true
		case string(ActionView):
			if actionProvided {
				return runtimeCfg, fmt.Errorf("multiple actions provided: %q", arg)
			}
			runtimeCfg.Action = ActionView
			runtimeCfg.ActionProvided = true
			actionProvided = true
		case string(ActionStats):
			if actionProvided {
				return runtimeCfg, fmt.Errorf("multiple actions provided: %q", arg)
			}
			runtimeCfg.Action = ActionStats
			runtimeCfg.ActionProvided = true
			actionProvided = true
		case string(ActionVerify):
			if actionProvided {
				return runtimeCfg, fmt.Errorf("multiple actions provided: %q", arg)
			}
			runtimeCfg.Action = ActionVerify
			runtimeCfg.ActionProvided = true
			actionProvided = true
		case string(ActionDelete):
			if actionProvided {
				return runtimeCfg, fmt.Errorf("multiple actions provided: %q", arg)
			}
			runtimeCfg.Action = ActionDelete
			runtimeCfg.ActionProvided = true
			actionProvided = true
		case string(ActionExpire):
			if actionProvided {
				return runtimeCfg, fmt.Errorf("multiple actions provided: %q", arg)
			}
			runtimeCfg.Action = ActionExpire
			runtimeCfg.ActionProvided = true
			actionProvided = true
		case string(ActionStream):
			if actionProvided {
				return runtimeCfg, fmt.Errorf("multiple actions provided: %q", arg)
			}
			runtimeCfg.Action = ActionStream
			runtimeCfg.ActionProvided = true
			actionProvided = true
		default:
			return runtimeCfg, fmt.Errorf("unsupported argument %q", arg)
		}
	}

	if runtimeCfg.TTLSeconds == 0 {
		runtimeCfg.TTLSeconds = runtimeCfg.KeyStore.DefaultTTLSeconds
	}
	runtimeCfg.KeyStore.DefaultTTLSeconds = runtimeCfg.TTLSeconds

	return runtimeCfg, nil
}

func printUsage(indexedFiles []string, cfg RuntimeConfig) {
	sorted := append([]string(nil), indexedFiles...)
	sort.Strings(sorted)

	fmt.Printf("Usage: go run main.go [run|remote] [upload|store|clean|deep-clean|view|stats|verify|delete|expire|stream] [%s] [%s] [%s N] [%s PATH]\n",
		REASSEMBLE_FLAG,
		VERBOSE_FLAG,
		TTL_SECONDS_FLAG,
		STORE_PATH_FLAG,
	)
	fmt.Printf("No mode defaults to %q.\n", cfg.Mode)
	fmt.Printf("No action defaults to %q.\n", cfg.Action)
	fmt.Printf("Reassembly defaults to disabled; enable with %q.\n", REASSEMBLE_FLAG)
	fmt.Printf("Verbose logging defaults to disabled; enable with %q.\n", VERBOSE_FLAG)
	fmt.Printf("Default TTL is %d seconds; override with %q.\n", cfg.TTLSeconds, TTL_SECONDS_FLAG)
	fmt.Printf("Store action accepts a direct path via %q.\n", STORE_PATH_FLAG)
	fmt.Printf("Reassembled copy outputs are written to %s.\n", cfg.KeyStore.StorageDir)
	fmt.Printf("\nUpload action indexes %s and excludes directories + copy.* files.\n", cfg.UploadDirectory)
	fmt.Println("Actions: upload (from upload dir), store (explicit filepath), clean (.kdht only), deep-clean (.kdht + metadata + cache), view (inspect metadata + optional reassemble), stats (storage/system stats), verify (deep integrity scan), delete (remove a single file), expire (sweep TTL-expired files), stream (stream a stored file to disk).")

	if len(sorted) == 0 {
		fmt.Println("\nNo indexable upload files were found.")
		return
	}

	fmt.Println("\nIndexed upload files:")
	for idx, file := range sorted {
		fmt.Printf("    %d: %q\n", idx, file)
	}
}
