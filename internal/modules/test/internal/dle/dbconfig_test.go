package dle

import "testing"

func TestDSN(t *testing.T) {
	tests := []struct{ host, want string }{
		{"localhost", "u:p@tcp(localhost:3306)/db?multiStatements=true&charset=utf8mb4"},
		{"db.example.com:3307", "u:p@tcp(db.example.com:3307)/db?multiStatements=true&charset=utf8mb4"},
	}
	for _, tt := range tests {
		got := DBConfig{Host: tt.host, Name: "db", User: "u", Pass: "p"}.DSN()
		if got != tt.want {
			t.Errorf("host %q: got %q want %q", tt.host, got, tt.want)
		}
	}
}
