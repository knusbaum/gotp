package main

import (
	"fmt"
	"sync"
	//	"sync/atomic"
	"time"
)


type Pid uint64

const (
	CONTROL_SHUTDOWN = iota
)

const (
	STATUS_OK = iota
	STATUS_ERROR = iota
	STATUS_SHUTDOWN = iota
	STATUS_RESTART = iota
)

type State struct {
	status uint8
	e error
}

type Control struct {
	Action uint8
}

type Proc struct {
	Pid Pid
	Mailbox chan interface{}
	Control chan Control
	replySync chan interface{}
	stateHandle chan State
	finalState State
}

var currPid Pid = 0
var pidRegistry map[Pid]*Proc = make(map[Pid]*Proc)
var pidRegistryLock sync.Mutex

var nameRegistry  map[string]Pid = make(map[string]Pid)
var nameRegistryLock sync.Mutex

func Spawn(f func(*Proc) error) *Proc {
	mailbox := make(chan interface{}, 1024)
	control := make(chan Control, 1024)
	replySync := make(chan interface{})
	stateHandle := make(chan State, 1024)
	p := &Proc{
		Mailbox: mailbox,
		Control: control,
		replySync: replySync,
		stateHandle: stateHandle,
	}
	pid := registerProc(p)
	p.Pid = pid
	go func() {
		defer deregisterProc(pid)
		defer close(control)
		defer close(replySync)
		defer close(mailbox)
		defer close(stateHandle)
		procShutdown := false
		defer func () {
			if !procShutdown {
				p.finalState = State { STATUS_ERROR, nil }
				p.stateHandle <- p.finalState
			}
		}()

		err := f(p)
		if err != nil {
			procShutdown = true
			p.finalState = State { STATUS_ERROR, err }
			p.stateHandle <- p.finalState
		} else {
			procShutdown = true
			p.finalState = State { STATUS_SHUTDOWN, nil }
			p.stateHandle <- p.finalState
		}
	}()
	return p
}


func (p *Proc) Wait() State {
	state, ok :=  <- p.stateHandle
	if !ok {
		return p.finalState
	}
	return state
}

func (p *Proc) Stop(timeout time.Duration) (State, error) {
	fmt.Printf("STOPPING Proc<%v>\n", p.Pid)
	p.Control <- Control{ CONTROL_SHUTDOWN }
	select {
	case result := <- p.stateHandle:
		//fmt.Printf("STOPPED PROC: %#v\n", result)
		return result, nil
	case <- time.After(timeout):
		return State{}, fmt.Errorf("Tried to stop Proc<%v>, but timed out after 3 seconds.", p.Pid)
	}
}

func registerProc(proc *Proc) Pid {
	pidRegistryLock.Lock()
	defer pidRegistryLock.Unlock()

	newPid := currPid
	currPid++
	pidRegistry[newPid] = proc
	return newPid
}

func deregisterProc(pid Pid) {
	pidRegistryLock.Lock()
	defer pidRegistryLock.Unlock()
	delete(pidRegistry, pid)
}

func SendPid(pid Pid, val interface{}) error {
	pidRegistryLock.Lock()
	defer pidRegistryLock.Unlock()

	proc, ok := pidRegistry[pid]
	if !ok {
		return fmt.Errorf("No such proc for Pid %v", pid)
	}
	proc.Mailbox <- val
	return nil
}

func RegisterName(name string, pid Pid) {
	nameRegistryLock.Lock()
	defer nameRegistryLock.Unlock()
	//fmt.Printf("Registered PID %v as \"%s\"\n", pid, name)
	nameRegistry[name] = pid
}

func LookupPid(name string) (pid Pid, ok bool) {
	nameRegistryLock.Lock()
	defer nameRegistryLock.Unlock()
	pid, ok = nameRegistry[name]
	return
}

func Send(name string, val interface{}) error {
	pid, ok := LookupPid(name)
	if !ok {
		return fmt.Errorf("No such pid registered for \"%s\"", name)
	}
	return SendPid(pid, val)
}

type Call struct {
	from Pid
	msg interface{}
}

type Cast struct {
	msg interface{}
}

func (p *Proc)CastPid(pid Pid, msg interface{}) error {
	cast := Cast{ msg }
	return SendPid(pid, cast)
}

func (p *Proc)Cast(name string, msg interface{}) error {
	pid, ok := LookupPid(name)
	if !ok {
		return fmt.Errorf("No such pid registered for \"%s\"", name)
	}
	return p.CastPid(pid, msg)
}

func Reply(pid Pid, msg interface{}) error {
	pidRegistryLock.Lock()
	defer pidRegistryLock.Unlock()

	proc, ok := pidRegistry[pid]
	if !ok {
		return fmt.Errorf("No such proc for Pid %v", pid)
	}
	proc.replySync <- msg
	return nil
}

func (p *Proc)waitReply() interface{} {
	msg := <- p.replySync
	return msg
}

func (p *Proc) CallPid(pid Pid, msg interface{}) (interface{}, error) {
	call := Call{p.Pid, msg}
	err := SendPid(pid, call)
	if err != nil {
		return nil, err
	}

	result := p.waitReply()
	return result, nil
}

func (p *Proc) Call(name string, msg interface{}) (interface{}, error) {
	pid, ok := LookupPid(name)
	if !ok {
		return nil, fmt.Errorf("No such pid registered for \"%s\"", name)
	}
	return p.CallPid(pid, msg)
}
