package fix

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ag3ntsc4rn/lagdoc/internal/rules"
)

// Runner is what running a fix does to the machine, so Run can be tested.
type Runner interface {
	Exec(ctx context.Context, name string, args ...string) ([]byte, error)
	Alive(pids []int32) []int32
	Parent(pid int32) (int32, error)
	Terminate(pid int32) error // SIGTERM; not used on Windows
	Free(mount string) (uint64, bool)
	Sleep(d time.Duration)
}

type Status int

const (
	Done    Status = iota
	Pending        // asked, but not finished: an app still open, maybe asking to save
	Failed
)

type Result struct {
	Status  Status
	Message string
}

const (
	// How long a quitting app gets before lagdoc reports it still open.
	quitWait = 15 * time.Second
	quitPoll = 250 * time.Millisecond
	// How long a cleanup command may run.
	commandTimeout = 15 * time.Minute
)

// ErrManual is returned for fixes lagdoc never runs.
var ErrManual = errors.New("this fix is for you to do; lagdoc doesn't run it")

func Run(ctx context.Context, f Fix, r Runner) Result {
	switch {
	case f.Manual:
		return Result{Failed, ErrManual.Error()}
	case f.Quit != nil:
		return runQuit(ctx, f.Quit, r, runtime.GOOS)
	case len(f.Command) > 0:
		return runCommand(ctx, f, r)
	}
	return Result{Failed, "nothing to run"}
}

func runQuit(ctx context.Context, q *Quit, r Runner, goos string) Result {
	alive := r.Alive(q.PIDs)
	if len(alive) == 0 {
		return Result{Done, "already closed"}
	}
	switch goos {
	case "darwin":
		// The bundle ID goes in as an argument, never into the script text.
		qctx, cancel := context.WithTimeout(ctx, quitWait)
		_, _ = r.Exec(qctx, "/usr/bin/osascript",
			"-e", "on run argv", "-e", "tell application id (item 1 of argv) to quit", "-e", "end run",
			q.BundleID)
		cancel()
	case "windows":
		// Without /F, taskkill asks each window to close, as clicking × does.
		args := []string{}
		for _, pid := range roots(alive, r) {
			args = append(args, "/PID", strconv.Itoa(int(pid)))
		}
		_, _ = r.Exec(ctx, systemTool("taskkill"), args...)
	default:
		for _, pid := range roots(alive, r) {
			_ = r.Terminate(pid)
		}
	}
	for waited := time.Duration(0); waited < quitWait; waited += quitPoll {
		if alive = r.Alive(alive); len(alive) == 0 {
			return Result{Done, "closed"}
		}
		r.Sleep(quitPoll)
	}
	return Result{Pending, "still open: it may be asking whether to save your work"}
}

// roots are the processes whose parent isn't part of the same app, so a
// browser's main process is asked to quit and closes its own helpers.
func roots(pids []int32, r Runner) []int32 {
	var out []int32
	for _, pid := range pids {
		if ppid, err := r.Parent(pid); err != nil || !slices.Contains(pids, ppid) {
			out = append(out, pid)
		}
	}
	return out
}

func systemTool(name string) string { return System{}.SystemTool(name) }

func runCommand(ctx context.Context, f Fix, r Runner) Result {
	before, haveBefore := r.Free(f.Mount)
	cctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	out, err := r.Exec(cctx, f.Command[0], f.Command[1:]...)
	if err != nil {
		msg := lastLine(out)
		if msg == "" {
			msg = err.Error()
		}
		return Result{Failed, msg}
	}
	if after, ok := r.Free(f.Mount); ok && haveBefore && after > before {
		return Result{Done, "freed " + rules.DiskSize(after-before)}
	}
	return Result{Done, "done"}
}

func lastLine(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	l := strings.TrimSpace(lines[len(lines)-1])
	if len(l) > 200 {
		l = l[:200] + "…"
	}
	return l
}

// CommandLine is a command as shown to a person: system tools by name.
func CommandLine(cmd []string) string {
	if len(cmd) == 0 {
		return ""
	}
	parts := append([]string{displayTool(cmd[0])}, cmd[1:]...)
	return strings.Join(parts, " ")
}

func displayTool(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		p = p[i+1:]
	}
	return strings.TrimSuffix(p, ".exe")
}

func (s Status) String() string { return [...]string{"done", "pending", "failed"}[s] }
