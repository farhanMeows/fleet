// Package transcript reads Claude Code session transcripts (JSONL) for the
// dashboard's live view. Reads are incremental: callers pass the byte offset
// from the previous read and get only new entries.
package transcript

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Entry is a simplified transcript line for display.
type Entry struct {
	Role string `json:"role"` // user | assistant | tool
	Text string `json:"text"`
}

const maxRead = 2 << 20 // 2 MiB per request keeps responses bounded

// Tail reads entries from path starting at offset. It returns the parsed
// entries and the new offset to pass next time.
func Tail(path string, offset int64) ([]Entry, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, offset, err
	}
	size := fi.Size()
	if offset < 0 || offset > size {
		offset = 0
	}
	// First read of a large transcript: start near the end, from a line boundary.
	if offset == 0 && size > maxRead {
		offset = seekLineStart(f, size-maxRead)
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}

	var entries []Entry
	r := bufio.NewReaderSize(io.LimitReader(f, maxRead), 64*1024)
	read := int64(0)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			break // partial trailing line: re-read next poll
		}
		read += int64(len(line))
		if e, ok := parseLine(line); ok {
			entries = append(entries, e)
		}
	}
	return entries, offset + read, nil
}

// seekLineStart returns the offset of the first line start at or after pos.
func seekLineStart(f *os.File, pos int64) int64 {
	if _, err := f.Seek(pos, io.SeekStart); err != nil {
		return pos
	}
	r := bufio.NewReader(f)
	skipped, err := r.ReadBytes('\n')
	if err != nil {
		return pos
	}
	return pos + int64(len(skipped))
}

// transcriptLine mirrors the subset of Claude Code's transcript format we render.
type transcriptLine struct {
	Type    string `json:"type"`
	Message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

func parseLine(raw []byte) (Entry, bool) {
	var line transcriptLine
	if err := json.Unmarshal(raw, &line); err != nil {
		return Entry{}, false
	}
	if line.Type != "user" && line.Type != "assistant" {
		return Entry{}, false
	}

	// content is either a plain string or an array of typed blocks.
	var asString string
	if err := json.Unmarshal(line.Message.Content, &asString); err == nil {
		text := strings.TrimSpace(asString)
		if text == "" {
			return Entry{}, false
		}
		return Entry{Role: line.Type, Text: clip(text)}, true
	}

	var blocks []map[string]any
	if err := json.Unmarshal(line.Message.Content, &blocks); err != nil {
		return Entry{}, false
	}
	var texts []string
	role := line.Type
	for _, b := range blocks {
		switch b["type"] {
		case "text":
			if t, _ := b["text"].(string); strings.TrimSpace(t) != "" {
				texts = append(texts, strings.TrimSpace(t))
			}
		case "tool_use":
			name, _ := b["name"].(string)
			summary := toolInputSummary(b["input"])
			role = line.Type // assistant issuing a tool call
			if summary != "" {
				texts = append(texts, fmt.Sprintf("→ %s: %s", name, summary))
			} else {
				texts = append(texts, "→ "+name)
			}
		case "tool_result":
			role = "tool"
			if t := extractToolResultText(b["content"]); t != "" {
				texts = append(texts, t)
			}
		}
	}
	if len(texts) == 0 {
		return Entry{}, false
	}
	return Entry{Role: role, Text: clip(strings.Join(texts, "\n"))}, true
}

func toolInputSummary(input any) string {
	m, ok := input.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"command", "description", "file_path", "prompt", "pattern", "url", "query"} {
		if v, ok := m[key].(string); ok && v != "" {
			return firstLine(v)
		}
	}
	return ""
}

func extractToolResultText(content any) string {
	switch c := content.(type) {
	case string:
		return clip(strings.TrimSpace(c))
	case []any:
		for _, item := range c {
			if m, ok := item.(map[string]any); ok && m["type"] == "text" {
				if t, _ := m["text"].(string); t != "" {
					return clip(strings.TrimSpace(t))
				}
			}
		}
	}
	return ""
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}

func clip(s string) string {
	const max = 4000
	if len(s) > max {
		return s[:max] + "\n…[truncated]"
	}
	return s
}
