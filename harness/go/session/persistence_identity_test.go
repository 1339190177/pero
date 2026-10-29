package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestJSONLIdentityPreflightDoesNotReadOrRewriteExistingLog(t *testing.T) {
	p, err := NewJSONLSessionPersistence(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := SessionID("reserved-child")
	if found, err := p.Exists(t.Context(), id); err != nil || found {
		t.Fatalf("missing identity=(%v,%v)", found, err)
	}
	malformed := []byte("not a valid session log\n")
	if err = os.WriteFile(p.path(id), malformed, 0600); err != nil {
		t.Fatal(err)
	}
	if found, err := p.Exists(t.Context(), id); err != nil || !found {
		t.Fatalf("occupied malformed identity=(%v,%v)", found, err)
	}
	got, err := os.ReadFile(p.path(id))
	if err != nil || string(got) != string(malformed) {
		t.Fatalf("identity lookup changed data: %q %v", got, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if found, err := p.Exists(ctx, id); found || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled identity=(%v,%v)", found, err)
	}
}

func TestJSONLUnterminatedRecordRejectsLoadAndAttach(t *testing.T) {
	for _, withEvent := range []bool{false, true} {
		t.Run(fmt.Sprint(withEvent), func(t *testing.T) {
			p, err := NewJSONLSessionPersistence(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			s := NewSession("unterminated", "")
			detach, err := p.Attach(t.Context(), s)
			if err != nil {
				t.Fatal(err)
			}
			if withEvent {
				if _, err := s.Append("fixture/one", map[string]int{"n": 1}); err != nil {
					t.Fatal(err)
				}
			}
			if err := detach(); err != nil {
				t.Fatal(err)
			}
			loaded, err := p.Load(t.Context(), s.Header().ID)
			if err != nil {
				t.Fatal(err)
			}
			path := p.path(s.Header().ID)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			torn := raw[:len(raw)-1]
			if err := os.WriteFile(path, torn, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := p.Load(t.Context(), s.Header().ID); err == nil {
				t.Fatal("accepted unterminated record")
			}
			if close, err := p.Attach(t.Context(), loaded); err == nil {
				close()
				t.Fatal("attached to unterminated record")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(torn) {
				t.Fatalf("failed admission changed bytes: %q %v", after, err)
			}
		})
	}
}

func TestJSONLCompleteTailRecovery(t *testing.T) {
	for _, mode := range []string{"header", "event", "backup-exists", "backup-directory", "stale", "lease", "incomplete", "interior", "sequence"} {
		t.Run(mode, func(t *testing.T) {
			p, err := NewJSONLSessionPersistence(t.TempDir(), JSONLPersistenceOptions{RecoverCompleteTail: true})
			if err != nil {
				t.Fatal(err)
			}
			s := NewSession("recover-tail", "")
			close, err := p.Attach(t.Context(), s)
			if err != nil {
				t.Fatal(err)
			}
			if mode != "header" {
				if _, err = s.Append("fixture/one", map[string]int{"n": 1}); err != nil {
					t.Fatal(err)
				}
			}
			if err = close(); err != nil {
				t.Fatal(err)
			}
			loaded, err := p.Load(t.Context(), s.Header().ID)
			if err != nil {
				t.Fatal(err)
			}
			path := p.path(s.Header().ID)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			torn := raw[:len(raw)-1]
			switch mode {
			case "incomplete":
				torn = torn[:len(torn)-2]
			case "interior":
				torn = append([]byte("not-json\n"), torn...)
			case "sequence":
				torn = []byte(strings.Replace(string(torn), `"seq":1`, `"seq":9`, 1))
			case "stale":
				loaded = NewSession("recover-tail", "different")
			case "backup-exists":
				if err = os.WriteFile(path+".tail-backup", []byte("prior evidence"), 0600); err != nil {
					t.Fatal(err)
				}
			case "backup-directory":
				if err = os.Mkdir(path+".tail-backup", 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err = os.WriteFile(path, torn, 0600); err != nil {
				t.Fatal(err)
			}
			read, loadErr := p.Load(t.Context(), s.Header().ID)
			malformed := mode == "incomplete" || mode == "interior" || mode == "sequence"
			if (loadErr != nil) != malformed {
				t.Fatalf("load=%v", loadErr)
			}
			after, _ := os.ReadFile(path)
			if string(after) != string(torn) {
				t.Fatal("Load mutated bytes")
			}
			if mode == "lease" {
				lease, e := acquireSessionWriteLease(path + ".lock")
				if e != nil {
					t.Fatal(e)
				}
				defer lease.Close()
			}
			if mode == "header" || mode == "event" {
				loaded = read
			}
			close, err = p.Attach(t.Context(), loaded)
			if mode != "header" && mode != "event" {
				if err == nil {
					close()
					t.Fatal("unsafe recovery accepted")
				}
				after, _ = os.ReadFile(path)
				if string(after) != string(torn) {
					t.Fatal("failed recovery mutated bytes")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			backup, e := os.ReadFile(path + ".tail-backup")
			if e != nil || string(backup) != string(torn) {
				t.Fatalf("backup=%q %v", backup, e)
			}
			if _, err = loaded.Append("fixture/recovered", map[string]int{"n": 2}); err != nil {
				t.Fatal(err)
			}
			if err = close(); err != nil {
				t.Fatal(err)
			}
			strict, e := NewJSONLSessionPersistence(p.dir)
			if e != nil {
				t.Fatal(e)
			}
			reopened, e := strict.Load(t.Context(), s.Header().ID)
			if e != nil {
				t.Fatal(e)
			}
			if len(reopened.Events()) != len(loaded.Events()) {
				t.Fatal("reopen lost events")
			}
		})
	}
}
