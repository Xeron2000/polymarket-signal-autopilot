package execution

import "sync"

type KillSwitch struct {
	mu        sync.RWMutex
	triggered bool
	reason    string
}

func NewKillSwitch() *KillSwitch {
	return &KillSwitch{}
}

func (k *KillSwitch) Trigger(reason string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.triggered = true
	k.reason = reason
}

func (k *KillSwitch) Reset() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.triggered = false
	k.reason = ""
}

func (k *KillSwitch) Active() (bool, string) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.triggered, k.reason
}
