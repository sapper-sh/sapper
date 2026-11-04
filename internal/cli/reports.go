package cli

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/sapper-unbuilt/sapper/internal/ops"
	"github.com/sapper-unbuilt/sapper/internal/report"
)

func (a *app) reportsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "reports",
		Short: "List breach drill reports",
		Long: `reports lists the evidence reports saved by every breach drill, oldest first.
Each report records the sha256 of the report before it, so the list is a hash
chain: "sapper reports verify" recomputes every hash and finds edited, removed or
reordered reports.`,
		Example: `  sapper reports
  sapper reports show        # the latest report
  sapper reports show 12 --markdown
  sapper reports verify --pub-key drill.key.pub --head 4be0c1d2`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			all, err := a.reportEntries()
			if err != nil {
				return err
			}
			if a.jsonOut {
				type row struct {
					Seq      int       `json:"seq"`
					Started  time.Time `json:"started"`
					Verdict  string    `json:"verdict"`
					Snapshot string    `json:"snapshot"`
					Hash     string    `json:"hash"`
					Signed   bool      `json:"signed"`
					Path     string    `json:"path"`
					Error    string    `json:"error,omitempty"`
				}
				rows := []row{}
				for _, e := range all {
					r := row{Seq: e.Seq, Path: e.Path}
					if e.Err != nil {
						r.Error = e.Err.Error()
					} else {
						r.Started, r.Verdict, r.Snapshot, r.Hash, r.Signed = e.Report.Started, e.Report.Verdict, e.Report.Snapshot.ID, e.Report.Hash, e.Report.Signature != nil
					}
					rows = append(rows, r)
				}
				return a.printJSON(rows)
			}
			if len(all) == 0 {
				fmt.Fprintln(a.stdout, "No drill reports yet. Run `sapper breach` to make the first one.")
				return nil
			}
			tw := tabwriter.NewWriter(a.stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "#\tSTARTED\tVERDICT\tSNAPSHOT\tPROBLEMS\tHASH\tSIGNED")
			for _, e := range all {
				if e.Err != nil {
					fmt.Fprintf(tw, "%d\t-\tunreadable\t-\t-\t-\t-\n", e.Seq)
					continue
				}
				r := e.Report
				signed := "no"
				if r.Signature != nil {
					signed = report.Clean(r.Signature.KeyID[:min(8, len(r.Signature.KeyID))])
				}
				fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%d\t%s\t%s\n", r.Seq, r.Started.Local().Format("2006-01-02 15:04"), report.Clean(r.Verdict),
					orDash(report.Clean(r.Snapshot.Short())), r.Evidence.FindingsTotal, report.Clean(r.Hash[:min(12, len(r.Hash))]), signed)
			}
			return tw.Flush()
		},
	}
	c.AddCommand(a.reportsShowCmd(), a.reportsVerifyCmd())
	return c
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func (a *app) reportEntries() ([]report.Entry, error) {
	bp, err := a.load()
	if err != nil {
		return nil, err
	}
	store, err := ops.Reports(bp)
	if err != nil {
		return nil, err
	}
	return store.List()
}

func (a *app) reportsShowCmd() *cobra.Command {
	var md, brief bool
	c := &cobra.Command{
		Use:   "show [number]",
		Short: "Print one drill report (default: the latest)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			all, err := a.reportEntries()
			if err != nil {
				return err
			}
			if len(all) == 0 {
				return withCode(ExitError, fmt.Errorf("no drill reports yet; run `sapper breach`"))
			}
			e := all[len(all)-1]
			if len(args) == 1 {
				n, err := strconv.Atoi(args[0])
				if err != nil || n < 1 {
					return usagef("report number must be a positive integer, got %q", args[0])
				}
				found := false
				for _, x := range all {
					if x.Seq == n {
						e, found = x, true
					}
				}
				if !found {
					return withCode(ExitError, fmt.Errorf("no report #%d; `sapper reports` lists them", n))
				}
			}
			if e.Err != nil {
				return fmt.Errorf("report #%d (%s) cannot be read: %v", e.Seq, e.Path, e.Err)
			}
			switch {
			case a.jsonOut:
				return a.printJSON(e.Report)
			case md:
				fmt.Fprint(a.stdout, report.Markdown(e.Report))
			case brief:
				fmt.Fprintln(a.stdout, report.Brief(e.Report))
			default:
				fmt.Fprint(a.stdout, report.Terminal(e.Report, a.reportView(e.Path)))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&md, "markdown", false, "print the Markdown copy")
	c.Flags().BoolVar(&brief, "brief", false, "print one line")
	c.MarkFlagsMutuallyExclusive("markdown", "brief")
	return c
}

func (a *app) reportsVerifyCmd() *cobra.Command {
	var pubPath, head string
	c := &cobra.Command{
		Use:   "verify",
		Short: "Check the report chain for edited, missing or reordered reports",
		Long: `verify recomputes the hash of every saved report, checks that each names the
hash of the report before it, that numbers run from 1 without gaps, and that any
signatures are valid.

The chain alone cannot tell that the newest reports were deleted, and anyone who
can write the state directory can rebuild an unsigned chain from scratch. For
stronger guarantees, keep the signing key off this host and pass --pub-key, and
record the latest hash somewhere else and pass it as --head.

Exits 0 when the chain is intact and 3 when it is not.`,
		Example: `  sapper reports verify
  sapper reports verify --pub-key drill.key.pub
  sapper reports verify --head 4be0c1d2e3f4`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			var pub ed25519.PublicKey
			if pubPath != "" {
				k, err := report.LoadPublicKey(pubPath)
				if err != nil {
					return withCode(ExitUsage, fmt.Errorf("--pub-key: %w", err))
				}
				pub = k
			}
			head = strings.ToLower(strings.TrimSpace(head))
			if head != "" && (len(head) < 8 || len(head) > 64 || strings.Trim(head, "0123456789abcdef") != "") {
				return usagef("--head must be 8 to 64 hex characters of a report hash")
			}
			all, err := a.reportEntries()
			if err != nil {
				return err
			}
			issues := report.VerifyChain(all, pub, head)
			last := ""
			if n := len(all); n > 0 && all[n-1].Report != nil {
				last = all[n-1].Report.Hash
			}
			if a.jsonOut {
				if e := a.printJSON(map[string]any{"ok": len(issues) == 0, "reports": len(all), "head": last, "issues": append([]report.Issue{}, issues...)}); e != nil {
					return e
				}
			} else if len(issues) == 0 {
				what := "unsigned or self-signed"
				if pub != nil {
					what = "signed by key " + report.KeyID(pub)
				}
				if len(all) == 0 {
					fmt.Fprintln(a.stdout, "No reports to verify.")
				} else {
					fmt.Fprintf(a.stdout, "Chain intact: %s, every hash matches, %s.\n", plural(len(all), "report"), what)
					fmt.Fprintf(a.stdout, "Latest hash %s\n", report.Clean(last))
					fmt.Fprintln(a.stdout, "Record it somewhere else and pass it as --head to catch deleted reports.")
				}
			} else {
				fmt.Fprintf(a.stdout, "Chain broken: %s in %s.\n", plural(len(issues), "problem"), plural(len(all), "report"))
				for _, i := range issues {
					label := "chain"
					if i.Seq > 0 {
						label = "#" + strconv.Itoa(i.Seq)
					}
					fmt.Fprintf(a.stdout, "  %-6s %s\n", label, report.Clean(i.Problem))
				}
				fmt.Fprintln(a.stdout, "Treat these reports as untrusted. Compare them with any copies kept elsewhere, and run `sapper breach` to get a fresh result.")
			}
			if len(issues) > 0 {
				return withCode(ExitBreached, fmt.Errorf("report chain failed verification"))
			}
			return nil
		},
	}
	c.Flags().StringVar(&pubPath, "pub-key", "", "require every report to be signed by this ed25519 public key")
	c.Flags().StringVar(&head, "head", "", "expected hash (or prefix) of the newest report")
	return c
}

func (a *app) keygenCmd() *cobra.Command {
	var out string
	c := &cobra.Command{
		Use:   "keygen",
		Short: "Create an ed25519 key pair for signing drill reports",
		Long: `keygen writes a new ed25519 private key (mode 0600) and its public key next to
it with a .pub suffix. It never overwrites an existing key and never prints the
private key. Pass the private key to "sapper breach --sign-key" and the public key
to "sapper reports verify --pub-key".

A signature proves a report was not changed after signing only if the private key
is out of reach of whoever could change the report. Keep it readable only by the
user that runs drills, and keep the public key somewhere else as well.`,
		Example: `  sapper keygen --out ~/.config/sapper/drill.key`,
		Args:    cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			pub, pubPath, err := report.GenerateKey(out, nil)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(map[string]string{"private_key_file": out, "public_key_file": pubPath, "key_id": report.KeyID(pub)})
			}
			fmt.Fprintf(a.stdout, "private key  %s (mode 0600; not printed)\n", out)
			fmt.Fprintf(a.stdout, "public key   %s\n", pubPath)
			fmt.Fprintf(a.stdout, "key id       %s\n", report.KeyID(pub))
			return nil
		},
	}
	c.Flags().StringVarP(&out, "out", "o", defaultKeyPath(), "private key file to create")
	return c
}

// defaultKeyPath keeps the key in the user's config directory rather than the current
// directory, which is often a git checkout where a key is easy to commit by accident.
func defaultKeyPath() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "sapper", "drill.key")
	}
	return "sapper-drill.key"
}
