package main

import "testing"

// The task's action quotes the executable — its path may have spaces in it —
// and carries the argument the framework reads to keep the window hidden at
// login.
func TestBuildTaskAction(t *testing.T) {
	for _, tc := range []struct{ exe, want string }{
		{`C:\Program Files\bowel\bowel.exe`, `"C:\Program Files\bowel\bowel.exe" --mygo-opened-at-login`},
		{`C:\bowel\bowel.exe`, `"C:\bowel\bowel.exe" --mygo-opened-at-login`},
	} {
		if got := buildTaskAction(tc.exe); got != tc.want {
			t.Errorf("buildTaskAction(%q) = %q, want %q", tc.exe, got, tc.want)
		}
	}
}

// A switch creates the task of the highest run level at logon, or deletes it.
func TestSchtasksArgs(t *testing.T) {
	on := schtasksArgs(true, `C:\bowel\bowel.exe`)
	want := []string{"/Create", "/TN", loginTask, "/SC", "ONLOGON", "/RL", "HIGHEST", "/F", "/TR", `"C:\bowel\bowel.exe" --mygo-opened-at-login`}
	if len(on) != len(want) {
		t.Fatalf("on = %v, want %v", on, want)
	}
	for i := range want {
		if on[i] != want[i] {
			t.Errorf("on[%d] = %q, want %q", i, on[i], want[i])
		}
	}
	off := schtasksArgs(false, `C:\bowel\bowel.exe`)
	wantOff := []string{"/Delete", "/TN", loginTask, "/F"}
	if len(off) != len(wantOff) {
		t.Fatalf("off = %v, want %v", off, wantOff)
	}
	for i := range wantOff {
		if off[i] != wantOff[i] {
			t.Errorf("off[%d] = %q, want %q", i, off[i], wantOff[i])
		}
	}
}
