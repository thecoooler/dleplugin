package bundle

import "testing"

func TestReleaseDowngradeComparison(t *testing.T) {
	for _, tt := range []struct {
		from, to  string
		downgrade bool
	}{
		{"v2.0.0", "v1.0.0", true},
		{"v1.10.0", "v1.9.0", true},
		{"v1.2.11", "v1.2.9", true},
		{"v1.0.0", "v1.0.0-rc.1", true},
		{"v1.0.0-rc.11", "v1.0.0-rc.9", true},
		{"v1.0.0-beta", "v1.0.0-alpha", true},
		{"v1.0.0-beta", "v1.0.0-11", true},
		{"v1.0.0-alpha.1", "v1.0.0-alpha", true},
		{"v1.0.0-alpha", "v1.0.0-alpha.1", false},
		{"v1.0.0-11", "v1.0.0-beta", false},
		{"v1.0.0", "v2.0.0", false},
		{"v1.0.0", "v1.0.0", false},
		{"v1.0.0+abc", "v1.0.0+def", false},
		{"v1.0.0-rc.01", "v1.0.0-alpha", false},
		{"v01.0.0", "v0.0.0", false},
		{"dev", "v1.0.0", false},
		{"v1.0.0", "dev", false},
	} {
		t.Run(tt.from+"->"+tt.to, func(t *testing.T) {
			if got := newerRelease(tt.from, tt.to); got != tt.downgrade {
				t.Fatalf("downgrade = %v, want %v", got, tt.downgrade)
			}
		})
	}
}
