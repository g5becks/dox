package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

// README code blocks containing dox commands must be preceded by a marker:
//
//	<!-- doxtest: workspace=synced -->  run after syncing local fixture sources
//	<!-- doxtest: workspace=empty -->   run with a dox.toml that has no sources
//	<!-- doxtest: workspace=bare -->    run in an empty directory
//	<!-- doxtest: config -->            the TOML block must load (dox list)
//	<!-- doxtest: skip (reason) -->     documented reason it cannot run offline
var (
	readmeMarker  = regexp.MustCompile(`^<!-- doxtest: (workspace=(synced|empty|bare)|config|skip \(.+\)) -->$`)
	trailingShell = regexp.MustCompile(`\s{2,}#.*$`)
)

type readmeBlock struct {
	line     int
	marker   string
	language string
	body     []string
}

func readmeBlocks(t *testing.T) []readmeBlock {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	var blocks []readmeBlock
	for index := 0; index < len(lines); index++ {
		fence, found := strings.CutPrefix(lines[index], "```")
		if !found || fence == "" {
			continue
		}
		block := readmeBlock{line: index + 1, language: fence}
		for previous := index - 1; previous >= 0; previous-- {
			if trimmed := strings.TrimSpace(lines[previous]); trimmed != "" {
				if readmeMarker.MatchString(trimmed) {
					block.marker = trimmed
				}
				break
			}
		}
		for index++; index < len(lines) && lines[index] != "```"; index++ {
			block.body = append(block.body, lines[index])
		}
		blocks = append(blocks, block)
	}
	return blocks
}

func doxCommands(block readmeBlock) []string {
	var commands []string
	for _, line := range block.body {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "dox ") {
			commands = append(commands, strings.TrimSpace(trailingShell.ReplaceAllString(trimmed, "")))
		}
	}
	return commands
}

// shellWords splits a README command line with shell-style double and single
// quotes, then requotes each word for testscript (which only knows single quotes).
func shellWords(t *testing.T, line string) string {
	t.Helper()
	var words []string
	var current strings.Builder
	inWord := false
	var quote rune
	for _, r := range line {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			current.WriteRune(r)
		case r == '"' || r == '\'':
			quote, inWord = r, true
		case r == ' ' || r == '\t':
			if inWord {
				words = append(words, current.String())
				current.Reset()
				inWord = false
			}
		default:
			current.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		t.Fatalf("unterminated quote in README command %q", line)
	}
	if inWord {
		words = append(words, current.String())
	}
	for index, word := range words {
		words[index] = "'" + strings.ReplaceAll(word, "'", "''") + "'"
	}
	return strings.Join(words, " ")
}

func TestREADMEExamplesAreMarked(t *testing.T) {
	t.Parallel()
	for _, block := range readmeBlocks(t) {
		if len(doxCommands(block)) > 0 && block.marker == "" {
			t.Errorf("README.md:%d: dox example block needs a <!-- doxtest: ... --> marker", block.line)
		}
	}
}

func TestREADMEExamples(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	count := 0
	for _, block := range readmeBlocks(t) {
		script, ok := readmeScript(t, block)
		if !ok {
			continue
		}
		count++
		name := filepath.Join(dir, fmt.Sprintf("readme-line-%04d.txtar", block.line))
		if err := os.WriteFile(name, []byte(script), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if count == 0 {
		t.Fatal("no runnable README examples found")
	}
	testscript.Run(t, testscript.Params{
		Dir: dir,
		Setup: func(env *testscript.Env) error {
			env.Values[scriptEnvKey{}] = append([]string(nil), env.Vars...)
			return nil
		},
		Cmds: scriptCommands(),
	})
}

func readmeScript(t *testing.T, block readmeBlock) (string, bool) {
	t.Helper()
	var script strings.Builder
	switch block.marker {
	case "<!-- doxtest: config -->":
		script.WriteString("# README config example must load.\ndox list\n\n-- dox.toml --\n")
		script.WriteString(strings.Join(block.body, "\n") + "\n")
		return script.String(), true
	case "<!-- doxtest: workspace=synced -->":
		script.WriteString("httpservedir site\nenvsubst config.in dox.toml\ndox sync\n")
	case "<!-- doxtest: workspace=empty -->":
		script.WriteString("cp empty.toml dox.toml\n")
	case "<!-- doxtest: workspace=bare -->":
	default:
		return "", false
	}
	for _, command := range doxCommands(block) {
		script.WriteString(shellWords(t, command) + "\n")
	}
	script.WriteString(readmeFixtures)
	return script.String(), true
}

// readmeFixtures serves the collections the README examples name.
const readmeFixtures = `
-- empty.toml --
output = ".dox"
-- config.in --
output = ".dox"
[sources.goreleaser]
url = "$SERVER/goreleaser/install.md"
filename = "docs/install.md"
[sources.hono]
url = "$SERVER/hono/llms-full.txt"
[sources.react]
url = "$SERVER/react/hooks.md"
filename = "docs/hooks.md"
-- site/goreleaser/install.md --
# Installation

Install GoReleaser with your package manager, then add a configuration file.

## Configuration

The configuration guide covers builds, archives and the release API.

## Hooks

Run hooks before and after builds.

## Logging

    func NewLogger() *Logger { return &Logger{} }

## Imports

Import existing configuration files.

## Authentication

Set a token for releases.

line 23
line 24
line 25
line 26
line 27
line 28
line 29
line 30
line 31
line 32
line 33
line 34
line 35
-- site/hono/llms-full.txt --
Hono guide: routing, middleware and the API reference.
-- site/react/hooks.md --
# Hooks

Use useState and other hooks; this guide covers authentication patterns.
`
