package commentconsole

import "testing"

func TestConsoleOriginAllowed(t *testing.T) {
	for _, tc := range []struct {
		origin, host string
		allowed      bool
	}{
		{"http://127.0.0.1:18060", "127.0.0.1:18060", true},
		{"http://127.0.0.1:5173", "127.0.0.1:18060", true},
		{"https://studio.example.com", "studio.example.com", true},
		{"https://evil.example", "127.0.0.1:18060", false},
		{"http://localhost:5173", "studio.example.com", false},
		{"null", "127.0.0.1:18060", false},
		{"http://127.0.0.1:18060/extra", "127.0.0.1:18060", false},
	} {
		if got := consoleOriginAllowed(tc.origin, tc.host); got != tc.allowed {
			t.Errorf("origin=%q host=%q: got %v, want %v", tc.origin, tc.host, got, tc.allowed)
		}
	}
}
