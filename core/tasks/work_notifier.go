package tasks

import "sync"

type workNotifier struct {
	mu sync.Mutex
	ch chan struct{}
}

func newWorkNotifier() *workNotifier {
	return &workNotifier{ch: make(chan struct{})}
}

func (n *workNotifier) current() <-chan struct{} {
	if n == nil {
		return nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.ch
}

func (n *workNotifier) notifyAll() {
	if n == nil {
		return
	}
	n.mu.Lock()
	close(n.ch)
	n.ch = make(chan struct{})
	n.mu.Unlock()
}
