package key_store

import "testing"

func TestPromoteCandidateBlockSize(t *testing.T) {
	tests := []struct {
		name      string
		fileSize  uint64
		candidate uint64
		want      uint64
	}{
		{
			name:      "zero file size returns zero",
			fileSize:  0,
			candidate: 12345,
			want:      0,
		},
		{
			name:      "file below max block size keeps candidate",
			fileSize:  uint64(MaxBlockSize) - 1,
			candidate: uint64(MinBlockSize),
			want:      uint64(MinBlockSize),
		},
		{
			name:      "file at max block size keeps candidate",
			fileSize:  uint64(MaxBlockSize),
			candidate: uint64(MinBlockSize),
			want:      uint64(MinBlockSize),
		},
		{
			name:      "file just above max block size promotes to max block size",
			fileSize:  uint64(MaxBlockSize) + 1,
			candidate: uint64(MinBlockSize),
			want:      uint64(MaxBlockSize),
		},
		{
			name:      "hard max cap remains enforced during promotion",
			fileSize:  uint64(MaxBlockSize) + 1,
			candidate: uint64(MaxBlockSize) * 2,
			want:      uint64(MaxBlockSize),
		},
		{
			name:      "file at large threshold is still eligible for promotion",
			fileSize:  uint64(MaxBlockSize) * uint64(LargeFileMx),
			candidate: uint64(MinBlockSize),
			want:      uint64(MaxBlockSize),
		},
		{
			name:      "file above large threshold keeps regular candidate",
			fileSize:  uint64(MaxBlockSize)*uint64(LargeFileMx) + 1,
			candidate: uint64(MinBlockSize),
			want:      uint64(MinBlockSize),
		},
		{
			name:      "one gibibyte keeps regular candidate",
			fileSize:  uint64(1 << 30),
			candidate: uint64(1 << 20),
			want:      uint64(1 << 20),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := promoteCandidateBlockSize(tc.fileSize, tc.candidate)
			if got != tc.want {
				t.Fatalf("promoteCandidateBlockSize(%d, %d) = %d, want %d", tc.fileSize, tc.candidate, got, tc.want)
			}
		})
	}
}

func TestCalculateBlockSizePromotionIntegration(t *testing.T) {
	tests := []struct {
		name     string
		fileSize uint64
		want     uint32
	}{
		{
			name:     "small file remains single chunk",
			fileSize: 1024,
			want:     1024,
		},
		{
			name:     "just above max block promotes to max block",
			fileSize: uint64(MaxBlockSize) + 1,
			want:     MaxBlockSize,
		},
		{
			name:     "at large threshold still promotes to max block",
			fileSize: uint64(MaxBlockSize) * uint64(LargeFileMx),
			want:     MaxBlockSize,
		},
		{
			name:     "above large threshold uses regular calculation",
			fileSize: uint64(MaxBlockSize)*uint64(LargeFileMx) + 1,
			want:     1 << 19, // 512 KiB from regular rounding path
		},
		{
			name:     "one gibibyte uses regular calculation",
			fileSize: 1 << 30,
			want:     1 << 20, // 1 MiB from regular rounding path
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CalculateBlockSize(tc.fileSize)
			if got != tc.want {
				t.Fatalf("CalculateBlockSize(%d) = %d, want %d", tc.fileSize, got, tc.want)
			}
		})
	}
}
