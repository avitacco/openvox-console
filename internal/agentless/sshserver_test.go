package agentless

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/voxpupuli/enterprise-console/internal/taskcatalog"
)

// testSSH is an SSH server in this process that runs each exec request
// with the local shell, so a task really runs. A fake `sudo` stands in for
// the real one (a test cannot become another user).
type testSSH struct {
	t        *testing.T
	listener net.Listener
	hostKey  ssh.Signer
	tmp      string

	mu          sync.Mutex
	commands    []string
	passwordTry int
	keyTry      int
	users       map[string]string // user -> password
	authorized  ssh.PublicKey
	sudoRefuses bool
	sudoCalls   []string
	extraEnv    []string
}

func newSigner(t *testing.T) (ssh.Signer, ssh.PublicKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer, signer.PublicKey()
}

func startSSH(t *testing.T) *testSSH {
	t.Helper()
	hostKey, _ := newSigner(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &testSSH{t: t, listener: l, hostKey: hostKey, tmp: t.TempDir(), users: map[string]string{"ops": "right-password"}}
	t.Cleanup(func() { l.Close() })

	// The fake sudo: sudo -n -u USER -- cmd...
	bin := filepath.Join(s.tmp, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho \"$3\" >> " + filepath.Join(s.tmp, "sudo.log") + "\n" +
		"if [ -f " + filepath.Join(s.tmp, "sudo-refuses") + " ]; then echo 'sudo: a password is required' >&2; exit 1; fi\n" +
		"shift 4\nexec env -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "sudo"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	go s.serve()
	return s
}

func (s *testSSH) host() string { h, _, _ := net.SplitHostPort(s.listener.Addr().String()); return h }
func (s *testSSH) port() int {
	_, p, _ := net.SplitHostPort(s.listener.Addr().String())
	n, _ := strconv.Atoi(p)
	return n
}

func (s *testSSH) setSudoRefuses(on bool) {
	f := filepath.Join(s.tmp, "sudo-refuses")
	if on {
		_ = os.WriteFile(f, nil, 0o644)
	} else {
		_ = os.Remove(f)
	}
}

func (s *testSSH) sudoLog() string {
	b, _ := os.ReadFile(filepath.Join(s.tmp, "sudo.log"))
	return string(b)
}

func (s *testSSH) commandLog() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.commands...)
}

func (s *testSSH) attempts() (password, key int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.passwordTry, s.keyTry
}

// leftovers lists what is left in the temporary directory tasks use.
func (s *testSSH) leftovers() []string {
	entries, _ := os.ReadDir(filepath.Join(s.tmp, "work"))
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func (s *testSSH) serve() {
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			s.mu.Lock()
			s.passwordTry++
			want, ok := s.users[c.User()]
			s.mu.Unlock()
			if ok && want == string(pw) {
				return nil, nil
			}
			return nil, errors.New("denied")
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			s.mu.Lock()
			s.keyTry++
			auth := s.authorized
			s.mu.Unlock()
			if auth != nil && string(auth.Marshal()) == string(key.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("denied")
		},
	}
	cfg.AddHostKey(s.hostKey)
	for {
		raw, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(raw, cfg)
	}
}

func (s *testSSH) handle(raw net.Conn, cfg *ssh.ServerConfig) {
	conn, chans, reqs, err := ssh.NewServerConn(raw, cfg)
	if err != nil {
		raw.Close()
		return
	}
	defer conn.Close()
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "no")
			continue
		}
		ch, creqs, err := nc.Accept()
		if err != nil {
			return
		}
		go s.session(conn.User(), ch, creqs)
	}
}

func (s *testSSH) session(user string, ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	for req := range reqs {
		if req.Type != "exec" {
			_ = req.Reply(false, nil)
			continue
		}
		var payload struct{ Command string }
		_ = ssh.Unmarshal(req.Payload, &payload)
		_ = req.Reply(true, nil)
		s.mu.Lock()
		s.commands = append(s.commands, payload.Command)
		s.mu.Unlock()

		work := filepath.Join(s.tmp, "work")
		_ = os.MkdirAll(work, 0o755)
		cmd := exec.Command("sh", "-c", payload.Command)
		cmd.Env = append(os.Environ(), "PATH="+filepath.Join(s.tmp, "bin")+":"+os.Getenv("PATH"), "TMPDIR="+work, "USER="+user)
		cmd.Stdin = ch
		cmd.Stdout = ch
		cmd.Stderr = ch.Stderr()
		code := 0
		if err := cmd.Run(); err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				code = ee.ExitCode()
			} else {
				code = 127
			}
		}
		_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{uint32(code)}))
		return
	}
}

// --- fakes ---

type fakeRegistry struct {
	mu       sync.Mutex
	conns    map[string]Connection
	proposed map[int64][]byte
	flagged  map[int64][]byte
}

func newRegistry() *fakeRegistry {
	return &fakeRegistry{conns: map[string]Connection{}, proposed: map[int64][]byte{}, flagged: map[int64][]byte{}}
}

func (r *fakeRegistry) Connection(_ context.Context, name string) (Connection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.conns[name]
	if !ok {
		return Connection{}, ErrNotFound
	}
	return c, nil
}

func (r *fakeRegistry) ProposeKey(_ context.Context, id int64, wire []byte) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.proposed[id] = wire
	return PublicKeyFingerprint(wire)
}

func (r *fakeRegistry) FlagChangedKey(_ context.Context, id int64, wire []byte) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flagged[id] = wire
	return PublicKeyFingerprint(wire)
}

type fakeFiles struct {
	mu      sync.Mutex
	content map[string][]byte // by sha256
	fetched int
}

func (f *fakeFiles) File(_ context.Context, file taskcatalog.ResolvedFile) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetched++
	data, ok := f.content[file.SHA256]
	if !ok {
		return nil, &taskcatalog.ChecksumMismatchError{Filename: file.Filename, Want: file.SHA256, Got: "other"}
	}
	return data, nil
}

type rig struct {
	t     *testing.T
	ssh   *testSSH
	reg   *fakeRegistry
	files *fakeFiles
	exec  *Executor
}

func newRig(t *testing.T) *rig {
	s := startSSH(t)
	r := &rig{t: t, ssh: s, reg: newRegistry(), files: &fakeFiles{content: map[string][]byte{}}}
	r.exec = NewExecutor(r.reg, r.files)
	r.reg.conns["host1"] = r.conn(1, "ops", "right-password")
	return r
}

// conn is a connection to the test server with its host key pinned.
func (r *rig) conn(id int64, user, password string) Connection {
	return Connection{ID: id, Name: "host1", Address: r.ssh.host(), Port: r.ssh.port(), Username: user, Password: password, PinnedKey: r.ssh.hostKey.PublicKey().Marshal()}
}

func (r *rig) set(name string, c Connection) { r.reg.conns[name] = c }

// task builds a resolved task whose one implementation is script.
func (r *rig) task(script string, params map[string]any, timeout int) taskcatalog.Resolved {
	sum := sha256.Sum256([]byte(script))
	h := hex.EncodeToString(sum[:])
	r.files.mu.Lock()
	r.files.content[h] = []byte(script)
	r.files.mu.Unlock()
	return taskcatalog.Resolved{
		Task: "demo::echo", Environment: "production",
		Implementations: []taskcatalog.ResolvedImplementation{{Name: "echo.sh", Requirements: []string{"shell"}}},
		Files:           []taskcatalog.ResolvedFile{{Filename: "echo.sh", Path: "/f", SHA256: h, Size: int64(len(script))}},
		Params:          params, TimeoutSeconds: timeout,
	}
}

func decode(t *testing.T, raw []byte) wireResponse {
	t.Helper()
	var w wireResponse
	if err := jsonUnmarshal(raw, &w); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return w
}

var _ = io.EOF
