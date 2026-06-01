package tasks

import (
	"testing"
	"time"
)

func TestWorkNotifierNotifyAllClosesCurrentChannelAndRotates(t *testing.T) {
	notifier := newWorkNotifier()
	first := notifier.current()

	select {
	case <-first:
		t.Fatal("first channel is already closed before notify")
	default:
	}

	notifier.notifyAll()

	select {
	case <-first:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("first channel was not closed by notifyAll")
	}

	second := notifier.current()
	if first == second {
		t.Fatal("current channel was not rotated after notifyAll")
	}

	select {
	case <-second:
		t.Fatal("second channel is already closed")
	default:
	}
}

func TestWorkNotifierNotifyAllIsSafeWithoutWaiters(t *testing.T) {
	notifier := newWorkNotifier()

	notifier.notifyAll()
	notifier.notifyAll()

	current := notifier.current()
	select {
	case <-current:
		t.Fatal("current channel is closed after repeated notifyAll calls")
	default:
	}
}
