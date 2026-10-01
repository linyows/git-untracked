package untracked

import (
	"strings"
	"testing"
)

func TestShouldSync(t *testing.T) {
	zero := strings.Repeat("0", 40)
	zero256 := strings.Repeat("0", 64)
	a, b := "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222"
	tests := []struct {
		name             string
		prev, next, flag string
		main, synced     bool
		want             bool
	}{
		{"new worktree", zero, a, "1", false, false, true},
		{"new worktree sha256", zero256, a, "1", false, false, true},
		{"file checkout", zero, a, "0", false, false, false},
		{"main worktree", zero, a, "1", true, false, false},
		{"first checkout after --no-checkout", a, a, "1", false, false, true},
		{"same commit after sync", a, a, "1", false, true, false},
		{"branch switch", a, b, "1", false, false, false},
		{"empty previous HEAD", "", a, "1", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := shouldSync(tt.prev, tt.next, tt.flag, tt.main, tt.synced)
			if got != tt.want || reason == "" {
				t.Errorf("shouldSync = %v (%s), want %v", got, reason, tt.want)
			}
		})
	}
}
