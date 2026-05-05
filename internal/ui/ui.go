package ui

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

var output io.Writer = os.Stderr

var (
	bold   = color("\033[1m")
	dim    = color("\033[2m")
	red    = color("\033[31m")
	green  = color("\033[32m")
	yellow = color("\033[33m")
	blue   = color("\033[34m")
	cyan   = color("\033[36m")
	reset  = color("\033[0m")
)

func color(code string) string {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return ""
	}
	return code
}

type Spinner struct {
	out      io.Writer
	frames   []rune
	interval time.Duration
	stop     chan struct{}
	mu       sync.Mutex
	prefix   string
	elapsed  time.Duration
}

func NewSpinner(prefix string) *Spinner {
	return &Spinner{
		out:      os.Stderr,
		frames:   []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'},
		interval: 80 * time.Millisecond,
		stop:     make(chan struct{}),
		prefix:   prefix,
	}
}

func (s *Spinner) Start() {
	go func() {
		i := 0
		start := time.Now()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-ticker.C:
				s.mu.Lock()
				s.elapsed = time.Since(start)
				s.mu.Unlock()
				frame := s.frames[i%len(s.frames)]
				fmt.Fprintf(s.out, "\r%s %s%s%s %s%s%s",
					cyan, string(frame), reset,
					bold, s.prefix, reset,
					dim+fmt.Sprintf(" (%.1fs)", s.elapsed.Seconds())+reset,
				)
				i++
			}
		}
	}()
}

func (s *Spinner) Stop() {
	close(s.stop)
	fmt.Fprint(s.out, "\r"+strings.Repeat(" ", 80)+"\r")
}

type Progress struct {
	out    io.Writer
	total  int
	done   int
	mu     sync.Mutex
	prefix string
	start  time.Time
}

func NewProgress(prefix string, total int) *Progress {
	return &Progress{
		out:    os.Stderr,
		total:  total,
		prefix: prefix,
		start:  time.Now(),
	}
}

func (p *Progress) Increment(label string) {
	p.mu.Lock()
	p.done++
	cur := p.done
	total := p.total
	p.mu.Unlock()

	if total <= 0 {
		fmt.Fprintf(p.out, "  %s %s\n", green+"✓"+reset, label)
		return
	}

	barWidth := 20
	filled := barWidth * cur / total
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	elapsed := time.Since(p.start).Seconds()
	status := green + "✓" + reset
	if strings.Contains(label, "error") || strings.Contains(label, "fail") {
		status = red + "✗" + reset
	}

	fmt.Fprintf(p.out, "\r  %s %s%d/%d%s [%s] %s %.1fs  ",
		status, bold, cur, total, reset, bar, label, elapsed,
	)
}

func (p *Progress) Done() {
	elapsed := time.Since(p.start)
	fmt.Fprintf(p.out, "\n  %s%s Done in %.1fs%s\n", green, bold, elapsed.Seconds(), reset)
}

func PrintHeader(text string) {
	width := 50
	fmt.Fprintf(output, "\n%s%s%s\n", bold+blue, "🛡️  acig", reset)
	fmt.Fprintf(output, "%s%s%s\n", dim, strings.Repeat("─", width), reset)
	fmt.Fprintf(output, "  %s\n\n", text)
}

func PrintStep(icon, text string) {
	fmt.Fprintf(output, "  %s %s\n", icon, text)
}

func PrintStepf(icon, format string, args ...any) {
	fmt.Fprintf(output, "  %s %s\n", icon, fmt.Sprintf(format, args...))
}

func PrintSuccess(text string) {
	fmt.Fprintf(output, "  %s%s %s%s\n", green+bold, "✓", text, reset)
}

func PrintWarning(text string) {
	fmt.Fprintf(output, "  %s%s %s%s\n", yellow+bold, "⚠", text, reset)
}

func PrintError(text string) {
	fmt.Fprintf(output, "  %s%s %s%s\n", red+bold, "✗", text, reset)
}

func PrintVerdict(v VerdictSummary) {
	fmt.Fprintln(output)
	fmt.Fprintf(output, "%s%s──────────────────────────────────%s\n", dim, strings.Repeat("─", 34), reset)

	switch v.Decision {
	case "pass":
		fmt.Fprintf(output, "  %sPASS%s  ", green+bold, reset)
	case "warn":
		fmt.Fprintf(output, "  %sWARN%s  ", yellow+bold, reset)
	case "block":
		fmt.Fprintf(output, "  %sBLOCK%s ", red+bold, reset)
	default:
		fmt.Fprintf(output, "  %s ", v.Decision)
	}

	fmt.Fprintf(output, "risk=%s  findings=%d  cost=$%.4f  duration=%.1fs",
		v.RiskLevel, v.Findings, v.CostUSD, float64(v.DurationMS)/1000.0)
	if v.Suppressions > 0 {
		fmt.Fprintf(output, "  suppressions=%d", v.Suppressions)
	}
	fmt.Fprintln(output)
	fmt.Fprintf(output, "%s%s──────────────────────────────────%s\n", dim, strings.Repeat("─", 34), reset)
}

type VerdictSummary struct {
	Decision     string
	RiskLevel    string
	Findings     int
	CostUSD      float64
	DurationMS   int64
	Suppressions int
}

func PrintFindings(findings []FindingDisplay) {
	if len(findings) == 0 {
		PrintSuccess("No findings. Code looks clean.")
		return
	}
	fmt.Fprintf(output, "\n%sFindings:%s\n", bold, reset)
	for i, f := range findings {
		icon := severityIcon(f.Severity)
		fileStr := ""
		if f.File != "" {
			fileStr = dim + " @ " + f.File + reset
			if f.Line > 0 {
				fileStr = dim + " @ " + f.File + fmt.Sprintf(":%d", f.Line) + reset
			}
		}
		fmt.Fprintf(output, "  %s %d. %s%s%s %s[%s]%s\n", icon, i+1, bold, f.Title, reset, dim, f.Critic, reset)
		if fileStr != "" {
			fmt.Fprintf(output, "     %s\n", fileStr)
		}
	}
	fmt.Fprintln(output)
}

func severityIcon(sev string) string {
	switch sev {
	case "blocking":
		return red + "🚫" + reset
	case "high":
		return red + "●" + reset
	case "medium":
		return yellow + "●" + reset
	case "low":
		return green + "●" + reset
	case "info":
		return dim + "●" + reset
	default:
		return "●"
	}
}

type FindingDisplay struct {
	Critic   string
	Severity string
	Title    string
	File     string
	Line     int
}

func Output() io.Writer {
	return output
}

func ShouldShowUI() bool {
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		return false
	}
	if os.Getenv("CI") != "" {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return isTerminal()
}

func isTerminal() bool {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func SetupLogger(verbose bool) {
	if verbose {
		handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
		slog.SetDefault(slog.New(handler))
	} else if ShouldShowUI() {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	}
}