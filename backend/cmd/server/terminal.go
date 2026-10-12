package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

// guardTerminal restores terminal flags even if a signal interrupts ReadPassword.
// The signal path must exit directly: the main goroutine can be blocked on stdin.
func guardTerminal(input *os.File) (func(), error) {
	fd := int(input.Fd())
	if !term.IsTerminal(fd) {
		return func() {}, nil
	}
	state, err := term.GetState(fd)
	if err != nil {
		return nil, fmt.Errorf("读取终端状态失败: %w", err)
	}
	interrupts := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case sig := <-interrupts:
			_ = term.Restore(fd, state)
			if sig == syscall.SIGTERM {
				os.Exit(143)
			}
			os.Exit(130)
		case <-done:
		}
	}()
	return func() {
		signal.Stop(interrupts)
		close(done)
		_ = term.Restore(fd, state)
	}, nil
}
