//go:build windows

package main

import (
	"sync"

	"github.com/EquentR/agent_runtime/app/commands"
	"golang.org/x/sys/windows/svc"
)

func runProgram(mode, name string, run func() error) error {
	if mode != "windows-service" {
		return run()
	}
	if name == "" {
		name = "IceArt"
	}
	return svc.Run(name, &iceArtService{run: run})
}

type iceArtService struct {
	run  func() error
	once sync.Once
}

func (s *iceArtService) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	done := make(chan error, 1)
	go func() { done <- s.run() }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case err := <-done:
			if err != nil {
				return false, 1
			}
			return false, 0
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				status <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				s.once.Do(commands.RequestShutdown)
				if err := <-done; err != nil {
					return false, 1
				}
				return false, 0
			default:
				return false, 1
			}
		}
	}
}
