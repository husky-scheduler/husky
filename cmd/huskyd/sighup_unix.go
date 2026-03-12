//go:build !windows

package daemoncmd

import (
	"os"
	"os/signal"
	"syscall"
)

// watchSIGHUP starts a goroutine that calls onHUP whenever the process
// receives SIGHUP. The goroutine exits when done is closed.
func watchSIGHUP(done <-chan struct{}, onHUP func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	go func() {
		defer signal.Stop(ch)
		for {
			select {
			case <-done:
				return
			case <-ch:
				onHUP()
			}
		}
	}()
}
