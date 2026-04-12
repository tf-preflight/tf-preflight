package ui

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

// Progress prints lightweight progress updates while checks are executing.
// Output is line-based on purpose so it does not corrupt prompts or Terraform subprocess logs.
type Progress struct {
	enabled bool
	out     io.Writer
	verbose bool

	mu      sync.Mutex
	phase   string
	total   int
	current int
}

func NewProgress(enabled, verbose bool, out io.Writer) *Progress {
	return &Progress{
		enabled: enabled,
		verbose: verbose,
		out:     out,
	}
}

func (p *Progress) Start(phase string, total int) {
	p.mu.Lock()
	p.phase = strings.TrimSpace(phase)
	p.total = total
	p.current = 0
	p.mu.Unlock()
	p.writeProgress("starting")
}

func (p *Progress) Tick(message string) {
	p.mu.Lock()
	p.current++
	p.mu.Unlock()
	p.writeProgress(message)
}

func (p *Progress) Message(message string) {
	if !p.enabled || !p.verbose {
		return
	}
	_ = writeLine(p.out, drawMessageLine(message))
}

func (p *Progress) Fail(message string) {
	p.writeProgress(fmt.Sprintf("failed: %s", message))
}

func (p *Progress) Done(message string) {
	p.mu.Lock()
	if p.total > 0 {
		p.current = p.total
	}
	p.mu.Unlock()
	p.writeProgress(fmt.Sprintf("done: %s", message))
}

func (p *Progress) writeProgress(message string) {
	if !p.enabled {
		return
	}

	p.mu.Lock()
	phase := p.phase
	current := p.current
	total := p.total
	p.mu.Unlock()

	_ = writeLine(p.out, drawProgressLine(phase, message, current, total))
}

func writeLine(out io.Writer, line string) error {
	if out == nil {
		return nil
	}
	_, err := fmt.Fprintln(out, line)
	return err
}

func normalizeProgress(current, total int) (int, int) {
	if total <= 0 {
		if current < 0 {
			return 0, 0
		}
		return current, 0
	}
	if current < 0 {
		current = 0
	}
	if current > total {
		current = total
	}
	return current, total
}

func drawProgressLine(phase, message string, current, total int) string {
	phase = strings.TrimSpace(phase)
	message = strings.TrimSpace(message)
	current, total = normalizeProgress(current, total)

	if total > 0 && phase != "" {
		return fmt.Sprintf("[progress] %s (%d/%d): %s", phase, current, total, message)
	}
	if phase != "" && message != "" {
		return fmt.Sprintf("[progress] %s: %s", phase, message)
	}
	if phase != "" {
		return fmt.Sprintf("[progress] %s", phase)
	}
	return fmt.Sprintf("[progress] %s", message)
}

func drawMessageLine(message string) string {
	return fmt.Sprintf("[info] %s", strings.TrimSpace(message))
}
