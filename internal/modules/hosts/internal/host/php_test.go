package host

import "testing"

func TestPHPSelection(t *testing.T) {
	for _, tc := range []struct{ dle, requested, preferred, want string }{
		{"11.0", "", "", "7.4"},
		{"11.0", "", "8.5", "8.5"}, // explicit configured version
		{"16.0", "", "auto", "7.4"},
		{"16.1", "", "7.4", "7.4"},
		{"21.0", "", "auto", "8.5"},
		{"21.0", "", "8.1", "8.1"}, // compatible configured runtime
		{"16.0", "", "7.2", "7.2"}, // below minimum PHP version
		{"21.0", "auto", "8.1", "8.5"},
		{"21.0", "8.0", "8.5", "8.0"}, // explicit override
		{"11.0", "8.5", "7.4", "8.5"},
	} {
		if got := selectPHP(tc.dle, tc.requested, tc.preferred); got != tc.want {
			t.Errorf("DLE %s requested=%q preferred=%q: got %s, want %s", tc.dle, tc.requested, tc.preferred, got, tc.want)
		}
	}
}
