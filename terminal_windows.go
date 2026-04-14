//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/UserExistsError/conpty"
)

// conptyReadWriteCloser wraps a ConPty to implement io.ReadWriteCloser.
type conptyReadWriteCloser struct {
	cpty *conpty.ConPty
}

func (c *conptyReadWriteCloser) Read(p []byte) (int, error) {
	return c.cpty.Read(p)
}

func (c *conptyReadWriteCloser) Write(p []byte) (int, error) {
	return c.cpty.Write(p)
}

func (c *conptyReadWriteCloser) Close() error {
	return c.cpty.Close()
}

// findShell returns the best available shell on Windows.
func findShell() string {
	// Prefer pwsh (PowerShell Core) > powershell > cmd
	for _, shell := range []string{"pwsh.exe", "powershell.exe"} {
		if path, err := exec.LookPath(shell); err == nil {
			return path
		}
	}
	return "cmd.exe"
}

// startTerminal creates a new ConPTY-backed terminal session on Windows.
func startTerminal(workDir string) (io.ReadWriteCloser, *exec.Cmd, interface{}, error) {
	shell := findShell()

	cpty, err := conpty.Start(shell, conpty.ConPtyWorkDir(workDir))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("conpty start error: %w", err)
	}

	// Wrap the ConPty process into an exec.Cmd-like structure for process management.
	// ConPty manages the process internally; we create a minimal Cmd to hold the Process.
	pid := cpty.Pid()
	proc, err := os.FindProcess(pid)
	if err != nil {
		cpty.Close()
		return nil, nil, nil, fmt.Errorf("find process %d: %w", pid, err)
	}

	cmd := &exec.Cmd{
		Path:    shell,
		Process: proc,
	}

	rw := &conptyReadWriteCloser{cpty: cpty}
	return rw, cmd, cpty, nil
}

// resizeTerminal resizes the terminal to the given dimensions.
func resizeTerminal(platformData interface{}, cols, rows uint16) error {
	cpty, ok := platformData.(*conpty.ConPty)
	if !ok {
		return nil
	}
	return cpty.Resize(int(cols), int(rows))
}

// restartCommand creates the command to restart the fileserver on Windows.
func restartCommand(exePath, pid string, devMode bool, args ...string) *exec.Cmd {
	exeDir := filepath.Dir(exePath)
	exeName := filepath.Base(exePath)
	quotedArgs := strings.Join(args, "' '")
	if quotedArgs != "" {
		quotedArgs = "'" + quotedArgs + "'"
	}

	var script string
	if devMode {
		script = fmt.Sprintf(
			`do { Start-Sleep -Milliseconds 500 } while (Get-Process -Id %s -ErrorAction SilentlyContinue); Set-Location %q; go build -o %s .; & %q %s`,
			pid, exeDir, exeName, exePath, quotedArgs,
		)
	} else {
		script = fmt.Sprintf(
			`do { Start-Sleep -Milliseconds 500 } while (Get-Process -Id %s -ErrorAction SilentlyContinue); & %q %s`,
			pid, exePath, quotedArgs,
		)
	}
	return exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script)
}

// detachProcess sets process attributes to detach the child process on Windows.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}
