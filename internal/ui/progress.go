package ui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

const (
	progressBarWidth = 24
)

// Progress prints lightweight progress updates while checks are executing.
type Progress struct {
	enabled bool
	out     io.Writer
	verbose bool

	mu          sync.Mutex
	phase       string
	total       int
	current     int
	lastPercent int
	lastUpdate  time.Time
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
	p.phase = phase
	p.total = total
	p.current = 0
	p.lastPercent = -1
	p.mu.Unlock()
	p.draw(fmt.Sprintf("starting %s", phase), true)
}

func (p *Progress) Tick(message string) {
	p.mu.Lock()
	p.current++
	p.mu.Unlock()
	p.draw(message, false)
}

func (p *Progress) Message(message string) {
	p.draw(message, false)
}

func (p *Progress) Fail(message string) {
	p.draw(fmt.Sprintf("failed: %s", message), false)
	p.finishLine()
}

func (p *Progress) Done(message string) {
	p.mu.Lock()
	p.current = p.total
	p.mu.Unlock()
	p.draw(fmt.Sprintf("done: %s", message), false)
	p.finishLine()
}

func (p *Progress) draw(message string, force bool) {
	if !p.enabled {
		return
	}

	p.mu.Lock()
	current := p.current
	total := p.total
	p.mu.Unlock()

	now := time.Now()
	if !force && total > 0 {
		percent := percent(current, total)
		p.mu.Lock()
		lastPercent := p.lastPercent
		p.mu.Unlock()
		if percent == lastPercent && now.Sub(p.lastUpdate) < 120*time.Millisecond {
			return
		}
		p.mu.Lock()
		p.lastPercent = percent
		p.lastUpdate = now
		p.mu.Unlock()
	}
	_ = outputLine(p.out, drawBarLine(message, current, total))
}

func (p *Progress) finishLine() {
	if p.out == nil {
		return
	}
	_, _ = fmt.Fprintln(p.out)
}

func outputLine(out io.Writer, line string) error {
	if out == nil {
		return nil
	}
	_, err := fmt.Fprint(out, "\r"+line)
	return err
}

func percent(current, total int) int {
	if total <= 0 {
		return 0
	}
	if current < 0 {
		return 0
	}
	if current > total {
		current = total
	}
	return int(float64(current) * 100.0 / float64(total))
}

func drawBarLine(message string, current, total int) string {
	if total <= 0 {
		return fmt.Sprintf("[....] ...%% %-5s %s", "", message)
	}
	done := percent(current, total)
	filled := (current * progressBarWidth) / total
	if filled > progressBarWidth {
		filled = progressBarWidth
	}
	if filled < 0 {
		filled = 0
	}
	bar := strings.Repeat("=", filled)
	if filled < progressBarWidth {
		bar += ">"
		bar += strings.Repeat(" ", progressBarWidth-filled-1)
	}
	return fmt.Sprintf("[%s] %3d%% %3d/%-3d %s", bar, done, current, total, message)
}
