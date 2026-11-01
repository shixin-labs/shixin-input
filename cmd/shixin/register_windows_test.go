package main

import (
	"errors"
	"reflect"
	"testing"
	"unsafe"
)

func TestRegistrationSteps(t *testing.T) {
	systemFailure := errors.New("UAC cancelled or machine step failed")
	userFailure := errors.New("user step failed")
	for _, tt := range []struct {
		name                                         string
		remove, enabled, cancel                      bool
		systemErr, disableErr, enableErr, cleanupErr error
		steps                                        []string
		wantErr                                      error
	}{
		{name: "register", steps: []string{"system", "enable"}},
		{name: "cancel registration", systemErr: systemFailure, steps: []string{"system"}, wantErr: systemFailure},
		{name: "enable failure", enableErr: userFailure, steps: []string{"system", "enable"}, wantErr: userFailure},
		{name: "unregister enabled", remove: true, enabled: true, steps: []string{"approve", "disable", "system", "cleanup"}},
		{name: "unregister disabled", remove: true, steps: []string{"approve", "system", "cleanup"}},
		{name: "cancel never disables enabled keyboard", remove: true, enabled: true, cancel: true, systemErr: systemFailure, steps: []string{"approve"}, wantErr: systemFailure},
		{name: "machine failure restores enabled keyboard", remove: true, enabled: true, systemErr: systemFailure, steps: []string{"approve", "disable", "system", "enable"}, wantErr: systemFailure},
		{name: "cancel never enables absent keyboard", remove: true, cancel: true, systemErr: systemFailure, steps: []string{"approve"}, wantErr: systemFailure},
		{name: "disable failure preserves machine", remove: true, enabled: true, disableErr: userFailure, steps: []string{"approve", "disable"}, wantErr: userFailure},
		{name: "rollback failure retains both errors", remove: true, enabled: true, systemErr: systemFailure, enableErr: userFailure, steps: []string{"approve", "disable", "system", "enable"}, wantErr: systemFailure},
		{name: "cleanup failure after removal", remove: true, enabled: true, cleanupErr: userFailure, steps: []string{"approve", "disable", "system", "cleanup"}, wantErr: userFailure},
		{name: "TIP removed before COM failure never rolls back", remove: true, enabled: true, systemErr: errMachineRemoved, steps: []string{"approve", "disable", "system", "cleanup"}, wantErr: errMachineRemoved},
		{name: "TIP removed and user cleanup fails", remove: true, enabled: true, systemErr: errMachineRemoved, cleanupErr: userFailure, steps: []string{"approve", "disable", "system", "cleanup"}, wantErr: errMachineRemoved},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var steps []string
			err := registrationSteps(tt.remove, tt.enabled, func(remove bool, before func() error) error {
				if remove != tt.remove {
					t.Fatal("wrong machine operation")
				}
				if remove {
					steps = append(steps, "approve")
					if tt.cancel {
						return tt.systemErr
					}
				}
				if before != nil {
					if err := before(); err != nil {
						return err
					}
				}
				steps = append(steps, "system")
				return tt.systemErr
			}, func(remove bool) error {
				if remove {
					steps = append(steps, "disable")
					return tt.disableErr
				}
				steps = append(steps, "enable")
				return tt.enableErr
			}, func() error { steps = append(steps, "cleanup"); return tt.cleanupErr })
			if !reflect.DeepEqual(steps, tt.steps) || !errors.Is(err, tt.wantErr) {
				t.Fatalf("steps %v, error %v; want %v, %v", steps, err, tt.steps, tt.wantErr)
			}
			if tt.cleanupErr != nil && !errors.Is(err, tt.cleanupErr) {
				t.Fatal("cleanup error lost")
			}
			if tt.remove && tt.systemErr != nil && tt.enableErr != nil && !errors.Is(err, tt.enableErr) {
				t.Fatal("rollback error lost")
			}
		})
	}
}

func TestShellExecuteInfoLayout(t *testing.T) {
	var info shellExecuteInfo
	if unsafe.Sizeof(info) != 112 || unsafe.Offsetof(info.process) != 104 {
		t.Fatal("SHELLEXECUTEINFOW layout differs from the Windows 64-bit ABI")
	}
}
