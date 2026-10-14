package fix

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"testing"
)

func TestSystemOwnershipAndAncestors(t *testing.T) {
	s := System{}
	if !s.Mine(int32(os.Getpid())) {
		t.Error("this process is mine")
	}
	if !s.Ancestors()[int32(os.Getppid())] {
		t.Error("the parent is an ancestor")
	}
}

func TestSystemQuitsARealProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("taskkill without /F needs a window to close")
	}
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Skip("no sleep:", err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }() // reap it, so it doesn't linger as a zombie
	pid := int32(cmd.Process.Pid)
	res := runQuit(context.Background(), &Quit{App: "sleep", PIDs: []int32{pid}}, System{}, "linux")
	if res.Status != Done {
		cmd.Process.Kill()
		t.Fatalf("got %+v", res)
	}
	<-done
}

func TestSystemDirSize(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/a", make([]byte, 1000), 0o600); err != nil {
		t.Fatal(err)
	}
	if n, ok := (System{}).DirSize(context.Background(), dir); !ok || n != 1000 {
		t.Errorf("got %d %v", n, ok)
	}
	if _, ok := (System{}).DirSize(context.Background(), dir+"/missing"); ok {
		t.Error("missing dir")
	}
	if fs := (System{}).LargestFiles(dir, 3); len(fs) != 1 || fs[0].Name != "a" {
		t.Errorf("largest: %+v", fs)
	}
}
