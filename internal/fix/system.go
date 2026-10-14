package fix

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/process"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
)

// System is the real machine, for both planning (Env) and running (Runner).
type System struct{}

func (System) OS() string { return runtime.GOOS }

func (System) Home() string {
	h, _ := os.UserHomeDir()
	return h
}

func (System) CacheDir() string {
	d, _ := os.UserCacheDir()
	return d
}

func (System) Getenv(k string) string { return os.Getenv(k) }

func (System) LookPath(tool string) bool {
	_, err := exec.LookPath(tool)
	return err == nil
}

func (System) SystemTool(name string) string {
	if runtime.GOOS == "windows" {
		if root := os.Getenv("SystemRoot"); root != "" {
			return filepath.Join(root, "System32", name+".exe")
		}
	}
	return name
}

// dirSizeTimeout bounds how long one folder is measured; a bigger folder
// reports what was counted so far, which is still "at least" right.
const dirSizeTimeout = 5 * time.Second

func (System) DirSize(ctx context.Context, dir string) (uint64, bool) {
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return 0, false
	}
	ctx, cancel := context.WithTimeout(ctx, dirSizeTimeout)
	defer cancel()
	var total uint64
	readable := false
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return fs.SkipAll
		}
		if err != nil {
			return nil // skip what can't be read
		}
		if path == dir {
			readable = true
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += uint64(info.Size())
			}
		}
		return nil
	})
	return total, readable
}

func (System) LargestFiles(dir string, n int) []File {
	es, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var fs []File
	for _, e := range es {
		if !e.Type().IsRegular() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if info, err := e.Info(); err == nil {
			fs = append(fs, File{Name: e.Name(), Size: uint64(info.Size())})
		}
	}
	slices.SortFunc(fs, func(a, b File) int { return cmp.Compare(b.Size, a.Size) })
	return fs[:min(n, len(fs))]
}

func (System) Mine(pid int32) bool {
	p := &process.Process{Pid: pid}
	if runtime.GOOS == "windows" {
		me, err := user.Current()
		owner, err2 := p.Username()
		return err == nil && err2 == nil && strings.EqualFold(owner, me.Username)
	}
	uids, err := p.Uids()
	return err == nil && len(uids) > 0 && int(uids[0]) == os.Getuid()
}

func (System) Ancestors() map[int32]bool {
	out := map[int32]bool{}
	pid := int32(os.Getppid())
	for i := 0; pid > 1 && i < 64 && !out[pid]; i++ {
		out[pid] = true
		next, err := (&process.Process{Pid: pid}).Ppid()
		if err != nil {
			break
		}
		pid = next
	}
	return out
}

func (System) BundleID(app string) string { return collect.BundleID(app) }

// DockerBuildCache asks the Docker daemon how much build cache it could
// reclaim. False if docker isn't running.
func (System) DockerBuildCache(ctx context.Context) (uint64, bool) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "system", "df", "--format", "{{json .}}").Output()
	if err != nil {
		return 0, false
	}
	return parseDockerDF(out)
}

func parseDockerDF(out []byte) (uint64, bool) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		var row struct{ Type, Reclaimable string }
		if json.Unmarshal(sc.Bytes(), &row) != nil || row.Type != "Build Cache" {
			continue
		}
		return parseDockerSize(strings.Fields(row.Reclaimable + " ")[0])
	}
	return 0, false
}

// parseDockerSize reads Docker's human sizes ("1.234GB", "512kB", "0B"),
// which are decimal.
func parseDockerSize(s string) (uint64, bool) {
	units := []struct {
		suffix string
		mult   float64
	}{{"TB", 1e12}, {"GB", 1e9}, {"MB", 1e6}, {"kB", 1e3}, {"KB", 1e3}, {"B", 1}}
	for _, u := range units {
		if n, ok := strings.CutSuffix(s, u.suffix); ok {
			v, err := strconv.ParseFloat(n, 64)
			if err != nil || v < 0 {
				return 0, false
			}
			return uint64(v * u.mult), true
		}
	}
	return 0, false
}

// Runner side.

func (System) Exec(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = nil
	return cmd.CombinedOutput()
}

func (System) Alive(pids []int32) []int32 {
	var out []int32
	for _, pid := range pids {
		if ok, err := process.PidExists(pid); err == nil && ok {
			out = append(out, pid)
		}
	}
	return out
}

// Terminate sends SIGTERM. It's only used off Windows, where gopsutil's
// Terminate would kill outright.
func (System) Terminate(pid int32) error { return (&process.Process{Pid: pid}).Terminate() }

func (System) Parent(pid int32) (int32, error) { return (&process.Process{Pid: pid}).Ppid() }

func (System) Free(mount string) (uint64, bool) {
	u, err := disk.Usage(mount)
	if err != nil {
		return 0, false
	}
	return u.Free, true
}

func (System) Sleep(d time.Duration) { time.Sleep(d) }
