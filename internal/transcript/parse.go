package transcript

import (
	"bufio"
	"encoding/json"
	"os"
)

// ParseFile reads a JSONL transcript into records. Malformed lines are skipped
// so a partially-written tail line (live session) never aborts the read.
func ParseFile(path string) ([]*Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var recs []*Record
	sc := bufio.NewScanner(f)
	// transcript lines can be large (tool results); allow up to 64MB per line.
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r Record
		if err := json.Unmarshal(line, &r); err != nil {
			continue
		}
		recs = append(recs, &r)
	}
	return recs, sc.Err()
}
