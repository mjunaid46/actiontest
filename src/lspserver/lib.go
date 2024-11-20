package lspserver

import (
	"encoding/json"
	"os"
	"strings"
)

func LoadPrompt(fileName string) ([]byte, error) {
	return os.ReadFile(fileName)
}

func TrimLeadingString(str, point string) string {
	index := strings.Index(str, point)
	if index == -1 {
		return str
	}
	return str[index:]
}

func TrimTrailingString(str, point string) string {
	index := strings.Index(str, point)
	if index == -1 {
		return str
	}
	return str[:index+len(point)]
}

func JSONStringify(obj interface{}) (string, error) {
	bytes, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}
