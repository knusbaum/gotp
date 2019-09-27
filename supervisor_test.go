package main

import (
	"fmt"
	"time"
	"testing"
	"math/rand"
)


func TestSupervisorStartupShutdown(t *testing.T) {
	spec := SupervisorSpec {}
	supervisor := spec.CreateSupervisor()
	superProc := supervisor.Start()

	Spawn(func (p *Proc) error {
		reply, err := p.CallPid(superProc.Pid, 10)
		if err != nil {
			t.Error(err)
		} else {
			switch reply.(type) {
			case error:
			default:
				t.Errorf("Expected error reply, but got %v", reply)
			}
		}
		return nil
	}).Wait()

	state, err := superProc.Stop(5 * time.Second)
	if err != nil {
		t.Error(err)
		return
	}
	if state.Status != STATUS_SHUTDOWN {
		t.Errorf("Expected status %d, but have %d.", STATUS_SHUTDOWN, state.Status)
	}

	//proc.Stop(time.Second)
}

func TestSupervisorLaunchesChild(t *testing.T) {
	c := make(chan int)
	expectedValue := rand.Int()
	f := func (proc *Proc) error {
		c <- expectedValue
		<- proc.Control
		return nil
	}

	spec := SupervisorSpec{
		ChildSpecs: []ChildSpec{
			ChildSpec{
				ChildGen: func () Child { return &FuncChild{ f } },
			},
		},
	}

	supervisor := spec.CreateSupervisor()
	superProc := supervisor.Start()
	defer superProc.Stop(5 * time.Second)

	select {
	case val := <-c:
		if val != expectedValue {
			t.Errorf("Expected value %d, but have %d.", expectedValue, val)
		}
	case <- time.After(time.Second):
		t.Errorf("Expected value %d, but timed out.", expectedValue)
	}
}

func TestSupervisorRestart(t *testing.T) {
	c := make(chan int)
	expectedValue := rand.Int()
	restarts := 0
	f := func (proc *Proc) error {
		c <- expectedValue
		restarts++
		if restarts > 4 {
			<- proc.Control
		}
		return nil
	}

	spec := SupervisorSpec{
		ChildSpecs: []ChildSpec{
			ChildSpec{
				ChildGen: func () Child { return &FuncChild{ f } },
			},
		},
	}

	supervisor := spec.CreateSupervisor()
	superProc := supervisor.Start()
	defer superProc.Stop(5 * time.Second)

	for i := 0; i < 5; i++ {
		select {
		case val := <- c:
			if val != expectedValue {
				t.Errorf("Expected value %d, but have %d (iteration %d)", expectedValue, val, i)
				return
			}
			fmt.Printf("GOT %d\n", val)
		case <- time.After(1 * time.Second):
			t.Errorf("Expected more values, but timed out waiting (iteration %d)", i)
			return
		}
	}
}

func TestSupervisor_ONE_FOR_ONE(t *testing.T) {
	deathSignal := make(chan bool)
	dyingChild := func (proc *Proc) error {
		//fmt.Printf("DyingChild<%v>\n", proc.Pid)
		<- time.After(10 * time.Millisecond)
		select {
		case deathSignal <- true:
		default:
		}
		return nil
	}

	var livingProc *Proc
	livingChild := func (proc *Proc) error {
		//fmt.Printf("LivingChild<%v>\n", proc.Pid)
		livingProc = proc
		<- proc.Control
		//fmt.Printf("LIVING CHILD DYING\n")
		return nil
	}

	spec := SupervisorSpec{
		ChildSpecs: []ChildSpec{
			ChildSpec{
				ChildGen: func () Child { return &FuncChild{ dyingChild } },
			},
			ChildSpec{
				ChildGen: func () Child { return &FuncChild{ livingChild } },
			},
		},
		//RestartStrategy: STRATEGY_ONE_FOR_ALL,
	}

	supervisor := spec.CreateSupervisor()
	superProc := supervisor.Start()
	defer superProc.Stop(5 * time.Second)

	<- deathSignal
	state, err := livingProc.WaitTimeout(100 * time.Millisecond)
	if err == nil {
		t.Errorf("Expected 'livingChild' to survive, but it seems to have died: %#v", state)
	}
}

func TestSupervisor_ONE_FOR_ALL(t *testing.T) {
	deathSignal := make(chan bool)
	dyingChild := func (proc *Proc) error {
		//fmt.Printf("DyingChild<%v>\n", proc.Pid)
		<- time.After(10 * time.Millisecond)
		select {
		case deathSignal <- true:
		default:
		}
		return nil
	}

	var livingProc *Proc
	livingChild := func (proc *Proc) error {
		//fmt.Printf("LivingChild<%v>\n", proc.Pid)
		livingProc = proc
		<- proc.Control
		//fmt.Printf("LIVING CHILD DYING\n")
		return nil
	}

	spec := SupervisorSpec{
		ChildSpecs: []ChildSpec{
			ChildSpec{
				ChildGen: func () Child { return &FuncChild{ dyingChild } },
			},
			ChildSpec{
				ChildGen: func () Child { return &FuncChild{ livingChild } },
			},
		},
		RestartStrategy: STRATEGY_ONE_FOR_ALL,
	}

	supervisor := spec.CreateSupervisor()
	superProc := supervisor.Start()
	defer superProc.Stop(5 * time.Second)

	<- deathSignal
	state, err := livingProc.WaitTimeout(100 * time.Millisecond)
	if err != nil {
		t.Error(err)
	}
	if state.Status != STATUS_SHUTDOWN {
		t.Errorf("Expected status %d, but have %d.", STATUS_SHUTDOWN, state.Status)
	}
}

func TestSupervisorStartChild(t *testing.T) {
	c1 := make(chan bool, 100)
	c2 := make(chan bool, 100)

	transient1 := func (proc *Proc) error {
		fmt.Printf("Transient1<%v>\n", proc.Pid)
		c1 <- true
		return nil
	}

	transient2 := func (proc *Proc) error {
		fmt.Printf("Transient2<%v>\n", proc.Pid)
		c2 <- true
		return nil
	}

	livingProcVal := make(chan *Proc, 100)
	livingChild := func (proc *Proc) error {
		fmt.Printf("livingChild<%v>\n", proc.Pid)
		livingProcVal <- proc
		<- proc.Control
		return nil
	}

	spec := SupervisorSpec{
		ChildSpecs: []ChildSpec{
			ChildSpec{
				ChildGen: func () Child { return &FuncChild{ livingChild } },
				ChildId: "LIVINGCHILD",
			},
		},
		RestartStrategy: STRATEGY_ONE_FOR_ALL,
	}

	supervisor := spec.CreateSupervisor()
	superProc := supervisor.Start()
	defer superProc.Stop(5 * time.Second)

	
	fmt.Println("Start1")
	Spawn(func (p *Proc) error {
		err := SupervisorStartChildPid(p, superProc.Pid, &ChildSpec{
			ChildGen: func () Child { return &FuncChild{ transient1 } },
			ChildId: "TRANSIENT1",
			Lifetime: LIFETIME_TRANSIENT,
		})
		if err != nil {
			t.Error(err)
		}
		return nil
	}).Wait()
	fmt.Println("END Start1")

	fmt.Println("Start2")
	Spawn(func (p *Proc) error {
		err := SupervisorStartChildPid(p, superProc.Pid, &ChildSpec{
			ChildGen: func () Child { return &FuncChild{ transient2 } },
			ChildId: "TRANSIENT2",
			Lifetime: LIFETIME_TRANSIENT,
		})
		if err != nil {
			t.Error(err)
		}
		return nil
	}).Wait()
	fmt.Println("END Start2")

	livingProc := <-livingProcVal
	state, err := livingProc.WaitTimeout(100 * time.Millisecond)
	if err == nil {
		t.Errorf("Expected 'livingChild' to survive, but it seems to have died: %#v", state)
	}

	if len(c1) != 1 {
		t.Errorf("Expected 1 value from transient1, but have %d\n", len(c1))
	}

	if len(c2) != 1 {
		t.Errorf("Expected 1 value from transient1, but have %d\n", len(c1))
	}
}
