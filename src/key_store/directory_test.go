package key_store

import "testing"

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
		isErr bool
	}{
		{"src/main.go", "src/main.go", false},
		{"./src/main.go", "src/main.go", false},
		{"src/api/", "src/api/", false},
		{"src\\api\\main.go", "src/api/main.go", false},
		{"../escape.txt", "", true},
		{"src/../escape.txt", "escape.txt", false},
		{"", "", true},
		{".", "", true},
		{"./", "", true},
	}
	for _, tt := range tests {
		got, err := NormalizePath(tt.input)
		if tt.isErr {
			if err == nil {
				t.Errorf("NormalizePath(%q) expected error, got %q", tt.input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizePath(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("NormalizePath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
