package tasks

import "sync"

// EventHub 维护任务级别的内存订阅关系。
//
// 它只负责当前进程内的实时分发，不承担持久化职责；
// 历史事件仍然以数据库中的 task_events 为准。
type EventHub struct {
	mu          sync.RWMutex
	nextID      uint64
	subscribers map[string]map[uint64]chan TaskEvent
}

// NewEventHub 创建一个空的事件订阅中心。
func NewEventHub() *EventHub {
	return &EventHub{
		subscribers: make(map[string]map[uint64]chan TaskEvent),
	}
}

// Subscribe 为指定任务注册一个实时事件订阅。
//
// 返回值中的取消函数会移除订阅并关闭对应 channel。
func (h *EventHub) Subscribe(taskID string) (<-chan TaskEvent, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// 为当前订阅分配自增 id，便于后续精准注销。
	h.nextID++
	id := h.nextID
	ch := make(chan TaskEvent, 32)
	// 按 task 维度维护订阅列表，方便定向广播。
	if h.subscribers[taskID] == nil {
		h.subscribers[taskID] = make(map[uint64]chan TaskEvent)
	}
	h.subscribers[taskID][id] = ch

	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		// 取消订阅时释放 channel，并在最后一个订阅移除后清理 task 桶。
		subs := h.subscribers[taskID]
		if subs == nil {
			return
		}
		if existing, ok := subs[id]; ok {
			delete(subs, id)
			close(existing)
		}
		if len(subs) == 0 {
			delete(h.subscribers, taskID)
		}
	}
}

// Publish 将事件广播给对应任务的所有实时订阅者。
func (h *EventHub) Publish(events ...TaskEvent) {
	if len(events) == 0 {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, event := range events {
		subs := h.subscribers[event.TaskID]
		for _, ch := range subs {
			// 非阻塞发送，避免慢消费者拖垮任务执行主流程。
			select {
			case ch <- event:
			default:
			}
		}
	}
}
