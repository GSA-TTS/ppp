package policy

import "testing"

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantHost string
		wantPort int
		wantErr  bool
	}{
		{name: "host only", in: "api.anthropic.com", wantHost: "api.anthropic.com"},
		{name: "host and port", in: "api.anthropic.com:443", wantHost: "api.anthropic.com", wantPort: 443},
		{name: "ipv4 host", in: "10.0.0.1", wantHost: "10.0.0.1"},
		{name: "ipv4 host and port", in: "10.0.0.1:8080", wantHost: "10.0.0.1", wantPort: 8080},
		{name: "bare ipv6", in: "2001:db8::1", wantHost: "2001:db8::1"},
		{name: "bracketed ipv6 with port", in: "[2001:db8::1]:443", wantHost: "2001:db8::1", wantPort: 443},
		{name: "bracketed ipv6 no port", in: "[::1]", wantHost: "::1"},
		{name: "surrounding whitespace", in: "  example.com:80  ", wantHost: "example.com", wantPort: 80},
		{name: "empty", in: "", wantErr: true},
		{name: "whitespace only", in: "   ", wantErr: true},
		{name: "empty host with port", in: ":443", wantErr: true},
		{name: "non-numeric port", in: "host:https", wantErr: true},
		{name: "port zero", in: "host:0", wantErr: true},
		{name: "port too large", in: "host:70000", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseTarget(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseTarget(%q) = %+v, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTarget(%q) unexpected error: %v", tc.in, err)
			}
			if got.Host != tc.wantHost || got.Port != tc.wantPort {
				t.Fatalf("ParseTarget(%q) = {Host:%q Port:%d}, want {Host:%q Port:%d}",
					tc.in, got.Host, got.Port, tc.wantHost, tc.wantPort)
			}
		})
	}
}
