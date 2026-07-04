// Package guard enforces the prod-data rule: agents must never run
// destructive commands against production databases/hosts. It runs as a
// PreToolUse hook on Bash — the ONLY fleet hook allowed to exit non-zero.
// Internal errors fail open: a broken guard must not brick all Bash usage
// (the CLAUDE.md rule remains as the second layer).
package guard

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// destructive matches operations that can lose or corrupt data. A command is
// only blocked when BOTH a prod pattern AND a destructive verb appear, so
// day-to-day dev work (localhost SQL, force pushes to feature branches on a
// dev remote) is unaffected unless it references prod.
var destructive = regexp.MustCompile(`(?i)\b(drop|truncate|delete|update|alter)\b|--force|flushall|dropdatabase|db\.drop`)

const PatternsFile = "guard-patterns.txt"

// Load reads prod patterns (one per line, # comments) from ~/.fleet.
func Load(fleetDir string) ([]string, error) {
	f, err := os.Open(filepath.Join(fleetDir, PatternsFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out, sc.Err()
}

func Add(fleetDir, pattern string) error {
	f, err := os.OpenFile(filepath.Join(fleetDir, PatternsFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, pattern)
	return err
}

func Remove(fleetDir, pattern string) error {
	patterns, err := Load(fleetDir)
	if err != nil {
		return err
	}
	kept := patterns[:0]
	found := false
	for _, p := range patterns {
		if p == pattern {
			found = true
			continue
		}
		kept = append(kept, p)
	}
	if !found {
		return fmt.Errorf("pattern %q not found", pattern)
	}
	content := strings.Join(kept, "\n")
	if content != "" {
		content += "\n"
	}
	return os.WriteFile(filepath.Join(fleetDir, PatternsFile), []byte(content), 0o644)
}

// Verdict is the result of checking one command.
type Verdict struct {
	Blocked bool
	Pattern string // which prod pattern matched
	Verb    string // which destructive fragment matched
}

// Check evaluates a shell command against the loaded prod patterns.
// Patterns are matched case-insensitively as plain substrings unless they
// compile as a regexp (then used as one).
func Check(command string, patterns []string) Verdict {
	verb := destructive.FindString(command)
	if verb == "" {
		return Verdict{}
	}
	lower := strings.ToLower(command)
	for _, p := range patterns {
		if strings.Contains(lower, strings.ToLower(p)) {
			return Verdict{Blocked: true, Pattern: p, Verb: verb}
		}
		if re, err := regexp.Compile("(?i)" + p); err == nil && re.MatchString(command) {
			return Verdict{Blocked: true, Pattern: p, Verb: verb}
		}
	}
	return Verdict{}
}
