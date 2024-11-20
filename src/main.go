package main

import (
	"flag"
	"fmt"
	"log"
	"github.com/TobiasYin/go-lsp/logs"
	"lspserver/lspserver"
	"os"
	"encoding/json"
	"io"
	"path/filepath"
)

var AppName = "lsp-server"
var version = "unknown"

type Config struct {
    Stdio       bool   `json:"stdio"`
    Version     bool   `json:"version"`
    PromptFile  string `json:"prompt_file"`
    Backend     string `json:"backend"`
    ConnectTest bool   `json:"connect_test"`
	RetryPrompt string `json:"retry_prompt"`
}

func readConfigFile(filePath string) (*Config, error) {
    configFile, err := os.Open(filePath)
    if err != nil {
        return nil, fmt.Errorf("error opening config file: %w", err)
    }
    defer configFile.Close()

    byteValue, err := io.ReadAll(configFile)
    if err != nil {
        return nil, fmt.Errorf("error reading config file: %w", err)
    }

    var config Config
    err = json.Unmarshal(byteValue, &config)
    if err != nil {
        return nil, fmt.Errorf("error unmarshalling config file: %w", err)
    }

    return &config, nil
}

func init() {
	var logger *log.Logger
	var logPath *string
	var checkVersion *bool

	defer func() {
		logs.Init(logger)
	}()

	// Determine the directory of the executable
    exePath, err := os.Executable()
    if err != nil {
        logs.Printf("Error determining executable path: %v", err)
    }
    exeDir := filepath.Dir(exePath)

    // Construct the path to the configuration file
    configFilePath := filepath.Join(exeDir, "server_config.json")

    // Read the configuration file
    config, err := readConfigFile(configFilePath)
    if err != nil {
        logs.Printf("Error reading config file: %v", err)
    }

    _ = flag.Bool("stdio", config.Stdio, "Use stdio for LSP communication")
    checkVersion = flag.Bool("version", config.Version, "Print version and exit")
    lspserver.ParamPromptFile = flag.String("prompt-file", config.PromptFile, "prompt file path")
    lspserver.ParamBackend = flag.String("backend", config.Backend, "backend to use (openai)")
    lspserver.ParamConnectTest = flag.Bool("connect-test", config.ConnectTest, "test connection to backend")
	lspserver.ParamRetryPromptFile = flag.String("retry-prompt", config.RetryPrompt, "Retry Prompt File")
	
	flag.Parse()

	if *lspserver.ParamBackend != "ollama" && *lspserver.ParamBackend != "openai" {
		fmt.Println("valid backends: ollama, openai")
		os.Exit(1)
	}

	if *checkVersion {
		fmt.Printf("%s (build %s)\n", AppName, version)
		os.Exit(0)
	}

	logPath = flag.String("logs", "", "logs file path")
	if logPath == nil || *logPath == "" {
		logger = log.New(os.Stderr, "", 0)
		return
	}

	p := *logPath
	f, err := os.Open(p)
	if err == nil {
		logger = log.New(f, "", 0)
		return
	}

	f, err = os.Create(p)
	if err == nil {
		logger = log.New(f, "", 0)
		return
	}

	panic(fmt.Sprintf("logging init error: %v", *logPath))
}

func main() {
	logs.Printf("%s (build %s)\n", AppName, version)
	lspserver.Serve(AppName)
}
