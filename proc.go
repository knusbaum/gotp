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
	Status uint8
	Err error
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
	stateLock sync.Mutex
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
		// Need stateLock to avoid races between stateHandle, finalState and channel closes.
		p.stateLock.Lock()
		defer p.stateLock.Unlock()
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

func (p *Proc) WaitTimeout(timeout time.Duration) (State, error) {
	select {
	case state, ok := <- p.stateHandle:
		if !ok {
			return p.finalState, nil
		}
		return state, nil
	case <- time.After(timeout):
		return State{}, fmt.Errorf("Timed out after %s", timeout.String())
	}
}

func (p *Proc) Stop(timeout time.Duration) (State, error) {
	// Make sure the proc state doesn't change between checking and sending to Control.
	p.stateLock.Lock()
	select {
	case state, ok := <- p.stateHandle:
		fmt.Printf("Proc<%v> Already Stopped.\n", p.Pid)
		if !ok {
			p.stateLock.Unlock()
			return p.finalState, nil
		} else {
			p.stateLock.Unlock()
			return state, nil
		}
	default:
	}

	fmt.Printf("STOPPING Proc<%v>\n", p.Pid)
	p.Control <- Control{ CONTROL_SHUTDOWN }
	p.stateLock.Unlock()
	select {
	case result := <- p.stateHandle:
		//fmt.Printf("STOPPED PROC: %#v\n", result)
		return result, nil
	case <- time.After(timeout):
		return State{}, fmt.Errorf("Tried to stop Proc<%v>, but timed out after %s.", p.Pid, timeout.String())
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
	select {
	case proc.replySync <- msg:
		return nil
	case <- time.After(1 * time.Second):
		// This *shouldn't* happen, but we want to avoid deadlock
		return fmt.Errorf("Failed to reply. Calling process not ready.")
	}
}

func (p *Proc) waitReply(timeout time.Duration) (interface{}, error) {
	select {
	case msg := <- p.replySync:
		return msg, nil
	case <- time.After(timeout):
		return nil, fmt.Errorf("Timed out after %s waiting for reply.", timeout.String())
	}
}

func (p *Proc) CallPidTimeout(pid Pid, msg interface{}, timeout time.Duration) (interface{}, error) {
	call := Call{p.Pid, msg}
	err := SendPid(pid, call)
	if err != nil {
		return nil, err
	}
	return p.waitReply(timeout)
}

func (p *Proc) CallTimeout(name string, msg interface{}, timeout time.Duration) (interface{}, error) {
	pid, ok := LookupPid(name)
	if !ok {
		return nil, fmt.Errorf("No such pid registered for \"%s\"", name)
	}
	return p.CallPidTimeout(pid, msg, timeout)
}

func (p *Proc) CallPid(pid Pid, msg interface{}) (interface{}, error) {
	return p.CallPidTimeout(pid, msg, 5 * time.Second)
}

func (p *Proc) Call(name string, msg interface{}) (interface{}, error) {
	return p.CallTimeout(name, msg, 5 * time.Second)
}
