package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode/utf8"
	"unicode"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/sapper-sh/sapper/internal/art"
	"github.com/sapper-sh/sapper/internal/termx"
)

const (
	consolePrompt = "sapper> "
	consoleHint   = "/help for commands"
)

// errConsoleExit asks the console loop to restore the terminal and return.
var errConsoleExit = errors.New("console exit")

// consoleCommands are the real sapper operations the console will run.
// Anything else is refused so the console cannot grow a second, fake command set.
var consoleCommands = map[string]bool{
	"status":    true,
	"validate":  true,
	"preflight": true,
	"backup":    true,
	"breach":    true,
	"reports":   true,
}

// consoleReady reports whether bare sapper should stay open after the boot reveal.
// Colour and a real keyboard are required. --plain, --quiet, --json, --no-color,
// NO_COLOR, --no-anim and SAPPER_NO_ANIM keep the previous print-and-exit behaviour.
func (a *app) consoleReady(noAnim bool) bool {
	if a.plain || a.quiet || a.jsonOut || noAnim || os.Getenv("SAPPER_NO_ANIM") != "" {
		return false
	}
	if !a.style.Color || !a.style.TTY {
		return false
	}
	in, inOK := a.stdin.(*os.File)
	out, outOK := a.stdout.(*os.File)
	if !inOK || !outOK {
		return false
	}
	return termx.IsTTY(in) && termx.IsTTY(out)
}

type sessionFlags struct {
	blueprint      string
	jsonOut        bool
	quiet, verbose bool
	noColor, plain bool
}

func (a *app) session() sessionFlags {
	return sessionFlags{a.blueprintPath, a.jsonOut, a.quiet, a.verbose, a.noColor, a.plain}
}

func (a *app) restoreSession(s sessionFlags) {
	a.blueprintPath = s.blueprint
	a.jsonOut = s.jsonOut
	a.quiet = s.quiet
	a.verbose = s.verbose
	a.noColor = s.noColor
	a.plain = s.plain
}

func topCommand(cmd *cobra.Command) string {
	for cmd != nil && cmd.Parent() != nil && cmd.Parent().Parent() != nil {
		cmd = cmd.Parent()
	}
	if cmd == nil {
		return ""
	}
	return cmd.Name()
}

// execConsoleLine runs one console line against the real command tree.
// Session flags (blueprint, --json, --verbose, …) are restored afterwards,
// so a one-off flag on the line does not stick to the next command.
func (a *app) execConsoleLine(ctx context.Context, line string) (string, error) {
	argv, err := splitConsoleArgs(line)
	if err != nil {
		return "", err
	}
	if len(argv) == 0 {
		return "", nil
	}
	switch argv[0] {
	case "exit", "quit":
		return "", errConsoleExit
	case "help", "/help":
		if len(argv) == 1 {
			return consoleUsage(a.blueprintPath), nil
		}
		argv = append(append([]string{}, argv[1:]...), "--help")
	}
	if a.root == nil {
		return "", errors.New("console has no command tree")
	}
	// Repeatable flags carry parse state. Start each submission with a fresh tree.
	session := a.session()
	child := *a
	a = &child
	a.rootCmd()
	a.restoreSession(session)
	a.root.InitDefaultHelpFlag()
	cmd, rest, err := a.root.Find(argv)
	if err != nil {
		return "", fmt.Errorf("%w\n%s", err, consoleHint)
	}
	if cmd == nil || cmd == a.root || !consoleCommands[topCommand(cmd)] {
		return "", fmt.Errorf("unknown command %q\n%s", argv[0], consoleHint)
	}

	helpRequested := false
	if err := cmd.ParseFlags(rest); err != nil {
		if !errors.Is(err, pflag.ErrHelp) {
			return "", err
		}
		helpRequested = true
	}
	if help, _ := cmd.Flags().GetBool("help"); help {
		helpRequested = true
	}
	if helpRequested {
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		_ = cmd.Help()
		return buf.String(), nil
	}
	args := cmd.Flags().Args()
	if err := cmd.ValidateArgs(args); err != nil {
		return "", err
	}
	if err := cmd.ValidateRequiredFlags(); err != nil {
		return "", err
	}
	if err := cmd.ValidateFlagGroups(); err != nil {
		return "", err
	}

	var stdout, stderr bytes.Buffer
	prevOut, prevErr := a.stdout, a.stderr
	prevConsole := a.inConsole
	a.stdout, a.stderr = &stdout, &stderr
	a.inConsole = true
	a.root.SetOut(&stdout)
	a.root.SetErr(&stderr)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	if ctx == nil {
		ctx = context.Background()
	}
	cmd.SetContext(ctx)
	defer func() {
		a.stdout, a.stderr = prevOut, prevErr
		a.inConsole = prevConsole
		a.root.SetOut(prevOut)
		a.root.SetErr(prevErr)
	}()

	var runErr error
	if cmd.RunE != nil {
		runErr = cmd.RunE(cmd, args)
	} else if cmd.Run != nil {
		cmd.Run(cmd, args)
	} else {
		runErr = fmt.Errorf("cannot run %s", cmd.CommandPath())
	}
	text := joinStreams(stdout.String(), stderr.String())
	if runErr != nil && !strings.Contains(text, runErr.Error()) {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += runErr.Error()
	}
	return text, runErr
}

func joinStreams(stdout, stderr string) string {
	stdout = strings.TrimRight(stdout, "\n")
	stderr = strings.TrimRight(stderr, "\n")
	switch {
	case stdout == "":
		return stderr
	case stderr == "":
		return stdout
	default:
		return stdout + "\n" + stderr
	}
}

func consoleUsage(blueprint string) string {
	return fmt.Sprintf(`status     fortress, last backup and last drill
validate   check the blueprint
preflight  check that backup and breach can run
backup     back up every declared volume
breach     restore the latest backup and verify it
reports    list drill reports
help       this text, or help <command>
exit       leave the console

blueprint  %s
%s`, blueprint, consoleHint)
}

// splitConsoleArgs splits a console line on whitespace, keeping quoted words.
func splitConsoleArgs(s string) ([]string, error) {
	var args []string
	var b strings.Builder
	var quote rune
	esc := false
	started := false
	for _, r := range s {
		switch {
		case esc:
			started = true
			b.WriteRune(r)
			esc = false
		case r == '\\' && quote != '\'':
			esc = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
		case r == '\'' || r == '"':
			started = true
			quote = r
		case r == ' ' || r == '\t':
			if started {
				args = append(args, b.String())
				b.Reset()
				started = false
			}
		default:
			started = true
			b.WriteRune(r)
		}
	}
	if esc || quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	if started {
		args = append(args, b.String())
	}
	return args, nil
}

// lineEdit is the console input line.
type lineEdit struct {
	buf []rune
	cursor, recall, pages int
	history []string
	draft string
	pending []byte
}

// commandMatches uses the same allowlist as command dispatch.
func commandMatches(prefix string) []string {
	if prefix == "" {
		return nil
	}
	var matches []string
	for name, allowed := range consoleCommands {
		if allowed && strings.HasPrefix(name, prefix) {
			matches = append(matches, name)
		}
	}
	for _, name := range []string{"help", "/help", "exit", "quit"} {
		if strings.HasPrefix(name, prefix) {
			matches = append(matches, name)
		}
	}
	sort.Strings(matches)
	return matches
}

func commandHint(input string) string {
	if matches := commandMatches(input); len(matches) > 0 {
		if len(matches) == 1 {
			if matches[0] == input {
				return "Enter to run " + input
			}
			return "Tab to complete: " + matches[0]
		}
		return strings.Join(matches, "  ")
	}
	return consoleHint + "  |  Tab to complete"
}

// feed applies one key. submitted is set when the operator presses Enter.
// quit is Ctrl+C, or Ctrl+D on an empty line.
func (e *lineEdit) feed(r rune) (submitted string, okSubmit, quit, redraw bool) {
	switch r {
	case '\t':
		matches := commandMatches(e.String())
		if len(matches) == 1 {
			e.buf = []rune(matches[0])
			e.cursor = len(e.buf)
		}
		return "", false, false, true
	case 3:
		return "", false, true, false
	case 4:
		if len(e.buf) == 0 {
			return "", false, true, false
		}
		return "", false, false, false
	case '\r', '\n':
		s := string(e.buf)
		if strings.TrimSpace(s) != "" && (len(e.history) == 0 || e.history[len(e.history)-1] != s) {
			e.history = append(e.history, s)
			if len(e.history) > 100 { e.history = e.history[1:] }
		}
		e.cursor, e.recall, e.draft = 0, 0, ""
		e.buf = nil
		return s, true, false, true
	case 127, 8:
		if e.cursor > 0 {
			e.cursor--
			e.buf = append(e.buf[:e.cursor], e.buf[e.cursor+1:]...)
		}
		return "", false, false, true
	default:
		if r >= 32 {
			e.buf = append(e.buf, 0)
			copy(e.buf[e.cursor+1:], e.buf[e.cursor:])
			e.buf[e.cursor] = r
			e.cursor++
			return "", false, false, true
		}
		return "", false, false, false
	}
}

// feedBytes decodes a raw chunk. Escape sequences are ignored so arrow keys
// do not land in the line. Completed lines are returned in order; a quit key
// stops the chunk.
func (e *lineEdit) feedBytes(buf []byte) (lines []string, quit bool) {
	buf = append(e.pending, buf...)
	e.pending = nil
	for len(buf) > 0 {
		if buf[0] == 0x1b {
			if len(buf) == 1 { e.pending = buf; break }
			if buf[1] != '[' && buf[1] != 'O' { buf = buf[1:]; continue }
			n := 2
			for n < len(buf) && (buf[n] < 0x40 || buf[n] > 0x7e) { n++ }
			if n == len(buf) {
				if n < 32 { e.pending = buf }
				break
			}
			switch string(buf[2:n+1]) {
			case "A", "B":
				if e.recall == 0 { e.draft = e.String() }
				if buf[n] == 'A' { e.recall = min(len(e.history), e.recall+1) } else { e.recall = max(0, e.recall-1) }
				line := e.draft
				if e.recall > 0 { line = e.history[len(e.history)-e.recall] }
				e.buf = []rune(line)
				e.cursor = len(e.buf)
			case "C": e.cursor = min(len(e.buf), e.cursor+1)
			case "D": e.cursor = max(0, e.cursor-1)
			case "5~": e.pages++
			case "6~": e.pages--
			}
			buf = buf[n+1:]
			continue
		}
		if !utf8.FullRune(buf) { e.pending = buf; break }
		r, size := utf8.DecodeRune(buf)
		if r == utf8.RuneError && size == 1 {
			buf = buf[1:]
			continue
		}
		buf = buf[size:]
		line, ok, q, _ := e.feed(r)
		if ok {
			lines = append(lines, line)
		}
		if q {
			return lines, true
		}
	}
	return lines, false
}

func (e *lineEdit) String() string { return string(e.buf) }

// consoleView is the fixed screen: art on top, log in the middle, prompt at the bottom.
type consoleView struct {
	blueprint          string
	offset, cursorBack int
	status string
	cols, rows         int
	art                []string
	logStart, logEnd   int
	promptRow, hintRow int
}

// consoleLayout pins the boot art to the top and keeps the prompt and the
// /help hint on the last two rows. A tall terminal keeps at least ten rows
// for command output; the art steps down when that budget cannot hold it.
func consoleLayout(cols, rows int) consoleView {
	if cols < 20 {
		cols = 20
	}
	if rows < 6 {
		rows = 6
	}
	// Reserve ten transcript rows after the banner, session rule and prompt.
	const chrome = 3
	bannerBudget := max(1, rows-chrome-10)
	lines := art.Lines(art.FitBox(cols, bannerBudget+1, art.Full))
	return consoleView{
		cols:      cols,
		rows:      rows,
		art:       lines,
		logStart: len(lines) + 2,
		logEnd:    rows - 2,
		promptRow: rows - 1,
		hintRow:   rows,
	}
}

// renderConsole paints the stationary banner when entering or resizing the console.
func renderConsole(v consoleView, log []string, input string) string {
	var b strings.Builder
	b.WriteString("\x1b[2J")
	frame := strings.TrimSuffix(art.Render(v.art, true), "\n")
	for i, line := range strings.Split(frame, "\n") {
		fmt.Fprintf(&b, "\x1b[%d;1H%s", i+1, line)
	}
	b.WriteString(renderConsoleBody(v, log, input))
	return b.String()
}

// Keep the blueprint visible even after a long command scrolls the transcript.
func sessionRule(cols int, blueprint string) string {
	const label = " SESSION "
	room := max(0, cols-1-len(label)-2)
	name := []rune(strings.Map(func(r rune) rune {
		if !unicode.IsPrint(r) {
			return '?'
		}
		return r
	}, blueprint))
	width := textCells(name)
	if width > room {
		name = tailCells(name, max(0, room-1))
		if room > 0 { name = append([]rune{'\u2026'}, name...) }
	}
	fill := max(0, room-textCells(name))
	return "\x1b[38;2;0;140;200m─" + label + "\x1b[0m" + string(name) +
		" \x1b[38;2;0;90;125m" + strings.Repeat("─", fill) + "\x1b[0m"
}

func renderConsoleBody(v consoleView, log []string, input string) string {
	var b strings.Builder
	sep := v.logStart - 1
	fmt.Fprintf(&b, "\x1b[%d;1H\x1b[2K\x1b[2m%s\x1b[0m", sep, sessionRule(v.cols, v.blueprint))
	height := v.logEnd-v.logStart+1
	all := wrappedLog(log, v.cols)
	end := len(all)-min(v.offset, max(0,len(all)-height))
	start := max(0,end-height)
	visible := all[start:end]
	if start > 0 || end < len(all) {
		v.status = fmt.Sprintf("PgUp/PgDn  %d above  %d below", start, len(all)-end)
	}
	for i := 0; i < v.logEnd-v.logStart+1; i++ {
		line := ""
		if i < len(visible) {
			line = visible[i]
			if strings.HasPrefix(line, "> ") {
				line = "\x1b[1;38;2;120;225;255m" + line + "\x1b[0m"
			}
		}
		fmt.Fprintf(&b, "\x1b[%d;1H\x1b[2K%s", v.logStart+i, line)
	}
	b.WriteString(renderPrompt(v, input))
	return b.String()
}

func renderPrompt(v consoleView, input string) string {
	runes := []rune(input)
	pos := len(runes)-min(len(runes),v.cursorBack)
	room := max(0,v.cols-len(consolePrompt)-1)
	left := tailCells(runes[:pos],room)
	right := runes[pos:]
	free := room-textCells(left)
	n := 0
	for n<len(right) && runeCells(right[n])<=free { free-=runeCells(right[n]); n++ }
	shown := string(left)+string(right[:n])
	move := ""
	if n>0 { move=fmt.Sprintf("\x1b[%d;%dH",v.promptRow,len(consolePrompt)+textCells(left)+1) }
	hint := commandHint(input)
	if v.status != "" && input == "" { hint=v.status }
	// Hints contain only ASCII from the allowlist and fixed labels.
	hint = hint[:min(len(hint), v.cols-1)]
	return fmt.Sprintf("\x1b[%d;1H\x1b[2K\x1b[2m%s\x1b[0m\x1b[%d;1H\x1b[48;2;12;35;46m\x1b[2K\x1b[38;2;190;240;255m%s%s\x1b[0m", v.hintRow, hint, v.promptRow, consolePrompt, shown) + move
}

func wrappedLog(log []string,cols int) []string {
	var wrapped []string
	for _, line := range log {
		for _, part := range strings.Split(stripANSI(line), "\n") {
			wrapped = append(wrapped, wrapRunes(part, cols)...)
		}
	}
	return wrapped
}

func wrapRunes(s string, cols int) []string {
	if cols < 1 {
		cols = 1
	}
	r := []rune(s)
	if len(r) == 0 {
		return []string{""}
	}
	var out []string
	start,width:=0,0
	for i,ch:=range r {
		cells:=runeCells(ch)
		if width>0 && width+cells>cols {
			out=append(out,string(r[start:i]))
			start,width=i,0
		}
		width+=cells
	}
	return append(out,string(r[start:]))
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		for i < len(s) && !(s[i] >= 'A' && s[i] <= 'Z' || s[i] >= 'a' && s[i] <= 'z') {
			i++
		}
	}
	return b.String()
}

func appendLog(log []string, text string) []string {
	if strings.TrimSpace(text) == "" {
		return log
	}
	log = append(log, strings.Split(strings.TrimRight(text, "\n"), "\n")...)
	const capLines = 400
	if len(log) > capLines {
		log = log[len(log)-capLines:]
	}
	return log
}
