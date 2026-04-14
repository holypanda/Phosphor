//go:build !windows

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/creack/pty"
)

// startTerminal creates a new PTY-backed terminal session on UNIX systems.
// Returns a ReadWriteCloser for I/O, the exec.Cmd, platform-specific data, and any error.
func startTerminal(workDir string) (io.ReadWriteCloser, *exec.Cmd, interface{}, error) {
	cmd := exec.Command("/bin/bash")
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, nil, nil, err
	}
	return ptmx, cmd, ptmx, nil
}

// resizeTerminal resizes the terminal to the given dimensions.
func resizeTerminal(platformData interface{}, cols, rows uint16) error {
	ptmx, ok := platformData.(*os.File)
	if !ok {
		return nil
	}
	return pty.Setsize(ptmx, &pty.Winsize{Cols: cols, Rows: rows})
}

// findGo returns the full path to the go binary, checking PATH and common install locations.
func findGo() string {
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	for _, p := range []string{
		"/usr/local/go/bin/go",
		"/usr/lib/go/bin/go",
		"/snap/bin/go",
		filepath.Join(os.Getenv("HOME"), "go", "bin", "go"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "go"
}

// restartCommand creates the command to restart the fileserver on UNIX.
func restartCommand(exePath, pid string, devMode bool, args ...string) *exec.Cmd {
	exeDir := filepath.Dir(exePath)
	exeName := filepath.Base(exePath)
	quotedArgs := ""
	for _, a := range args {
		quotedArgs += fmt.Sprintf(" %q", a)
	}

	var script string
	if devMode {
		goPath := findGo()
		script = fmt.Sprintf(
			`while kill -0 %s 2>/dev/null; do sleep 0.5; done; cd %q && %s build -o %s . && exec %s%s`,
			pid, exeDir, goPath, exeName, exePath, quotedArgs,
		)
	} else {
		script = fmt.Sprintf(
			`while kill -0 %s 2>/dev/null; do sleep 0.5; done; exec %s%s`,
			pid, exePath, quotedArgs,
		)
	}
	return exec.Command("bash", "-c", script)
}

// detachProcess sets process attributes to detach the child process on UNIX.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
