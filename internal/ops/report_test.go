package ops

import (
	"context"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sapper-unbuilt/sapper/internal/manifest"
	"github.com/sapper-unbuilt/sapper/internal/report"
)

func TestEveryDrillLeavesAChainedReport(t *testing.T) {
	bp := newFortress(t)
	fr := &fakeRestic{snapDir: t.TempDir()}
	ctx := context.Background()

	rep, _ := Breach(ctx, bp, fr, DrillOptions{})
	if r := rep.Report; r == nil || r.Verdict != report.NoVerdict || r.Headline != "There is no backup to test yet." || rep.ReportErr != nil {
		t.Fatalf("no-snapshot report: %+v err %v", r, rep.ReportErr)
	}
	if _, err := Backup(ctx, bp, fr); err != nil {
		t.Fatal(err)
	}
	_, pub, priv := keyPair(t)
	rep, err := Breach(ctx, bp, fr, DrillOptions{SignKey: priv, Now: func() time.Time { return time.Date(2026, 11, 4, 1, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	r := rep.Report
	if r.Verdict != report.Survived || r.Seq != 2 || r.Signature == nil || !strings.HasSuffix(rep.ReportPath, "000002.json") {
		t.Fatalf("survived report: %+v", r)
	}
	if r.Recovery.RPO.MeasuredMS != time.Hour.Milliseconds() || r.Recovery.RPO.Met == nil || !*r.Recovery.RPO.Met {
		t.Errorf("RPO %+v", r.Recovery.RPO)
	}
	if !r.Isolation.RestoreOutsideLive || !r.Isolation.LiveUntouched || r.Evidence.MerkleRoot == "" || len(r.Phases) != 6 {
		t.Errorf("isolation %+v phases %+v", r.Isolation, r.Phases)
	}
	if len(r.Volumes) != 4 || r.Volumes[0].App != "filebrowser" {
		t.Errorf("volumes %+v", r.Volumes)
	}

	rep, _ = Breach(ctx, bp, fr, DrillOptions{SignKey: priv, BeforeVerify: func(dir string) error {
		return os.WriteFile(manifest.RestoredPath(dir, filepath.Join(bp.VolumePaths()[0], "nested", "data.db")), []byte("tampered"), 0o644)
	}})
	if rep.Report.Verdict != report.Breached || rep.Report.Evidence.Findings[0].Kind == "" {
		t.Fatalf("breached report: %+v", rep.Report)
	}
	store, _ := Reports(bp)
	all, err := store.List()
	if err != nil || len(all) != 3 {
		t.Fatalf("reports: %d %v", len(all), err)
	}
	// The first report was unsigned; requiring the key flags exactly that one.
	if issues := report.VerifyChain(all, pub, ""); len(issues) != 1 || issues[0].Seq != 1 {
		t.Fatalf("issues: %+v", issues)
	}
	if issues := report.VerifyChain(all, nil, rep.Report.Hash); len(issues) != 0 {
		t.Fatalf("issues: %+v", issues)
	}
}

func TestReportFlagsLiveChangeDuringDrill(t *testing.T) {
	bp := newFortress(t)
	fr := &fakeRestic{snapDir: t.TempDir()}
	if _, err := Backup(context.Background(), bp, fr); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(bp.VolumePaths()[2], "nested", "data.db")
	rep, err := Breach(context.Background(), bp, fr, DrillOptions{BeforeVerify: func(string) error {
		return os.WriteFile(live, []byte("an app wrote this while the drill ran"), 0o644)
	}})
	if err != nil {
		t.Fatal(err)
	}
	iso := rep.Report.Isolation
	if !iso.LiveChecked || iso.LiveUntouched || iso.LiveChangedTotal != 1 || iso.LiveChanged[0] != live {
		t.Fatalf("isolation %+v", iso)
	}
}

func TestReportRecordsMissedTargets(t *testing.T) {
	bp := newFortress(t)
	bp.Targets.RPO = "1h"
	fr := &fakeRestic{snapDir: t.TempDir()}
	if _, err := Backup(context.Background(), bp, fr); err != nil {
		t.Fatal(err)
	}
	rep, err := Breach(context.Background(), bp, fr, DrillOptions{Now: func() time.Time { return time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	if got := rep.Report.TargetsMissed(); len(got) != 1 || got[0] != "RPO" {
		t.Fatalf("missed %v", got)
	}
}

func keyPair(t *testing.T) (string, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "drill.key")
	pub, _, err := report.GenerateKey(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	priv, _, err := report.LoadPrivateKey(p)
	if err != nil {
		t.Fatal(err)
	}
	return p, pub, priv
}
