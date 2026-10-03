//go:build windows

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"testing"
	"time"
	"unsafe"
)

func TestChooserHelper(t *testing.T) {
	if os.Getenv("MATTRIP_CHOOSER_TEST") == "" {
		return
	}
	// Keep the scheduler active while Win32 calls block. The chooser must stay
	// on its creating thread even when other goroutines need to run.
	runtime.GOMAXPROCS(2)
	go func() {
		for {
			runtime.GC()
			runtime.Gosched()
		}
	}()
	if err := chooseAction(); err != nil {
		t.Fatal(err)
	}
	fmt.Printf("selection=%d\n", selection)
}

func TestChooserRespondsToEveryAction(t *testing.T) {
	find := user32.NewProc("FindWindowW")
	owner := user32.NewProc("GetWindowThreadProcessId")
	send := user32.NewProc("SendMessageTimeoutW")
	for _, action := range []int{idRun, idInstall, idExit} {
		t.Run(strconv.Itoa(action), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20 * time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestChooserHelper$")
			cmd.Env = append(os.Environ(), "MATTRIP_CHOOSER_TEST=1")
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer cmd.Process.Kill()
			var hwnd uintptr
			for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
				hwnd, _, _ = find.Call(uintptr(unsafe.Pointer(utf16Ptr("MattRipAllInOneWindow"))), 0)
				var pid uint32
				owner.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
				if hwnd != 0 && pid == uint32(cmd.Process.Pid) {
					break
				}
				hwnd = 0
				time.Sleep(20 * time.Millisecond)
			}
			if hwnd == 0 {
				t.Fatal("chooser window did not appear")
			}
			// Send multiple synchronous messages before clicking each action.
			for i := 0; i < 20; i++ {
				var result uintptr
				ok, _, err := send.Call(hwnd, 0, 0, 0, 2, 2000, uintptr(unsafe.Pointer(&result)))
				if ok == 0 {
					t.Fatalf("chooser stopped responding: %v", err)
				}
				time.Sleep(10 * time.Millisecond)
			}
			var result uintptr
			send.Call(hwnd, wmCommand, uintptr(action), 0, 2, 2000, uintptr(unsafe.Pointer(&result)))
			if err := cmd.Wait(); err != nil {
				t.Fatalf("chooser failed to exit: %v\n%s", err, output.String())
			}
			if !bytes.Contains(output.Bytes(), []byte(fmt.Sprintf("selection=%d", action))) {
				t.Fatalf("wrong action: %s", output.String())
			}
		})
	}
}
