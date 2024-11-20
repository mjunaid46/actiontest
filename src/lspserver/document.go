package lspserver

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/TobiasYin/go-lsp/logs"
)

type LspDocuments interface {
	Load(uri string) (string, error)
	Store(uri string, data string) error
	Delete(uri string) error
	Dump() map[string]string
	LoadAnalysis(uri string) (string, error)
	StoreAnalysis(uri string, analysis string) error
	UpdateDiagnostics(uri string, diagnostics []LspDiagnostic) error
	GetDiagnostics(uri string) ([]LspDiagnostic, error)
}

type lspDocuments struct {
	data        map[string]string
	data_hash   map[string][sha256.Size]byte
	analysis    map[string]string
	diagnostics map[string][]LspDiagnostic
}

func NewLspDocuments() LspDocuments {
	return &lspDocuments{
		data:        make(map[string]string),
		data_hash:   make(map[string][sha256.Size]byte),
		analysis:    make(map[string]string),
		diagnostics: make(map[string][]LspDiagnostic),
	}
}

func (d *lspDocuments) Load(uri string) (string, error) {
	logs.Printf("[+] Loading Document....")
	if d.data[uri] == "" {
		s := fmt.Sprintf("document (%s) not found", uri)
		return "", errors.New(s)
	}
	return d.data[uri], nil
}

func (d *lspDocuments) Store(uri string, data string) error {
	logs.Printf("[+] Storing Document....")
	hash := sha256.Sum256([]byte(data))
	if d.data_hash[uri] == hash {
		return errors.New("document already stored")
	}

	d.data[uri] = data
	d.data_hash[uri] = hash
	return nil
}

func (d *lspDocuments) Delete(uri string) error {
	logs.Printf("[+] Clearing content")
	delete(d.data, uri)
	delete(d.data_hash, uri)
	return nil
}

func (d *lspDocuments) Dump() map[string]string {
	logs.Printf("[+] Dumping data")
	return d.data
}

func (d *lspDocuments) StoreAnalysis(uri string, analysis string) error {
	logs.Printf("[+] Storing Analysis")
	d.analysis[uri] = analysis
	return nil
}

func (d *lspDocuments) LoadAnalysis(uri string) (string, error) {
	logs.Printf("[+] Loading Analysis....")
	if d.analysis[uri] == "" {
		s := fmt.Sprintf("diagnostics (%s) not found", uri)
		return "", errors.New(s)
	}
	return d.analysis[uri], nil
}

func (d *lspDocuments) GetDiagnostics(uri string) ([]LspDiagnostic, error) {
	logs.Printf("[+] GetDiagnostics....")
	if d.diagnostics[uri] == nil {
		s := fmt.Sprintf("diagnostics (%s) not found", uri)
		return nil, errors.New(s)
	}

	return d.diagnostics[uri], nil
}

func (d *lspDocuments) UpdateDiagnostics(uri string, diagnostics []LspDiagnostic) error {
    logs.Printf("[+] UpdateDiagnostics for URI: %s with %d diagnostics\n", uri, len(diagnostics))
    for _, diag := range diagnostics {
        logs.Printf("Diagnostic: Line %d, Message: %s, Severity: %s", diag.LineNumber, diag.Description, diag.Severity)
    }
    d.diagnostics[uri] = diagnostics
    return nil
}
