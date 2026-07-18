package downloader

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseRange 解析 HTTP Range 头
// 支持: bytes=X-, bytes=X-Y, bytes=-Y
func ParseRange(rangeHeader string, totalSize int64) (*Range, error) {
	if rangeHeader == "" {
		return nil, nil
	}

	const prefix = "bytes="
	if !strings.HasPrefix(rangeHeader, prefix) {
		return nil, fmt.Errorf("invalid range header: %s", rangeHeader)
	}

	rangeVal := rangeHeader[len(prefix):]
	parts := strings.SplitN(rangeVal, "-", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid range: %s", rangeHeader)
	}

	r := &Range{}

	if parts[0] == "" {
		// bytes=-Y (suffix)
		suffix, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid suffix range: %s", parts[1])
		}
		r.Start = totalSize - suffix
		if r.Start < 0 {
			r.Start = 0
		}
		r.End = totalSize
	} else {
		start, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid start range: %s", parts[0])
		}
		r.Start = start

		if parts[1] == "" {
			// bytes=X-
			r.End = totalSize
		} else {
			end, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid end range: %s", parts[1])
			}
			r.End = end + 1 // inclusive → exclusive
			if r.End > totalSize {
				r.End = totalSize
			}
		}
	}

	return r, nil
}
