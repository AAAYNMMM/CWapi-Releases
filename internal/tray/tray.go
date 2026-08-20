package tray

import "sync"

type implementation interface {
	Start() error
	Close() error
}

// Controller owns the platform tray lifecycle. Product callbacks remain in the
// main Wails application so the tray never becomes a second business-state owner.
type Controller struct {
	impl implementation
	once sync.Once
}

func New(onOpen, onExit func()) *Controller {
	return &Controller{impl: newImplementation(onOpen, onExit)}
}

func (c *Controller) Start() error {
	if c == nil || c.impl == nil {
		return nil
	}
	return c.impl.Start()
}

func (c *Controller) Close() error {
	if c == nil || c.impl == nil {
		return nil
	}
	var err error
	c.once.Do(func() { err = c.impl.Close() })
	return err
}
