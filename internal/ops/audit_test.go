package ops

import (
	"context"
	"testing"

	"github.com/sapper-sh/sapper/internal/restic"
)

type retentionProbe struct{ fakeRestic }

func (*retentionProbe) Snapshots(context.Context, string) ([]restic.Snapshot, error) {
	return nil, nil
}

func (*retentionProbe) ForgetIDs(context.Context, []string) error {
	panic("unexpected snapshot deletion")
}

func TestRetentionRejectsInvalidProtectionRecord(t *testing.T) {
	bp := newFortress(t)
	bp.Backup.KeepLast = 1
	if err := writeState(bp, "last-verified.json", map[string]string{"snapshot": "invalid"}); err != nil {
		t.Fatal(err)
	}
	if _, err := retain(context.Background(), bp, &retentionProbe{}); err == nil {
		t.Fatal("invalid protection record ignored")
	}
}
