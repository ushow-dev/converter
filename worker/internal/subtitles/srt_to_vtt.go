package subtitles

import (
	"bytes"
	"strings"
)

// SRTtoVTT converts SRT subtitle bytes to WebVTT format.
// Transformation: prepend "WEBVTT\n\n" and replace comma with dot in timestamps.
func SRTtoVTT(srt []byte) []byte {
	content := string(srt)

	// Replace SRT timestamp separator: "00:01:23,456" → "00:01:23.456"
	// Only replace commas that look like timestamp separators (digit,digit pattern).
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		// Timestamp lines contain " --> "
		if strings.Contains(line, " --> ") {
			lines[i] = strings.ReplaceAll(line, ",", ".")
		}
	}

	var buf bytes.Buffer
	buf.WriteString("WEBVTT\n\n")
	buf.WriteString(strings.Join(lines, "\n"))
	return buf.Bytes()
}
