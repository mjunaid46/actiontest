package lspserver

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"

	"github.com/TobiasYin/go-lsp/logs"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
	"github.com/tmc/langchaingo/schema"
)

/* backend specific private data */
type lspBackendOpenAi struct {
	mutex            sync.Mutex
	client           *openai.Chat
	connected        bool
	modelName        string
	modelSeed        int
	modelMaxTokens   int
	modelTemperature float64
	systemPromptFile string
	systemPrompt     string
}

var misraRules = []string{
	"Code MUST follow MISRA C Coding Guidelines.",
	"Use 4 spaces for indentation; do not use tabs.",
	"Aim for a maximum line length of 76 columns.",
	"Place the `*` directly next to the variable name for pointers (e.g., `int *ptr`).",
	"Align variable names where possible and match the style of surrounding code.",
	"Enclose the statement forming the body of control structures (`if`, `else if`, `else`, `while`, `do ... while`, `for`) in braces.",
	"An `if (expression)` construct must be followed by a compound statement; `else` must be followed by a compound statement or another `if` statement.",
	"Terminate all `if ... else if` constructs with an `else` clause.",
	"A pointer resulting from arithmetic on a pointer operand must address an element of the same array as that pointer operand.",
	"Do not use the `sizeof` operator on function parameters declared as \"array of type\".",
	"Do not use the Standard Library function `system` from `<stdlib.h>`.",
	"Follow alignment (`<stdalign.h>`) and no-return functions (`<stdnoreturn.h>`) rules.",
	"Do not use type generic expressions (`_Generic`).",
	"Avoid using obsolescent language features.",
	"Declare all variables at the beginning of a block.",
	"Avoid using global variables; prefer static variables.",
	"Use only approved control structures; avoid `goto` statements.",
	"Ensure all loops have a fixed upper limit.",
	"Keep functions short and focused on a single task.",
	"Use function prototypes and limit the number of parameters.",
	"Use only standard MISRA-compliant data types.",
	"Avoid dynamic memory allocation (`malloc`, `calloc`, `free`).",
	"Use consistent comment styles:\n  - Single-line: `/* Comment */`\n  - Multi-line:\n    ```\n    /*\n     * Multi-line comment\n     * continues here.\n     */\n    ```",
	"Describe the intent, not the action; use full sentences, correct grammar, and spelling. Avoid non-obvious abbreviations.",
	"Use K&R style for bracing; always brace even single-line statements.",
	"Use a single exit point in functions, using `goto` for error handling.",
	"Wrap non-trivial macros in `do {...} while (0)`.",
	"Avoid magic numbers; use enumerations or constants.",
	"Define bitfield widths for `BOOL`, enums, and flags to ensure proper alignment.",
}

func NewOpenAiBackend() LspBackend {
	return &lspBackendOpenAi{
		mutex:            sync.Mutex{},
		connected:        false,
		modelName:        "gpt-4-1106-preview",
		modelMaxTokens:   4096,
		modelTemperature: math.SmallestNonzeroFloat64,
		modelSeed:        42,
	}
}

func (b *lspBackendOpenAi) Start() error {
	logs.Printf("OpenAI LSP Backend starting...")

	err := b.connect()
	if err != nil {
		return err
	}

	b.connected = true
	return nil
}

func (b *lspBackendOpenAi) connect() error {
	var err error
	var systemPrompt []byte

	if os.Getenv("OPENAI_API_KEY") == "" {
		return errors.New("OPENAI_API_KEY not set")
	}

	b.client, err = openai.NewChat(openai.WithModel(b.modelName))
	if err != nil {
		return err
	}

	systemPrompt, err = LoadPrompt(*ParamPromptFile)
	if err != nil {
		return err
	}

	b.systemPromptFile = *ParamPromptFile
	b.systemPrompt = string(systemPrompt)
	if *ParamConnectTest {
		response, err := b.request("int main() { return 0; }", "")
		if err != nil {
			return err
		}
		logs.Printf("%s", response)
	}
	return nil
}

func (b *lspBackendOpenAi) request(query string, rule string) (string, error) {
	ctx := context.Background()

	prompt := fmt.Sprintf("%s\nRule: %s", b.systemPrompt, rule)

	completion, err := b.client.Call(ctx, []schema.ChatMessage{
		schema.SystemChatMessage{Content: prompt},
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

// preprocessDocument2 splits the document into chunks of 30 lines each with correct line numbers
func preprocessDocument2(document string) []string {
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

	// Split the document into lines
	lines = strings.Split(document, "\n")

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

func (b *lspBackendOpenAi) AnalyseDocument(uri string, document string) (string, error) {
	logs.Printf("AnalyseDocument: %s", document)

	b.mutex.Lock()
	defer b.mutex.Unlock()

	logs.Printf("Document Input: %s", document)

	chunks := preprocessDocument2(document)
	logs.Printf("Preprocessed Document into %d chunks", len(chunks))

	var responseBuilder strings.Builder
	for _, rule := range misraRules {
		for i, chunk := range chunks {
			query := fmt.Sprintf("FileName: %s\nSource Code (Chunk %d):\n%s", uri, i+1, chunk)
			response, err := b.request(query, rule)
			if err != nil {
				return "", err
			}
			responseBuilder.WriteString(response)
			responseBuilder.WriteString("\n")
			logs.Printf("[+] Response for chunk %d with rule %s: %s", i+1, rule, response)
		}
	}

	return responseBuilder.String(), nil
}

// OnCompletion processes the completion request
func (b *lspBackendOpenAi) CompleteCode(uri string, query string, systemPrompt string) ([]string, error) {
	logs.Printf("OnCompletion: %s", query)

	b.mutex.Lock()
	defer b.mutex.Unlock()

	response, err := b.requestWithPrompt(query, systemPrompt)
	if err != nil {
		return nil, err
	}

	logs.Printf("[+] Completion Response: %s", response)
	completions := strings.Split(response, "\n")
	return completions, nil
}

func (b *lspBackendOpenAi) requestWithPrompt(query string, systemPrompt string) (string, error) {
	ctx := context.Background()
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