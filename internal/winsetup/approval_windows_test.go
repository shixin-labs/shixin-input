package winsetup

import (
	"golang.org/x/sys/windows"
	"testing"
	"time"
)

func TestMSIApprovalEvents(t *testing.T) {
	a, err := newApproval()
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	done := make(chan error, 1)
	go func() { done <- WaitApproval(a.goName, a.readyName) }()
	result, err := windows.WaitForSingleObject(a.ready, 1000)
	if err != nil || result != windows.WAIT_OBJECT_0 {
		t.Fatalf("no machine acknowledgement: %d %v", result, err)
	}
	select {
	case err := <-done:
		t.Fatalf("machine removal proceeded before user gate: %v", err)
	default:
	}
	if err := windows.SetEvent(a.goEvent); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("machine step did not proceed")
	}
	if err := WaitApproval(a.goName, a.readyName+"other"); err == nil {
		t.Fatal("mismatched events accepted")
	}
}
