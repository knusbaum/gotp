package main

import (
	"fmt"
	"time"
	"testing"
	"math/rand"
)

func TestProcSpawn(t *testing.T) {
	c := make(chan int)
	expectedValue := rand.Int()
	Spawn(func (proc *Proc) error {
		defer close(c)
		c <- expectedValue
		return nil
	})

	select {
	case val := <-c:
		if val != expectedValue {
			t.Errorf("Expected value %d, but have %d.", expectedValue, val)
		}
	case <- time.After(time.Second):
		t.Errorf("Expected value %d, but timed out.", expectedValue)
	}
}

func TestProcWaitShutdown(t *testing.T) {
	proc := Spawn(func (proc *Proc) error {
		<- time.After(100 * time.Millisecond)
		return nil
	})

	state := proc.Wait()
	if state.Err != nil {
		t.Errorf("Unexpected error: %s", state.Err)
	}
	if state.Status != STATUS_SHUTDOWN {
		t.Errorf("Expected status %d, but have %d.", STATUS_SHUTDOWN, state.Status)
	}
}

func TestProcWaitError(t *testing.T) {
	expectedErr := fmt.Errorf("Proc Failed.")
	proc := Spawn(func (proc *Proc) error {
		<- time.After(100 * time.Millisecond)
		return expectedErr
	})

	state := proc.Wait()
	if state.Err != expectedErr {
		t.Errorf("Expected error (%s), but have (%s).", expectedErr, state.Err)
	}
	if state.Status != STATUS_ERROR {
		t.Errorf("Expected status %d, but have %d.", STATUS_ERROR, state.Status)
	}
}


func TestProcMultipleWait(t *testing.T) {
	expectedErr := fmt.Errorf("Proc Failed.")
	proc := Spawn(func (proc *Proc) error {
		<- time.After(100 * time.Millisecond)
		return expectedErr
	})

	state := proc.Wait()
	if state.Err != expectedErr {
		t.Errorf("Expected error (%s), but have (%s).", expectedErr, state.Err)
	}
	if state.Status != STATUS_ERROR {
		t.Errorf("Expected status %d, but have %d.", STATUS_ERROR, state.Status)
	}

	state = proc.Wait()
	if state.Err != expectedErr {
		t.Errorf("Expected error (%s), but have (%s).", expectedErr, state.Err)
	}
	if state.Status != STATUS_ERROR {
		t.Errorf("Expected status %d, but have %d.", STATUS_ERROR, state.Status)
	}

	state = proc.Wait()
	if state.Err != expectedErr {
		t.Errorf("Expected error (%s), but have (%s).", expectedErr, state.Err)
	}
	if state.Status != STATUS_ERROR {
		t.Errorf("Expected status %d, but have %d.", STATUS_ERROR, state.Status)
	}
}

func TestProcStopSuccess(t *testing.T) {
	proc := Spawn(func (proc *Proc) error {
		for {
			select {
			case control := <- proc.Control:
				if control.Action == CONTROL_SHUTDOWN {
					return nil
				} else {
					fmt.Printf("Unhandled Control Message: %#v\n", control)
				}
			}
		}
		return nil
	})

	state, err := proc.Stop(100 * time.Millisecond)
	if err != nil {
		t.Error(err)
	} else {
		if state.Err != nil {
			t.Error(err)
		}
		if state.Status != STATUS_SHUTDOWN {
			t.Errorf("Expected status %d, but have %d.", STATUS_SHUTDOWN, state.Status)
		}
	}
}

func TestProcStopProcError(t *testing.T) {
	expectedErr := fmt.Errorf("Proc Failed.")
	proc := Spawn(func (proc *Proc) error {
		for {
			select {
			case control := <- proc.Control:
				if control.Action == CONTROL_SHUTDOWN {
					return expectedErr
				} else {
					fmt.Printf("Unhandled Control Message: %#v\n", control)
				}
			}
		}
		return nil
	})

	state, err := proc.Stop(100 * time.Millisecond)
	if err != nil {
		t.Error(err)
	} else {
		if state.Err != expectedErr {
			t.Errorf("Expected error (%s), but have (%s).", expectedErr, state.Err)
		}
		if state.Status != STATUS_ERROR {
			t.Errorf("Expected status %d, but have %d.", STATUS_ERROR, state.Status)
		}
	}
}

func TestProcStopProcFail(t *testing.T) {
	proc := Spawn(func (proc *Proc) error {
		for {
			select {
			case control := <- proc.Control:
				if control.Action == CONTROL_SHUTDOWN {
					fmt.Println("I'm a bad proc, and I'm not shutting down.")
				} else {
					fmt.Printf("Unhandled Control Message: %#v\n", control)
				}
			}
		}
		return nil
	})

	_, err := proc.Stop(100 * time.Millisecond)
	if err == nil {
		t.Errorf("Expected error, but got nil.")
	}
}

func TestProcDoubleStop(t *testing.T) {
	proc := Spawn(func (proc *Proc) error {
		for {
			select {
			case control := <- proc.Control:
				if control.Action == CONTROL_SHUTDOWN {
					return nil
				} else {
					fmt.Printf("Unhandled Control Message: %#v\n", control)
				}
			}
		}
		return nil
	})

	_, err := proc.Stop(100 * time.Millisecond)
	if err != nil {
		t.Error(err)
	}

	_, err = proc.Stop(100 * time.Millisecond)
	if err != nil {
		t.Error(err)
	}
}

func TestSendPid(t *testing.T) {
	c := make(chan interface{})
	expectedValue := rand.Int()
	proc := Spawn(func (proc *Proc) error {
		defer close(c)
		for {
			select {
			case <-proc.Control:
				return nil
			case msg := <-proc.Mailbox:
				c <- msg
			}
		}
	})
	defer proc.Stop(time.Second)

	err := SendPid(proc.Pid, expectedValue)
	if err != nil {
		t.Error(err)
	}

	select {
	case val := <-c:
		if val.(int) != expectedValue {
			t.Errorf("Expected value %d, but have %d.", expectedValue, val.(int))
		}
	case <- time.After(time.Second):
		t.Errorf("Expected value %d, but timed out.", expectedValue)
	}
}

func TestRegistry(t *testing.T) {
	proc := Spawn(func (proc *Proc) error {
		for {
			select {
			case <-proc.Control:
				return nil
			}
		}
	})
	defer proc.Stop(time.Second)

	procName := "TestRegistry"
	RegisterName(procName, proc.Pid)
	pid, ok := LookupPid(procName)
	if !ok {
		t.Errorf("Expected to find a Pid registered, but have none.")
		return
	}

	if pid != proc.Pid {
		t.Errorf("Expected Pid<%v> but have Pid<%v>", proc.Pid, pid)
	}
}

func TestSend(t *testing.T) {
	c := make(chan interface{})
	expectedValue := rand.Int()
	proc := Spawn(func (proc *Proc) error {
		defer close(c)
		for {
			select {
			case <-proc.Control:
				return nil
			case msg := <-proc.Mailbox:
				c <- msg
			}
		}
	})
	defer proc.Stop(time.Second)

	procName := "TestSend"
	RegisterName(procName, proc.Pid)

	err := Send(procName, expectedValue)
	if err != nil {
		t.Error(err)
		return
	}

	select {
	case val := <-c:
		if val.(int) != expectedValue {
			t.Errorf("Expected value %d, but have %d.", expectedValue, val.(int))
		}
	case <- time.After(time.Second):
		t.Errorf("Expected value %d, but timed out.", expectedValue)
	}
}


func TestCastPid(t *testing.T) {
	c := make(chan interface{})
	expectedValue := rand.Int()
	proc := Spawn(func (p *Proc) error {
		defer close(c)
		select {
		case <-p.Control:
			return nil
		case msg := <-p.Mailbox:
			switch cast := msg.(type) {
			case Cast:
				c <- cast.msg
				return nil
			}
		}
		return nil
	})
	defer proc.Stop(time.Second)

	proc2 := Spawn(func (p *Proc) error {
		err := p.CastPid(proc.Pid, expectedValue)
		if err != nil {
			t.Error(err)
		}
		return nil
	})
	defer proc2.Stop(time.Second)

	select {
	case val := <-c:
		if val.(int) != expectedValue {
			t.Errorf("Expected value %d, but have %d.", expectedValue, val.(int))
		}
	case <- time.After(time.Second):
		t.Errorf("Expected value %d, but timed out.", expectedValue)
	}
}

func TestCast(t *testing.T) {
	c := make(chan interface{})
	expectedValue := rand.Int()
	proc := Spawn(func (p *Proc) error {
		defer close(c)
		select {
		case <-p.Control:
			return nil
		case msg := <-p.Mailbox:
			switch cast := msg.(type) {
			case Cast:
				c <- cast.msg
				return nil
			}
		}
		return nil
	})
	defer proc.Stop(time.Second)
	procName := "TestCastPid"
	RegisterName(procName, proc.Pid)

	proc2 := Spawn(func (p *Proc) error {
		err := p.Cast(procName, expectedValue)
		if err != nil {
			t.Error(err)
		}
		return nil
	})
	defer proc2.Stop(time.Second)

	select {
	case val := <-c:
		if val.(int) != expectedValue {
			t.Errorf("Expected value %d, but have %d.", expectedValue, val.(int))
		}
	case <- time.After(time.Second):
		t.Errorf("Expected value %d, but timed out.", expectedValue)
	}
}

func TestCallReply(t *testing.T) {
	
}
