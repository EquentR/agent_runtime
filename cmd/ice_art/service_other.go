//go:build !windows

package main

func runProgram(_ string, _ string, run func() error) error {
	return run()
}
