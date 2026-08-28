package platform

import "sync"

type focusTracker struct {
	mu      sync.Mutex
	history []uint32
	started bool
}

type Native struct {
	power *PowerBlocker
	focus focusTracker
}

func NewNative() *Native {
	return &Native{power: NewPowerBlocker()}
}

func (n *Native) PreventAppSuspension(block bool) error {
	if n == nil || n.power == nil {
		return nil
	}
	return n.power.Set(block)
}

func (n *Native) Close() error {
	if n == nil || n.power == nil {
		return nil
	}
	err := n.power.Close()
	n.power = nil
	return err
}
