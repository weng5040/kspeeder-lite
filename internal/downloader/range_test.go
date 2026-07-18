package downloader

import (
	"testing"
)

func TestParseRange(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		totalSize int64
		wantStart int64
		wantEnd   int64
		wantErr   bool
	}{
		{"bytes start-end", "bytes=0-99", 200, 0, 100, false},
		{"bytes start-", "bytes=100-", 200, 100, 200, false},
		{"bytes suffix", "bytes=-50", 200, 150, 200, false},
		{"empty header", "", 200, 0, 0, false}, // returns nil
		{"invalid prefix", "invalid=0-100", 200, 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := ParseRange(tt.header, tt.totalSize)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if r == nil && tt.header == "" {
				return // nil range for empty header is expected
			}
			if r == nil {
				t.Fatal("expected non-nil range")
			}
			if r.Start != tt.wantStart {
				t.Errorf("start: want %d, got %d", tt.wantStart, r.Start)
			}
			if r.End != tt.wantEnd {
				t.Errorf("end: want %d, got %d", tt.wantEnd, r.End)
			}
		})
	}
}
