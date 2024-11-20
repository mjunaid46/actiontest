/*
 * This file contains the LSP server implementation.
 * Author: Zwane Mwaikambo
 */

package lspserver

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"strings"

	"github.com/TobiasYin/go-lsp/logs"
	"github.com/TobiasYin/go-lsp/lsp"
	"github.com/TobiasYin/go-lsp/lsp/defines"
)

// Keep the lsp protocol implementation separate from the rest of the application
type LspServer interface {
	Start() error
	OnInitialized(ctx context.Context, req *defines.InitializeParams) error
	OnDidOpenTextDocument(ctx context.Context, req *defines.DidOpenTextDocumentParams) error
	OnDidChangeTextDocument(ctx context.Context, req *defines.DidChangeTextDocumentParams) error
	OnDidSaveTextDocument(ctx context.Context, req *defines.DidSaveTextDocumentParams) error
	OnHover(ctx context.Context, req *defines.HoverParams) (result *defines.Hover, err error)
	OnDiagnostic(ctx context.Context, req *defines.DocumentDiagnosticParams) (*defines.FullDocumentDiagnosticReport, error)
	OnCompletion(ctx context.Context, req *defines.CompletionParams) (result *[]defines.CompletionItem, err error)
}

type lspServer struct {
	name      string
	server    *lsp.Server
	backend   LspBackend
	documents LspDocuments
}

func NewLspServer(name string) LspServer {
	return &lspServer{
		name: name,
	}
}

func (l *lspServer) Start() error {
	logs.Printf("LspServer starting...")

	switch *ParamBackend {
	case "openai":
		l.backend = NewOpenAiBackend()
	case "ollama":
		l.backend = NewOllamaBackend()
	default:
		logs.Printf("Invalid backend: %s", *ParamBackend)
		os.Exit(1)
	}

	l.documents = NewLspDocuments()
	logs.Printf("[+] New LSP Document [ %s ] ", l.documents)
	return l.backend.Start()
}

/*
 * OnInitialized is called when the client is ready to receive requests.
 * At this point the client has sent the initialize request and received the
 * response and capabilities of the server.
 *
 * @param ctx The context of the request.
 * @param req The initialize params.
 * @return error Any error that occurred during the request
 */
func (l *lspServer) OnInitialized(ctx context.Context, req *defines.InitializeParams) error {
	logs.Printf("OnInitialized: %s", req)
	l.server.OnDidOpenTextDocument(l.OnDidOpenTextDocument)
	return nil
}

/*
 * updateDocumentStore is helper for updating internal state whenever the document is opened
 * or saved by the client.
 *
 * @param ctx The context of the request.
 * @param req The open text document params.
 * @return error Any error that occurred during the request
 */

func (l *lspServer) updateDocumentStore(uri string, text string) error {
	var analysis string
	var diagnostics []LspDiagnostic
	logs.Printf("=> URI: [%s] TEXT: [%s]", uri, text)
	err := l.documents.Store(uri, text)
	if err != nil {
		// This is ok, the document may already be stored
		return nil
	}

	const maxRetries = 5
	instruction := ""

	for attempts := 1; attempts <= maxRetries; attempts++ {
		analysis, err = l.backend.AnalyseDocument(uri, instruction+text)
		if err != nil {
			return err
		}
		err = l.documents.StoreAnalysis(uri, analysis)
		if err != nil {
			return err
		}
		diagnostics, err = DiagnosticsUnmarshal(uri, analysis)
		if err != nil {
			if attempts < maxRetries {
				logs.Printf("AnalyseDocument attempt %d/%d failed: %v. Retrying...", attempts, maxRetries, err)
			} else {
				logs.Printf("AnalyseDocument attempt %d/%d failed: %v. No more retries.", attempts, maxRetries, err)
				return err
			}
			var temp []byte
			temp, err = LoadPrompt(*ParamRetryPromptFile)
			instruction = string(temp)
		} else {
			break
		}
	}

	if err != nil {
		logs.Printf("Failed to analyze document after %d attempts: %v\n", maxRetries, err)
		return err
	}

	err = l.documents.UpdateDiagnostics(uri, diagnostics)
	if err != nil {
		logs.Printf("Failed to update diagnostics: %v\n", err)
		return err
	}

	logs.Printf("Diagnostics successfully updated for URI: %s", uri)
	return nil
}

/*
 * OnDidOpenTextDocument is called when a text document is opened in a client.
 *
 * @param ctx The context of the request.
 * @param req The open text document params from the client.
 * @return error Any error that occurred during the request
 */

func (l *lspServer) OnDidOpenTextDocument(ctx context.Context, req *defines.DidOpenTextDocumentParams) error {
	logs.Printf("OnDidOpenTextDocument:\n%s", req)

	return l.updateDocumentStore(string(req.TextDocument.Uri), req.TextDocument.Text)
}

// ConvertFileURIToPath converts a file URI to a system-specific file path
func ConvertFileURIToPath(uri string) (string, error) {
	parsedURI, err := url.Parse(uri)
	if err != nil {
		return "", err
	}

	// Replace 'file:///' with ''
	path := strings.Replace(parsedURI.Path, "file:///", "", 1)

	// Handle Windows file paths (e.g., /C:/Users/... to C:/Users/...)
	if strings.HasPrefix(path, "/") && strings.Contains(path, ":") {
		path = path[1:]
	}

	return path, nil
}

func ReadFileContent(filePath string) (string, error) {
	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func (l *lspServer) OnDidChangeTextDocument(ctx context.Context, req *defines.DidChangeTextDocumentParams) error {
	uri := req.TextDocument.TextDocumentIdentifier.Uri

	logs.Printf("[+] OnDidChangeTextDocument: %s", string(uri))

	filePath, err := ConvertFileURIToPath(string(uri))

	if err != nil {
		logs.Printf("Error converting URI to file path: %v\n", err)
		return err
	}
	// var content string
	// var err error
	documentContent, err := ReadFileContent(filePath)
	// Fetch the latest content of the document
	// content, err := os.ReadFile(string(uri))
	if err != nil {
		logs.Printf("Error loading document content: %s", err)
		return err
	}

	// Analyze the document content
	analysis, err := l.backend.AnalyseDocument(string(uri), string(documentContent))
	if err != nil {
		logs.Printf("Error analyzing document: %s", err)
		return err
	}

	// Update the analysis in the document store
	if err = l.documents.StoreAnalysis(string(uri), analysis); err != nil {
		logs.Printf("Error storing document analysis: %s", err)
		return err
	}

	// Unmarshal diagnostics from the analysis
	diagnostics, err := DiagnosticsUnmarshal(string(uri), analysis)
	if err != nil {
		logs.Printf("Error unmarshalling diagnostics: %s", err)
		return err
	}

	err = l.documents.UpdateDiagnostics(string(uri), diagnostics)

	// Update diagnostics in the document store
	if err != nil {
		logs.Printf("Error updating diagnostics: %s", err)
		return err
	}
	// l.documents.UpdateDiagnostics(uri, diagnostics)
	// Send diagnostics to the client
	l.updateDocumentStore(string(uri), string(documentContent))

	return nil
}

func (l *lspServer) OnDidSaveTextDocument(ctx context.Context, req *defines.DidSaveTextDocumentParams) error {

	logs.Printf("OnDidSaveTextDocument:\n%s", req)

	logs.Printf("URI: %s | Text: %s ", string(req.TextDocument.Uri), req.Text)
	filePath, err := ConvertFileURIToPath(string(req.TextDocument.Uri))

	if err != nil {
		logs.Printf("Error converting URI to file path: %v\n", err)
		return err
	}
	// var content string
	// var err error
	documentContent, err := ReadFileContent(filePath)
	if err != nil {
		logs.Printf("Error loading document content: %s", err)
		return err
	}
	// TODO: Add IncludeText to server capabilities
	if documentContent != "" {
		return l.updateDocumentStore(string(req.TextDocument.Uri), documentContent)
	}

	return nil
}

/*
 * OnDiagnostic is called when a text document is opened in a client.
 * The client will send a notification to the server requesting diagnostics (Pull Diagnostics)
 *
 * @param ctx The context of the request.
 * @param req The diagnostic document param from the client.
 * @return report The full diagnostic report
 * @return error Any error that occurred during the request
 */

func (l *lspServer) OnDiagnostic(ctx context.Context, req *defines.DocumentDiagnosticParams) (*defines.FullDocumentDiagnosticReport, error) {
	logs.Printf("OnDiagnostic called for URI: %s", req.TextDocument.Uri)

	diagnostics := []defines.Diagnostic{}
	report := defines.FullDocumentDiagnosticReport{}

	docDiagnostics, err := l.documents.GetDiagnostics(string(req.TextDocument.Uri))
	if err != nil {
		logs.Printf("Error getting diagnostics for URI %s: %v\n", req.TextDocument.Uri, err)
		return &report, nil
	}

	for _, d := range docDiagnostics {
		var diagnostic defines.Diagnostic
		var severity defines.DiagnosticSeverity
		message := DiagnosticToPrettyText(d)

		switch d.Severity {
		case "advisory":
			severity = defines.DiagnosticSeverityWarning
		case "mandatory":
			severity = defines.DiagnosticSeverityError
		default:
			severity = defines.DiagnosticSeverityHint
		}

		diagRange := defines.Range{
			Start: defines.Position{Line: uint(d.LineNumber - 1), Character: 0},
			End:   defines.Position{Line: uint(d.LineNumber - 1), Character: 5},
		}

		relatedInfo := []defines.DiagnosticRelatedInformation{
			{
				Location: defines.Location{
					Uri:   req.TextDocument.Uri,
					Range: diagRange,
				},
				Message: message,
			},
		}

		searchUrl := fmt.Sprintf("https://bing.com/search?=\"%s\"", d.Source)
		diagnostic = defines.Diagnostic{
			Range:              diagRange,
			Severity:           &severity,
			Code:               d.Source + " " + d.Rule,
			Source:             &l.name,
			Message:            d.Description,
			CodeDescription:    &defines.CodeDescription{Href: defines.URI(searchUrl)},
			RelatedInformation: &relatedInfo,
		}

		diagnostics = append(diagnostics, diagnostic)
	}

	var items []interface{}
	for _, d := range diagnostics {
		items = append(items, d)
	}
	report = defines.FullDocumentDiagnosticReport{
		Kind:  defines.DocumentDiagnosticReportKindFull,
		Items: items,
	}

	logs.Printf("Diagnostics report created with %d items for URI %s\n", len(items), req.TextDocument.Uri)
	return &report, nil
}

/*
 * OnHover is called when a user hovers over a token in the editor. This method is then sent to the server
 * which will return a Hover object to the client.
 *
 * @param ctx The context of the request.
 * @param req The hover params from the client.
 * @return The hover object sent to the client
 * @return error Any error that occurred during the request
 */

func (l *lspServer) OnHover(ctx context.Context, req *defines.HoverParams) (result *defines.Hover, err error) {
	logs.Printf("OnHover: %s", req)
	var value string

	diagnostics, err := l.documents.GetDiagnostics(string(req.TextDocument.Uri))
	if err != nil {
		return nil, err
	}

	for _, d := range diagnostics {
		if d.LineNumber-1 != int(req.Position.Line) {
			continue
		}

		value, err = DiagnosticToJsonMarkup(d)
		if err != nil {
			break
		}
	}

	return &defines.Hover{
		Contents: defines.MarkupContent{
			Kind:  defines.MarkupKindMarkdown,
			Value: value,
		},
	}, nil
}

func strPtr(str string) *string {
	return &str
}

func kindPtr(kind defines.CompletionItemKind) *defines.CompletionItemKind {
	return &kind
}

func (l *lspServer) OnCompletion(ctx context.Context, req *defines.CompletionParams) (result *[]defines.CompletionItem, err error) {
	logs.Printf("OnCompletion: %v", req)

	// Define the system prompt for code completion
	systemPrompt := "You are a coding assistant. Provide the best possible code completions based on the given context."

	// Fetch the document content
	filePath, err := ConvertFileURIToPath(string(req.TextDocument.Uri))
	if err != nil {
		logs.Printf("Error converting URI to file path: %v\n", err)
		return nil, err
	}

	documentContent, err := ReadFileContent(filePath)
	if err != nil {
		logs.Printf("Error reading file content: %v\n", err)
		return nil, err
	}

	if documentContent == "" {
		return nil, fmt.Errorf("failed to retrieve document content")
	}

	// Determine the position in the document
	line := int(req.Position.Line)
	character := int(req.Position.Character)

	// Extract the previous 3 lines as the prefix
	startLine := line - 3
	if startLine < 0 {
		startLine = 0
	}
	lines := strings.Split(documentContent, "\n")

	// Ensure the line number is within the valid range
	if line >= len(lines) {
		return nil, fmt.Errorf("line number out of range")
	}

	// Get the prefix lines
	prefixLines := lines[startLine:line]
	prefix := strings.Join(prefixLines, "\n")

	// Extract the prefix up to the current character on the current line
	if character > 0 && character <= len(lines[line]) {
		currentLinePrefix := lines[line][:character]
		prefix = prefix + "\n" + currentLinePrefix
	}

	// Call the backend to get completions with the custom system prompt
	completions, err := l.backend.CompleteCode(string(req.TextDocument.Uri), prefix, systemPrompt)
	if err != nil {
		return nil, err
	}

	// Map completions to CompletionItems
	var completionItems []defines.CompletionItem
	for _, comp := range completions {
		completionItems = append(completionItems, defines.CompletionItem{
			Label:      comp,
			Kind:       kindPtr(defines.CompletionItemKindText),
			InsertText: strPtr(comp),
		})
	}

	return &completionItems, nil
}


// func (l *lspServer) OnCompletion(ctx context.Context, req *defines.CompletionParams) (result *[]defines.CompletionItem, err error) {
//     logs.Printf("OnCompletion: %v", req)

//     // Fetch the current document content
//     // documentContent, err := l.documents.GetContent(string(req.TextDocument.Uri))
//     // if err != nil {
//     //     return nil, err
//     // }

//     // Analyze the document or context to generate completion items
//     completionItems := []defines.CompletionItem{
//         {
//             Label:      "exampleFunction()",
//             Kind:       kindPtr(defines.CompletionItemKindFunction),
//             InsertText: strPtr("exampleFunction()"),
//         },
//         {
//             Label:      "exampleVariable",
//             Kind:       kindPtr(defines.CompletionItemKindVariable),
//             InsertText: strPtr("exampleVariable"),
//         },
//     }

//     // Return the completion items
//     return &completionItems, nil
// }

func Serve(name string) {
	lspserver := lspServer{name: name}
	lspserver.server = lsp.NewServer(&lsp.Options{CompletionProvider: &defines.CompletionOptions{
		TriggerCharacters: &[]string{"."},
	}})

	if lspserver.server == nil {
		panic("Error creating LspServer")
	}

	err := lspserver.Start()
	if err != nil {
		logs.Printf("start failed: %v", err)
		os.Exit(1)
		// TODO: handle retrying
	}

	lspserver.server.OnInitialized(lspserver.OnInitialized)
	lspserver.server.OnDidOpenTextDocument(lspserver.OnDidOpenTextDocument)
	lspserver.server.OnDidChangeTextDocument(lspserver.OnDidChangeTextDocument)
	lspserver.server.OnDidSaveTextDocument(lspserver.OnDidSaveTextDocument)
	lspserver.server.OnHover(lspserver.OnHover)
	lspserver.server.OnDiagnostic(lspserver.OnDiagnostic)
	lspserver.server.OnCompletion(lspserver.OnCompletion)
	lspserver.server.Run()
}
