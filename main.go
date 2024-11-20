package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/schema"
)

// Config holds the configuration for the LLM model
type Config struct {
	ModelName        string  `json:"model_name"`
	ModelMaxTokens   int     `json:"model_max_tokens"`
	ModelTemperature float64 `json:"model_temperature"`
	PromptFile       string  `json:"prompt_file"`
}

// ReportEntry holds the structure of each report entry
type ReportEntry struct {
	URI            string `json:"uri"`
	LineNumber     int    `json:"line_number"`
	LineContent    string `json:"line_content"`
	Source         string `json:"source"`
	Rule           string `json:"rule"`
	Severity       string `json:"severity"`
	Description    string `json:"description"`
	Recommendation string `json:"recommendation"`
}

// LoadConfig loads the config from the config.json file
func LoadConfig(path string) (*Config, error) {
	configFile, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("unable to read config file: %v", err)
	}

	var config Config
	err = json.Unmarshal(configFile, &config)
	if err != nil {
		return nil, fmt.Errorf("unable to parse config file: %v", err)
	}

	return &config, nil
}

// LoadPrompt loads the system prompt from a file
func LoadPrompt(path string) (string, error) {
	prompt, err := ioutil.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("unable to read prompt file: %v", err)
	}
	return string(prompt), nil
}

// Request sends the code to the LLM for analysis
func Request(ctx context.Context, client *ollama.Chat, systemPrompt, code string, config *Config) (string, error) {
	completion, err := client.Call(ctx, []schema.ChatMessage{
		schema.SystemChatMessage{Content: systemPrompt},
		schema.HumanChatMessage{Content: code},
	},
		llms.WithTemperature(config.ModelTemperature),
		llms.WithModel(config.ModelName),
		llms.WithMaxTokens(config.ModelMaxTokens),
	)

	if err != nil {
		return "", err
	}

	return completion.Content, nil
}

// GetDiffs extracts the diff of the repository from the last push
func GetDiffs() (string, error) {
	cmd := exec.Command("git", "diff", "HEAD~1")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("error running git diff: %v", err)
	}
	return string(output), nil
}

func main() {
	// Define and parse flags
	method := flag.String("method", "full", "Analysis method: full or diff")
	flag.Parse()

	// Determine the directory of the binary
	binaryDir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		log.Fatalf("Unable to determine binary directory: %v", err)
	}

	// Load config.json from the binary directory
	configPath := filepath.Join(binaryDir, "workflow-config.json")
	config, err := LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	// Load the system prompt from the specified prompt file
	promptPath := filepath.Join(binaryDir, config.PromptFile)
	systemPrompt, err := LoadPrompt(promptPath)
	if err != nil {
		log.Fatalf("Error loading prompt: %v", err)
	}

	fmt.Printf("System Prompt: %s\n", systemPrompt)
	// Create a new Ollama LLM client
	client, err := ollama.NewChat(ollama.WithLLMOptions(ollama.WithModel(config.ModelName)))
	if err != nil {
		log.Fatalf("Failed to create LLM client: %v", err)
	}

	// Initialize the report
	report := make(map[string][]ReportEntry)

	// Handle full or diff analysis
	if *method == "full" {
		// Get all files in the repository
		files, err := filepath.Glob("*.c") // Adjust the pattern as needed for your project
		if err != nil {
			log.Fatalf("Error listing files: %v", err)
		}

		// Analyze each file
		for _, file := range files {
			code, err := ioutil.ReadFile(file)
			if err != nil {
				log.Fatalf("Error reading file %s: %v", file, err)
			}

			// Send the code to the LLM for analysis
			ctx := context.Background()
			response, err := Request(ctx, client, systemPrompt, string(code), config)
			if err != nil {
				log.Fatalf("Error analyzing code: %v", err)
			}

			var entries []ReportEntry
			if err := json.Unmarshal([]byte(response), &entries); err != nil {
				log.Fatalf("Error parsing LLM response: %v", err)
			}

			report[file] = entries
		}
	} else if *method == "diff" {
		// Get the diffs from the repository
		diffs, err := GetDiffs()
		if err != nil {
			log.Fatalf("Error getting diffs: %v", err)
		}

		// Analyze the diffs
		ctx := context.Background()
		response, err := Request(ctx, client, systemPrompt, diffs, config)
		if err != nil {
			log.Fatalf("Error analyzing diffs: %v", err)
		}

		var entries []ReportEntry
		if err := json.Unmarshal([]byte(response), &entries); err != nil {
			log.Fatalf("Error parsing LLM response: %v", err)
		}

		report["diff"] = entries
	} else {
		log.Fatalf("Invalid method: %s", *method)
	}

	// Write the report to report.json
	reportFilePath := filepath.Join(binaryDir, "report.json")
	reportFile, err := os.Create(reportFilePath)
	if err != nil {
		log.Fatalf("Error creating report file: %v", err)
	}
	defer reportFile.Close()

	reportData, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		log.Fatalf("Error marshaling report: %v", err)
	}

	_, err = reportFile.Write(reportData)
	if err != nil {
		log.Fatalf("Error writing report to file: %v", err)
	}

	fmt.Printf("Analysis report saved to %s\n", reportFilePath)

	// Generate HTML report
	htmlTemplate := `
	<!DOCTYPE html>
	<html>
	<head>
		<title>Static Code Analysis Report</title>
		<style>
			body { font-family: Arial, sans-serif; margin: 20px; }
			table { width: 100%; border-collapse: collapse; margin-top: 20px; }
			table, th, td { border: 1px solid black; }
			th, td { padding: 8px; text-align: left; }
			th { background-color: #f2f2f2; }
		</style>
	</head>
	<body>
		<h1>Static Code Analysis Report</h1>
		{{range $file, $entries := .}}
			<h2>File: {{$file}}</h2>
			<table>
				<tr>
					<th>Line Number</th>
					<th>Line Content</th>
					<th>Source</th>
					<th>Rule</th>
					<th>Severity</th>
					<th>Description</th>
					<th>Recommendation</th>
				</tr>
				{{range $entry := $entries}}
				<tr>
					<td>{{$entry.LineNumber}}</td>
					<td>{{$entry.LineContent}}</td>
					<td>{{$entry.Source}}</td>
					<td>{{$entry.Rule}}</td>
					<td>{{$entry.Severity}}</td>
					<td>{{$entry.Description}}</td>
					<td>{{$entry.Recommendation}}</td>
				</tr>
				{{end}}
			</table>
		{{end}}
	</body>
	</html>
	`

	htmlFilePath := filepath.Join(binaryDir, "report.html")
	htmlFile, err := os.Create(htmlFilePath)
	if err != nil {
		log.Fatalf("Error creating HTML report file: %v", err)
	}
	defer htmlFile.Close()

	tmpl, err := template.New("report").Parse(htmlTemplate)
	if err != nil {
		log.Fatalf("Error parsing HTML template: %v", err)
	}

	if err := tmpl.Execute(htmlFile, report); err != nil {
		log.Fatalf("Error generating HTML report: %v", err)
	}

	fmt.Printf("HTML report saved to %s\n", htmlFilePath)
}
