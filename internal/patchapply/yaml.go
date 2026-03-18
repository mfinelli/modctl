/*
 * mod control (modctl): command-line mod manager
 * Copyright © 2026 Mario Finelli
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program. If not, see <https://www.gnu.org/licenses/>.
 */

package patchapply

import (
	"bytes"
	"fmt"
	"strings"

	goyaml "github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// applyYAML applies patch entries to a yaml file using AST manipulation
// to preserve comments.
func applyYAML(entries []Entry, input []byte) (Result, error) {
	var res Result

	file, err := parser.ParseBytes(input, parser.ParseComments)
	if err != nil {
		return res, fmt.Errorf("parse yaml: %w", err)
	}

	if len(bytes.TrimSpace(input)) == 0 {
		// initialize with an empty mapping document
		file, err = parser.ParseBytes([]byte("{}\n"), parser.ParseComments)
		if err != nil {
			return res, fmt.Errorf("init empty yaml document: %w", err)
		}
	}

	for _, e := range entries {
		switch e.PatchType {
		case "yaml_set":
			pathStr, err := buildYAMLPath(e.EntryKey)
			if err != nil {
				return res, fmt.Errorf("build yaml path for key %q: %w", e.EntryKey, err)
			}
			p, err := goyaml.PathString(pathStr)
			if err != nil {
				return res, fmt.Errorf("invalid yaml path for key %q: %w", e.EntryKey, err)
			}
			if err := setYAMLNode(file, p, e.EntryKey, e.EntryValue); err != nil {
				return res, fmt.Errorf("set yaml key %q: %w", e.EntryKey, err)
			}
			res.Applied++

		case "yaml_unset":
			// to remove a node we need to find it in the AST and remove it
			// from its parent mapping manually
			removed, err := removeYAMLKey(file, e.EntryKey)
			if err != nil {
				return res, fmt.Errorf("unset yaml key %q: %w", e.EntryKey, err)
			}
			if removed {
				res.Applied++
			} else {
				res.Skipped++
			}
		}
	}

	res.Output = []byte(file.String())
	return res, nil
}

// escapeYAMLString escapes double quotes in a string value.
func escapeYAMLString(s string) string {
	var b strings.Builder
	for _, c := range s {
		if c == '"' {
			b.WriteString(`\"`)
		} else if c == '\\' {
			b.WriteString(`\\`)
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// appendYAMLKey appends a new key-value pair to the root mapping of the file.
func appendYAMLKey(file *ast.File, key string, valueNode ast.Node) error {
	if len(file.Docs) == 0 {
		return fmt.Errorf("yaml file has no documents")
	}
	doc := file.Docs[0]
	mapping, ok := doc.Body.(*ast.MappingNode)
	if !ok {
		return fmt.Errorf("yaml document root is not a mapping")
	}

	keyFile, err := parser.ParseBytes([]byte(key+": null"), 0)
	if err != nil || len(keyFile.Docs) == 0 {
		return fmt.Errorf("build key node for %q: %w", key, err)
	}
	mv, ok := keyFile.Docs[0].Body.(*ast.MappingNode)
	if !ok || len(mv.Values) == 0 {
		return fmt.Errorf("unexpected AST structure for key %q", key)
	}
	newEntry := mv.Values[0]
	newEntry.Value = valueNode
	mapping.Values = append(mapping.Values, newEntry)
	return nil
}

// removeYAMLKey walks the AST to find and remove a key from the root mapping.
// Returns true if the key was found and removed.
// NB: rigt now this only handles the root mapping level (since our keys are
// flat strings). If we ever want nested path support we'll need to update this
func removeYAMLKey(file *ast.File, key string) (bool, error) {
	if len(file.Docs) == 0 {
		return false, nil
	}
	doc := file.Docs[0]
	mapping, ok := doc.Body.(*ast.MappingNode)
	if !ok {
		return false, nil
	}

	for i, mv := range mapping.Values {
		keyNode, ok := mv.Key.(*ast.StringNode)
		if !ok {
			continue
		}
		if keyNode.Value == key {
			mapping.Values = append(mapping.Values[:i], mapping.Values[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

// buildYAMLPath wraps a key in a YAMLPath expression.
// Keys containing dots or special characters are quoted per the goccy/go-yaml
// path spec: single-quote the key.
func buildYAMLPath(key string) (string, error) {
	// If the key contains characters that need quoting, wrap in single quotes
	needsQuote := false
	for _, c := range key {
		if c == '.' || c == '[' || c == ']' || c == '*' || c == '\'' {
			needsQuote = true
			break
		}
	}
	if needsQuote {
		escaped := strings.ReplaceAll(key, "'", "''")
		return "$.'" + escaped + "'", nil
	}
	return "$." + key, nil
}

func setYAMLNode(file *ast.File, p *goyaml.Path, rawKey string, value string) error {
	// build value node by parsing the value as yaml
	valueNode, err := parseYAMLValue(value)
	if err != nil {
		return fmt.Errorf("build value node: %w", err)
	}

	// try to replace existing node
	if err := p.ReplaceWithNode(file, valueNode); err == nil {
		return nil
	}

	// node doesn't exist — append by rebuilding the document
	return appendYAMLKeyByRebuild(file, rawKey, value)
}

// appendYAMLKeyByRebuild appends a key to the document by serializing
// the current AST back to a string, appending the new key, and re-parsing.
// This avoids issues with AST token position tracking during direct mutation.
func appendYAMLKeyByRebuild(file *ast.File, key string, value string) error {
	current := file.String()
	// ensure trailing newline
	if len(current) > 0 && current[len(current)-1] != '\n' {
		current += "\n"
	}

	// append the new key: value line
	// we need to produce valid YAML for the value
	newDoc := current + fmt.Sprintf("%s: %s\n", key, value)

	newFile, err := parser.ParseBytes([]byte(newDoc), parser.ParseComments)
	if err != nil {
		return fmt.Errorf("re-parse after append: %w", err)
	}

	// replace file contents in place
	file.Docs = newFile.Docs
	return nil
}

func parseYAMLValue(value string) (ast.Node, error) {
	valueFile, err := parser.ParseBytes([]byte(value), 0)
	if err == nil && len(valueFile.Docs) > 0 && valueFile.Docs[0].Body != nil {
		return valueFile.Docs[0].Body, nil
	}
	// fall back to quoted string
	quoted := `"` + escapeYAMLString(value) + `"`
	valueFile, err = parser.ParseBytes([]byte(quoted), 0)
	if err != nil || len(valueFile.Docs) == 0 {
		return nil, fmt.Errorf("cannot parse value %q as yaml", value)
	}
	return valueFile.Docs[0].Body, nil
}
