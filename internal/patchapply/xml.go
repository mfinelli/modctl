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
	"fmt"
	"strings"

	"github.com/beevik/etree"
)

// applyXML applies patch entries to an XML document using XPath expressions
// as keys. Comments, attribute order, and formatting are preserved by etree.
//
// entry_key is an XPath expression that may match one or more nodes.
// For xml_set:   sets the text content or attribute value of all matched nodes.
// For xml_unset: removes all matched nodes from their parents.
// For xml_clear: clears the text content or attribute value of all matched
//
//	nodes without removing them.
//
// Attribute nodes are identified by XPath expressions ending in /@attrname
// (e.g. //Settings/Window/@width). All other expressions are treated as
// element nodes and operate on their text content.
func applyXML(entries []Entry, input []byte) (Result, error) {
	var res Result

	doc := etree.NewDocument()
	doc.WriteSettings = etree.WriteSettings{
		CanonicalEndTags: false,
		CanonicalText:    false,
		CanonicalAttrVal: false,
		UseCRLF:          false,
	}

	if len(input) == 0 {
		// empty document: start with a bare root
		doc.CreateProcInst("xml", `version="1.0" encoding="UTF-8"`)
	} else {
		if err := doc.ReadFromBytes(input); err != nil {
			return res, fmt.Errorf("parse xml: %w", err)
		}
	}

	for _, e := range entries {
		switch e.PatchType {
		case "xml_set":
			applied, err := xmlSet(doc, e.EntryKey, e.EntryValue)
			if err != nil {
				return res, fmt.Errorf("xml_set %q: %w", e.EntryKey, err)
			}
			if applied == 0 {
				res.Skipped++
			} else {
				res.Applied++
			}

		case "xml_unset":
			removed, err := xmlUnset(doc, e.EntryKey)
			if err != nil {
				return res, fmt.Errorf("xml_unset %q: %w", e.EntryKey, err)
			}
			if removed == 0 {
				res.Skipped++
			} else {
				res.Applied++
			}

		case "xml_clear":
			cleared, err := xmlClear(doc, e.EntryKey)
			if err != nil {
				return res, fmt.Errorf("xml_clear %q: %w", e.EntryKey, err)
			}
			if cleared == 0 {
				res.Skipped++
			} else {
				res.Applied++
			}
		}
	}

	out, err := doc.WriteToBytes()
	if err != nil {
		return res, fmt.Errorf("serialize xml: %w", err)
	}
	res.Output = out
	return res, nil
}

// isAttrXPath reports whether the XPath expression targets an attribute node.
// We detect this by checking whether the last path segment starts with "@".
func isAttrXPath(xpath string) (parentXPath string, attrName string, ok bool) {
	// find the last "/" and check if what follows starts with "@"
	idx := strings.LastIndex(xpath, "/")
	if idx < 0 {
		return "", "", false
	}
	last := xpath[idx+1:]
	if !strings.HasPrefix(last, "@") {
		return "", "", false
	}
	return xpath[:idx], last[1:], true
}

// xmlSet sets the text content or attribute value of all nodes matched by
// the XPath expression. Returns the number of nodes modified.
func xmlSet(doc *etree.Document, xpath, value string) (int, error) {
	if parentXPath, attrName, ok := isAttrXPath(xpath); ok {
		elements := doc.FindElements(parentXPath)
		if len(elements) == 0 {
			return 0, nil
		}
		for _, el := range elements {
			el.CreateAttr(attrName, value)
		}
		return len(elements), nil
	}

	elements := doc.FindElements(xpath)
	if len(elements) == 0 {
		return 0, nil
	}
	for _, el := range elements {
		el.SetText(value)
	}
	return len(elements), nil
}

// xmlUnset removes all nodes matched by the XPath expression from their
// parents. Returns the number of nodes removed.
func xmlUnset(doc *etree.Document, xpath string) (int, error) {
	if parentXPath, attrName, ok := isAttrXPath(xpath); ok {
		elements := doc.FindElements(parentXPath)
		if len(elements) == 0 {
			return 0, nil
		}
		removed := 0
		for _, el := range elements {
			if el.RemoveAttr(attrName) != nil {
				removed++
			}
		}
		return removed, nil
	}

	elements := doc.FindElements(xpath)
	if len(elements) == 0 {
		return 0, nil
	}
	removed := 0
	for _, el := range elements {
		if el.Parent() == nil {
			continue
		}
		el.Parent().RemoveChild(el)
		removed++
	}
	return removed, nil
}

// xmlClear clears the text content or attribute value of all nodes matched
// by the XPath expression without removing the nodes themselves.
// Returns the number of nodes cleared.
func xmlClear(doc *etree.Document, xpath string) (int, error) {
	if parentXPath, attrName, ok := isAttrXPath(xpath); ok {
		elements := doc.FindElements(parentXPath)
		if len(elements) == 0 {
			return 0, nil
		}
		for _, el := range elements {
			el.CreateAttr(attrName, "")
		}
		return len(elements), nil
	}

	elements := doc.FindElements(xpath)
	if len(elements) == 0 {
		return 0, nil
	}
	for _, el := range elements {
		el.SetText("")
	}
	return len(elements), nil
}
