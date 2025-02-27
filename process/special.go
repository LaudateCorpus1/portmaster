package process

import (
	"context"
	"strconv"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/safing/portbase/log"
)

const (
	// UnidentifiedProcessID is the PID used for anything that could not be
	// attributed to a PID for any reason.
	UnidentifiedProcessID = -1

	// UndefinedProcessID is not used by any (virtual) process and signifies that
	// the PID is unset.
	UndefinedProcessID = -2

	// NetworkHostProcessID is the PID used for requests served to the network.
	NetworkHostProcessID = -255
)

var (
	// unidentifiedProcess is used when a process cannot be found.
	unidentifiedProcess = &Process{
		UserID:    UnidentifiedProcessID,
		UserName:  "Unknown",
		Pid:       UnidentifiedProcessID,
		ParentPid: UnidentifiedProcessID,
		Name:      "Unidentified Processes",
	}

	// systemProcess is used to represent the Kernel.
	systemProcess = &Process{
		UserID:    SystemProcessID,
		UserName:  "Kernel",
		Pid:       SystemProcessID,
		ParentPid: SystemProcessID,
		Name:      "Operating System",
	}

	getSpecialProcessSingleInflight singleflight.Group
)

// GetUnidentifiedProcess returns the special process assigned to unidentified processes.
func GetUnidentifiedProcess(ctx context.Context) *Process {
	return getSpecialProcess(ctx, unidentifiedProcess)
}

// GetSystemProcess returns the special process used for the Kernel.
func GetSystemProcess(ctx context.Context) *Process {
	return getSpecialProcess(ctx, systemProcess)
}

func getSpecialProcess(ctx context.Context, template *Process) *Process {
	p, _, _ := getSpecialProcessSingleInflight.Do(strconv.Itoa(template.Pid), func() (interface{}, error) {
		// Check if we have already loaded the special process.
		process, ok := GetProcessFromStorage(template.Pid)
		if ok {
			return process, nil
		}

		// Create new process from template
		process = template
		process.FirstSeen = time.Now().Unix()

		// Get profile.
		_, err := process.GetProfile(ctx)
		if err != nil {
			log.Tracer(ctx).Errorf("process: failed to get profile for process %s: %s", process, err)
		}

		// Save process to storage.
		process.Save()
		return process, nil
	})
	return p.(*Process) // nolint:forcetypeassert // Can only be a *Process.
}
