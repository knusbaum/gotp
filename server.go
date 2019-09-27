package main

import (
	"fmt"
)


type ServerHandler interface {
	Init(proc *Proc) error
	HandleCast(cast Cast) error
	HandleCall(call Call) (reply interface{}, err error)
	HandleInfo(msg interface {}) error
}

type Server struct {
	handler ServerHandler
}

func (child *Server) Run(proc *Proc) error {
	err := child.handler.Init(proc)
	if err != nil {
		return err
	}

	for {
		select {
		case <-proc.Control:
			return nil
		case imsg := <-proc.Mailbox:
			switch msg := imsg.(type) {
			case Call:
				reply, err := child.handler.HandleCall(msg)
				if err != nil {
					return err
				}
				if reply != nil {
					err := Reply(msg.from, reply)
					if err != nil {
						// We can ignore this error. If the caller died, we don't care.
						fmt.Printf("Failed to reply: %s\n", err)
					}
				}
			case Cast:
				err := child.handler.HandleCast(msg)
				if err != nil {
					return err
				}
			default:
				err := child.handler.HandleInfo(msg)
				if err != nil {
					return err
				}
			}
		}
	}
}

type DefaultHandler struct {}

func (h *DefaultHandler) Init(proc *Proc) error {
	return nil
}

func (h *DefaultHandler) HandleCast(cast Cast) error {
	fmt.Printf("Got unexpected cast: %#v\n", cast)
	return nil
}

func (h *DefaultHandler) HandleCall(call Call) (interface{}, error) {
	fmt.Printf("Got unexpected call: %#v\n", call)
	return nil, nil
}

func (h *DefaultHandler) HandleInfo(msg interface{}) error {
	fmt.Printf("Got unexpected info: %#v\n", msg)
	return nil
}
