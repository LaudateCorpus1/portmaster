package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	processInfo "github.com/shirou/gopsutil/process"
)

func checkAndCreateInstanceLock(path, name string) (pid int32, err error) {
	lockFilePath := filepath.Join(dataRoot.Path, path, fmt.Sprintf("%s-lock.pid", name))

	// read current pid file
	data, err := ioutil.ReadFile(lockFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			// create new lock
			return 0, createInstanceLock(lockFilePath)
		}
		return 0, err
	}

	// file exists!
	parsedPid, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		log.Printf("failed to parse existing lock pid file (ignoring): %s\n", err)
		return 0, createInstanceLock(lockFilePath)
	}

	// Check if process exists.
	p, err := processInfo.NewProcess(int32(parsedPid))
	switch {
	case err == nil:
		// Process exists, continue.
	case errors.Is(err, processInfo.ErrorProcessNotRunning):
		// A process with the locked PID does not exist.
		// This is expected, so we can continue normally.
		return 0, createInstanceLock(lockFilePath)
	default:
		// There was an internal error getting the process.
		return 0, err
	}

	// Get the process paths and evaluate and clean them.
	executingBinaryPath, err := p.Exe()
	if err != nil {
		return 0, fmt.Errorf("failed to get path of existing process: %w", err)
	}
	cleanedExecutingBinaryPath, err := filepath.EvalSymlinks(executingBinaryPath)
	if err != nil {
		return 0, fmt.Errorf("failed to evaluate path of existing process: %w", err)
	}
	ownBinaryPath, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("failed to get path of own process: %w", err)
	}
	cleanedOwnBinaryPath, err := filepath.EvalSymlinks(ownBinaryPath)
	if err != nil {
		return 0, fmt.Errorf("failed to evaluate path of own process: %w", err)
	}

	// Check if the binary path matches.
	if cleanedExecutingBinaryPath != cleanedOwnBinaryPath {
		// The process with the locked PID belongs to another binary.
		// As the Portmaster usually starts very early, it will have a low PID,
		// which could be assigned to another process on next boot.
		return 0, createInstanceLock(lockFilePath)
	}

	// Return PID of already running instance.
	return p.Pid, nil
}

func createInstanceLock(lockFilePath string) error {
	// check data root dir
	err := dataRoot.Ensure()
	if err != nil {
		log.Printf("failed to check data root dir: %s\n", err)
	}

	// create lock file
	// TODO: Investigate required permissions.
	err = ioutil.WriteFile(lockFilePath, []byte(fmt.Sprintf("%d", os.Getpid())), 0o0666) //nolint:gosec
	if err != nil {
		return err
	}

	return nil
}

func deleteInstanceLock(path, name string) error {
	lockFilePath := filepath.Join(dataRoot.Path, path, fmt.Sprintf("%s-lock.pid", name))
	return os.Remove(lockFilePath)
}
