package supervisor

import (
	"strings"
	"testing"
	"time"
)

func TestBuildArgs(t *testing.T) {
	s, err := New(Config{
		DataDir:   "/tmp/ppp-data",
		Ports:     []int{51820, 51821},
		AddonPath: "/tmp/addon.py",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	argv := s.buildArgs()
	got := strings.Join(argv, " ")
	want := mitmdumpBin +
		" --mode wireguard:/tmp/ppp-data/wg/keys-51820.conf@51820" +
		" --mode wireguard:/tmp/ppp-data/wg/keys-51821.conf@51821" +
		" -s /tmp/addon.py --set ppp_state_dir=/tmp/ppp-data"
	if got != want {
		t.Errorf("buildArgs()\n got: %s\nwant: %s", got, want)
	}
	// No element may be a packed shell string: each token is a distinct argv
	// element, so none should contain a space.
	for _, a := range argv {
		if strings.Contains(a, " ") {
			t.Errorf("argv element contains a space (would be a shell string): %q", a)
		}
	}
}

func TestNewValidation(t *testing.T) {
	cases := map[string]Config{
		"no data dir": {Ports: []int{51820}, AddonPath: "/a"},
		"no ports":    {DataDir: "/d", AddonPath: "/a"},
		"no addon":    {DataDir: "/d", Ports: []int{51820}},
	}
	for name, cfg := range cases {
		if _, err := New(cfg); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

func TestNewDefaultsReadyTimeout(t *testing.T) {
	s, err := New(Config{DataDir: "/d", Ports: []int{51820}, AddonPath: "/a"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.cfg.ReadyTimeout != DefaultReadyTimeout {
		t.Errorf("ReadyTimeout = %v, want %v", s.cfg.ReadyTimeout, DefaultReadyTimeout)
	}
}

func TestVersionMatches(t *testing.T) {
	cases := []struct {
		out  string
		want bool
	}{
		{"Mitmproxy: 12.2.3", true},
		{"Mitmproxy: 12.2.3\nPython:    3.14.0", true},
		{"Mitmproxy: 12.2.4", false},
		{"Mitmproxy: 11.0.0", false},
		{"no version here", false},
	}
	for _, c := range cases {
		if got := versionMatches(c.out, SupportedMitmproxyVersion); got != c.want {
			t.Errorf("versionMatches(%q) = %v, want %v", c.out, got, c.want)
		}
	}
}

func TestWaitForConfigsTimesOut(t *testing.T) {
	buf := &syncBuffer{}
	_, _ = buf.Write([]byte("nothing resembling a config here\n"))
	// want 1 config, but the buffer has none → should time out quickly.
	_, err := waitForConfigs(t.Context(), buf, 1, 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected timeout error, got %v", err)
	}
}

func TestWaitForConfigsSucceedsOnCapturedBlock(t *testing.T) {
	buf := &syncBuffer{}
	_, _ = buf.Write([]byte(sampleBlock))
	cfgs, err := waitForConfigs(t.Context(), buf, 1, 2*time.Second)
	if err != nil {
		t.Fatalf("waitForConfigs: %v", err)
	}
	if len(cfgs) != 1 || cfgs[0].ListenPort != 51820 {
		t.Fatalf("unexpected configs: %+v", cfgs)
	}
}

// sampleBlock is a minimal mitmdump 12.2.3 client-config block (timestamped
// opening fence, bare 60-hyphen closing fence).
const sampleBlock = "[10:00:00.000] ------------------------------------------------------------\n" +
	"[Interface]\n" +
	"PrivateKey = aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa=\n" +
	"Address = 10.0.0.1/32\n" +
	"DNS = 10.0.0.53\n" +
	"\n" +
	"[Peer]\n" +
	"PublicKey = bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb=\n" +
	"AllowedIPs = 0.0.0.0/0\n" +
	"Endpoint = 172.17.0.3:51820\n" +
	"------------------------------------------------------------\n"
