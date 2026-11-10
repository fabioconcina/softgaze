package service

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fabioconcina/softgaze/internal/config"
)

func startupDir() string {
	return filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
}

func vbsPath() string {
	return filepath.Join(startupDir(), "softgaze.vbs")
}

// Install creates a VBScript in the Startup folder that launches softgaze
// hidden (no console window). No admin privileges required.
func Install(execPath string) error {
	clearMarkOfTheWeb(execPath)

	if err := os.MkdirAll(config.Dir(), 0700); err != nil {
		return fmt.Errorf("creating log dir: %w", err)
	}
	if err := os.WriteFile(vbsPath(), []byte(vbsScript(execPath, config.LogPath())), 0644); err != nil {
		return fmt.Errorf("writing startup script: %w", err)
	}

	// Start it now
	if err := exec.Command("wscript.exe", vbsPath()).Start(); err != nil {
		return fmt.Errorf("starting softgaze: %w", err)
	}
	return nil
}

func Uninstall() error {
	// Stop the running instance; not this one (taskkill /im would end the
	// uninstall before it removes the script).
	killOtherInstances()

	if err := os.Remove(vbsPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing startup script: %w", err)
	}
	return nil
}

func Status() (string, error) {
	_, err := os.Stat(vbsPath())
	installed := err == nil

	out, _ := exec.Command("tasklist", "/fi", "imagename eq softgaze.exe", "/fo", "csv", "/nh").CombinedOutput()
	running := strings.Contains(string(out), "softgaze.exe")

	switch {
	case installed && running:
		return "running", nil
	case installed:
		return "installed (not running)", nil
	default:
		return "not installed", nil
	}
}

func Restart() error {
	killOtherInstances()
	waitForPortFree(config.Load().Port, 3*time.Second)

	path := vbsPath()
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("startup script not found: is the service installed?")
	}

	// Re-read the VBS to find the exe path and clear MotW
	if data, err := os.ReadFile(path); err == nil {
		if execPath := extractVBSPath(string(data)); execPath != "" {
			clearMarkOfTheWeb(execPath)
		}
	}

	if err := exec.Command("wscript.exe", path).Start(); err != nil {
		return fmt.Errorf("restarting softgaze: %w", err)
	}
	return nil
}

// killOtherInstances kills all softgaze.exe processes except the current one.
func killOtherInstances() {
	myPID := os.Getpid()
	out, err := exec.Command("tasklist", "/fi", "imagename eq softgaze.exe", "/fo", "csv", "/nh").CombinedOutput()
	if err != nil {
		// Without the list: all of them but this one.
		exec.Command("taskkill", "/f", "/im", "softgaze.exe", "/fi", "PID ne "+strconv.Itoa(myPID)).Run()
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Split(strings.TrimSpace(line), "\",\"")
		if len(fields) < 2 {
			continue
		}
		pidStr := strings.Trim(fields[1], "\"")
		pid, err := strconv.Atoi(pidStr)
		if err != nil || pid == myPID {
			continue
		}
		exec.Command("taskkill", "/f", "/pid", strconv.Itoa(pid)).Run()
	}
}

// waitForPortFree polls until the given TCP port is no longer in use,
// up to the specified timeout. This prevents the new service from failing
// to bind after force-killing the old process.
func waitForPortFree(port int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			ln.Close()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// clearMarkOfTheWeb removes the Zone.Identifier alternate data stream
// that Windows applies to downloaded files.
func clearMarkOfTheWeb(path string) {
	os.Remove(path + ":Zone.Identifier")
}
