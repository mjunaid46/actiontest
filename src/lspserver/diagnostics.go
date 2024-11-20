package lspserver

import (
	"encoding/json"
	"fmt"
	"regexp"
	"github.com/TobiasYin/go-lsp/logs"
)

// See ./prompts/prompt_base.txt
type LspDiagnostic struct {
	Uri            string `json:"uri"`
	LineNumber     int    `json:"line_number"`
	Source         string `json:"source"`
	Rule           string `json:"rule"`
	Severity       string `json:"severity"`
	Description    string `json:"description"`
	Recommendation string `json:"recommendation"`
}

/*
 * DiagnosticToPrettyText takes a LspDiagnostic struct and returns a string with the fields formatted
 * @param d The LspDiagnostic struct to format
 * @return ret The formatted string
*/
func DiagnosticToPrettyText(d LspDiagnostic) string {
	const fmtString string = `
Source: %s
Severity: %s
Recommendation: %s
`
	return fmt.Sprintf(fmtString, d.Source, d.Severity, d.Recommendation)
}

/*
 * DiagnosticToJsonMarkup takes a LspDiagnostic struct and returns a string with the fields formatted as JSON
 * and using markdown markup. Mainly used by the hover callback.
 * @param d The LspDiagnostic struct to format
 * @return ret The formatted string
 * @return error Any error that occurred during marshalling
*/
func DiagnosticToJsonMarkup(d LspDiagnostic) (string, error) {
	
	value, err := json.MarshalIndent(d, "", "  ")
	
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("#### diagnostics\n```json\n%s\n```", string(value)), nil
}

/*
 * DiagnosticsUnmarshal takes a JSON object in a string format and unmarshals it into a slice of LspDiagnostic structs
 * @param analysis The string to unmarshal
 * @return diagnostics A slice of LspDiagnostic structs
 * @return error Any error that occurred during unmarshalling
 */

 func DiagnosticsUnmarshal(uri, analysis string) ([]LspDiagnostic, error) {
	logs.Printf("Analyse Document: %s", analysis)

	// Define a regular expression to find JSON arrays in the input
	re := regexp.MustCompile(`\[\s*\{[^]]+\}\s*\]`)
	matches := re.FindAllString(analysis, -1)

	if len(matches) == 0 {
		return nil, fmt.Errorf("no valid JSON array found")
	}

	var allDiagnostics []LspDiagnostic

	for _, match := range matches {
		var diagnostics []LspDiagnostic
		err := json.Unmarshal([]byte(match), &diagnostics)
		if err != nil {
			logs.Printf("Error unmarshalling: %s", err)
			continue
		}
		allDiagnostics = append(allDiagnostics, diagnostics...)
	}

	for i := range allDiagnostics {
		allDiagnostics[i].Uri = uri
		logs.Printf("Uri: %s, Line Number: %d, Rule: %s, Severity: %s, Description: %s, Recommendation: %s\n",
			allDiagnostics[i].Uri, allDiagnostics[i].LineNumber, allDiagnostics[i].Rule, allDiagnostics[i].Severity, allDiagnostics[i].Description, allDiagnostics[i].Recommendation)
	}

	return allDiagnostics, nil
}
