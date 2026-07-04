package transcript

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
)

// Usage is the token consumption parsed from a transcript segment.
type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	CacheRead    int64 `json:"cache_read"`
	CacheCreate  int64 `json:"cache_create"`
	Turns        int64 `json:"turns"`
}

type usageLine struct {
	Type    string `json:"type"`
	Message struct {
		Usage struct {
			InputTokens         int64 `json:"input_tokens"`
			OutputTokens        int64 `json:"output_tokens"`
			CacheReadInput      int64 `json:"cache_read_input_tokens"`
			CacheCreationInput  int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// TailUsage sums token usage from assistant entries starting at offset,
// returning the new offset. Same incremental contract as Tail.
func TailUsage(path string, offset int64) (Usage, int64, error) {
	var u Usage
	f, err := os.Open(path)
	if err != nil {
		return u, offset, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return u, offset, err
	}
	if offset < 0 || offset > fi.Size() {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return u, offset, err
	}

	r := bufio.NewReaderSize(f, 128*1024)
	read := int64(0)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			break // partial trailing line: picked up next time
		}
		read += int64(len(line))
		var ul usageLine
		if json.Unmarshal(line, &ul) != nil || ul.Type != "assistant" {
			continue
		}
		us := ul.Message.Usage
		if us.InputTokens == 0 && us.OutputTokens == 0 && us.CacheReadInput == 0 {
			continue
		}
		u.InputTokens += us.InputTokens
		u.OutputTokens += us.OutputTokens
		u.CacheRead += us.CacheReadInput
		u.CacheCreate += us.CacheCreationInput
		u.Turns++
	}
	return u, offset + read, nil
}
