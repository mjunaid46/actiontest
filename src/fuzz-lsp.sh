#!/usr/bin/env bash

execDir=$(dirname "$(readlink -f "$0")")

. $execDir/credentials.env

$execDir/fuzz-lsp.exe -backend ollama -connect-test true -prompt-file $execDir/prompts/prompt.txt 2> $execDir/fuzz-lsp.log $@
