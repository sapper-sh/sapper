package restic

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

func TestParseSnapshotID(t *testing.T) {
	tests := []struct {
		name, out, want string
		wantErr         bool
	}{
		{"summary last", `{"message_type":"status","percent_done":0.5}` + "\n" + `{"message_type":"summary","snapshot_id":"abc123"}`, "abc123", false},
		{"no summary", `{"message_type":"status"}`, "", true},
		{"garbage lines ignored", "warning: x\n" + `{"message_type":"summary","snapshot_id":"d"}`, "d", false},
		{"empty", "", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSnapshotID([]byte(tc.out))
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("got %q, %v; want %q, err=%v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestParseLatest(t *testing.T) {
	tests := []struct {
		name, out, want string
		wantErr         error
	}{
		{"one", `[{"id":"a","time":"2026-01-01T00:00:00Z"}]`, "a", nil},
		{"newest wins", `[{"id":"a","time":"2026-01-01T00:00:00Z"},{"id":"b","time":"2026-02-01T00:00:00Z"}]`, "b", nil},
		{"none", `[]`, "", ErrNoSnapshot},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLatest([]byte(tc.out))
			if !errors.Is(err, tc.wantErr) || got.ID != tc.want {
				t.Fatalf("got %q, %v; want %q, %v", got.ID, err, tc.want, tc.wantErr)
			}
		})
	}
	if _, err := parseLatest([]byte("not json")); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestNewRequiresPassword(t *testing.T) {
	if _, err := New("/tmp/r", ""); err == nil {
		t.Fatal("expected error for empty password")
	}
}

func TestNewMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := New("/tmp/r", "pw"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("err = %v, want ErrNotInstalled", err)
	}
}

func TestClassifyExitCodes(t *testing.T) {
	run := func(code int) error {
		return exec.Command("sh", "-c", fmt.Sprintf("exit %d", code)).Run()
	}
	if err := classify("snapshots", run(12), "Fatal: wrong password or no key found"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("exit 12: %v", err)
	}
	err := classify("forget", run(11), "unable to create lock in backend: repository is already locked by PID 7")
	if !errors.Is(err, ErrLocked) || !strings.Contains(err.Error(), "restic -r <repository> unlock") || !strings.Contains(err.Error(), "PID 7") {
		t.Fatalf("exit 11: %v", err)
	}
	if err := classify("backup", run(3), "some files unreadable"); errors.Is(err, ErrLocked) || errors.Is(err, ErrWrongPassword) || !strings.Contains(err.Error(), "unreadable") {
		t.Fatalf("exit 3: %v", err)
	}
}
