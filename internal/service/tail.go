package service

import (
	"io"
	"os"
	"strings"
)

// tailChunk is how much of a file Tail reads from the end at a time.
const tailChunk = 16 << 10

// Tail returns the last n lines of a file, without line endings.
func Tail(file string, n int) ([]string, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	// Read backwards in chunks until there are more than n line breaks, so
	// that the first line kept is complete.
	end := info.Size()
	var buf []byte
	for end > 0 && strings.Count(string(buf), "\n") <= n {
		start := max(end-tailChunk, 0)
		chunk := make([]byte, end-start)
		if _, err := f.ReadAt(chunk, start); err != nil && err != io.EOF {
			return nil, err
		}
		buf = append(chunk, buf...)
		end = start
	}

	text := strings.ReplaceAll(string(buf), "\r\n", "\n")
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil, nil
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}
