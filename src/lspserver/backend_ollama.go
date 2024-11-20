package lspserver

import (
	"context"
	"fmt"
	"math"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/TobiasYin/go-lsp/logs"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/schema"
)

/* backend specific private data */
type lspBackendOllama struct {
	mutex            sync.Mutex
	client           *ollama.Chat
	connected        bool
	modelName        string
	modelSeed        int
	modelMaxTokens   int
	modelTemperature float64
	systemPromptFile string
	systemPrompt     string
	cancel           context.CancelFunc
}

func NewOllamaBackend() LspBackend {
	return &lspBackendOllama{
		mutex:            sync.Mutex{},
		connected:        false,
		modelName:        "deepseek-coder",
		modelMaxTokens:   4096,
		modelTemperature: math.SmallestNonzeroFloat64,
		modelSeed:        42,
	}
}

func (b *lspBackendOllama) Start() error {
	logs.Printf("Ollama LSP Backend starting...")

	err := b.connect()
	if err != nil {
		return err
	}
	logs.Printf("[+] Ollama Model Connected Successfully")
	b.connected = true
	return nil
}

func (b *lspBackendOllama) connect() error {
	var err error
	var systemPrompt []byte

	b.client, err = ollama.NewChat(ollama.WithLLMOptions(ollama.WithModel(b.modelName)))
	logs.Printf("Ollama New Chat....\n")
	if err != nil {
		return err
	}

	systemPrompt, err = LoadPrompt(*ParamPromptFile)
	logs.Printf("Prompts Loaded....\n%s", systemPrompt)
	if err != nil {
		return err
	}

	b.systemPromptFile = *ParamPromptFile
	b.systemPrompt = string(systemPrompt)

	return nil
}

func (b *lspBackendOllama) request(ctx context.Context, query string) (string, error) {
	logs.Printf("System Prompt: %s\nQuery: %s\n", b.systemPrompt, query)
	completion, err := b.client.Call(ctx, []schema.ChatMessage{
		schema.SystemChatMessage{Content: b.systemPrompt},
		schema.HumanChatMessage{Content: query},
	},
		llms.WithTemperature(b.modelTemperature),
		llms.WithModel(b.modelName),
		llms.WithMaxTokens(b.modelMaxTokens),
		llms.WithSeed(b.modelSeed),
	)

	if err != nil {
		return "", err
	}

	logs.Printf(completion.Content)
	return completion.Content, nil
}

// preprocessDocument splits the document into chunks of 50 lines each with correct line numbers
func preprocessDocument(document string) []string {
	var lines []string
	retryPrompt, err := os.ReadFile(*ParamRetryPromptFile)
	if err != nil {
		logs.Printf("Unable to read the retry prompt file")
	}

	// Check if the document starts with the retry prompt
	if strings.HasPrefix(document, string(retryPrompt)) {
		// Remove the retry prompt from the start of the document
		document = strings.TrimPrefix(document, string(retryPrompt))
	}

	// Determine the newline character based on the OS
	switch runtime.GOOS {
	case "windows":
		lines = strings.Split(document, "\r\n")
	case "darwin":
		lines = strings.Split(document, "\n")
	default:
		lines = strings.Split(document, "\n")
	}

	chunkSize := 30
	var chunks []string
	for i := 0; i < len(lines); i += chunkSize {
		end := i + chunkSize
		if end > len(lines) {
			end = len(lines)
		}
		var chunk strings.Builder
		for j := i; j < end; j++ {
			chunk.WriteString(fmt.Sprintf("Line %d: %s\n", j+1, lines[j]))
		}
		chunks = append(chunks, chunk.String())
	}

	return chunks
}

func (b *lspBackendOllama) AnalyseDocument(uri string, document string) (string, error) {
	logs.Printf("Analyse Document: %s\n%s", uri, document)

	b.mutex.Lock()
	defer b.mutex.Unlock()

	// Cancel any previous request
	if b.cancel != nil {
		b.cancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel

	logs.Printf("Document Input: %s", document)

	chunks := preprocessDocument(document)
	logs.Printf("Preprocessed Document into %d chunks", len(chunks))

	var err error
	PrevMod := b.modelName
	if strings.HasSuffix(uri, ".py") {
		b.modelName = "deepseek-coder"
	} else {
		b.modelName = "deepseek-coder"
	}

	if PrevMod != b.modelName {
		b.client, err = ollama.NewChat(ollama.WithLLMOptions(ollama.WithModel(b.modelName)))
		logs.Printf("Ollama New Chat....\n")
		if err != nil {
			return "", err
		}
	}

	var responseBuilder strings.Builder
	for i, chunk := range chunks {
		query := fmt.Sprintf("FileName: %s\nSource Code (Chunk %d):\n%s", uri, i+1, chunk)
		response, err := b.request(ctx, query)
		if err != nil {
			return "", err
		}
		responseBuilder.WriteString(response)
		responseBuilder.WriteString("\n")
		logs.Printf("[+] Response for chunk %d: %s", i+1, response)
	}

	return responseBuilder.String(), nil
}
// Implement CompleteCode method for code completion with custom systemPrompt
func (b *lspBackendOllama) CompleteCode(uri string, prefix string, systemPrompt string) ([]string, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	// Cancel any previous request
	if b.cancel != nil {
		b.cancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel

	query := fmt.Sprintf("Complete the code following this prefix:\n%s<PROVIDE_SUGGESTION_HERE>", prefix)
	response, err := b.requestWithPrompt(ctx, query, systemPrompt) // Use custom prompt
	if err != nil {
		return nil, err
	}

	// Split the response into possible completions
	completions := strings.Split(response, "\n")
	return completions, nil
}

// Updated request method to allow custom system prompts
func (b *lspBackendOllama) requestWithPrompt(ctx context.Context, query string, systemPrompt string) (string, error) {
	logs.Printf("Completion System Prompt: %s\nQuery: %s\n", systemPrompt, query)
	completion, err := b.client.Call(ctx, []schema.ChatMessage{
		schema.SystemChatMessage{Content: systemPrompt},
		schema.HumanChatMessage{Content: query},
	},
		llms.WithTemperature(b.modelTemperature),
		llms.WithModel(b.modelName),
		llms.WithMaxTokens(b.modelMaxTokens),
		llms.WithSeed(b.modelSeed),
	)

	if err != nil {
		return "", err
	}

	logs.Printf(completion.Content)
	return completion.Content, nil
}
