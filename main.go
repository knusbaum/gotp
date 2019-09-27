package main

import (
	"fmt"
	"time"
	"math/rand"
)

type TestChild struct {
	counter int
}

func (child *TestChild) Run(proc *Proc) error {
	fmt.Printf("Starting TestChild Proc<%v>\n", proc.Pid)
	for {
		select {

		case <-proc.Control:
			return nil

		case <-time.After(10 * time.Second):
			f := rand.Float64()
			if f > 0.50 {
				return fmt.Errorf("Failed to count to 10.")
			}

			child.counter++
			if child.counter > 10 {
				return nil
			}

		case msg := <-proc.Mailbox:
			fmt.Printf("(%d) GOT: %#v\n", child.counter, msg)
		}
	}
	return nil
}


type CountHandler struct {
	DefaultHandler
	counter int
}

func (h *CountHandler) Init(proc *Proc) error {
	fmt.Printf("Starting up CountHandler Proc<%v>\n", proc.Pid)
	return nil
}

func (h *CountHandler) HandleCall(call Call) (interface{}, error) {
	fmt.Println("Got CALL")
	h.counter++
	return h.counter, nil
}

func (h *CountHandler) HandleInfo(msg interface{}) error {
	fmt.Printf("Got INFO: %#v\n", msg)
	//panic("BAD INFO!")
	return nil
}

type TransientChild struct {}

func (tc *TransientChild) Run(proc *Proc) error {
	fmt.Printf("##########\nSTARTING TransientChild Proc<%v>\n##########\n", proc.Pid)
	<- time.After(3 * time.Second)
	return fmt.Errorf("TRANSIENT FAILURE.")
}

func main() {
	spec := SupervisorSpec{
		ChildSpecs: []ChildSpecification{
			ChildSpecification{
				ChildGen: func() Child { return &TestChild{} },
				ChildId: "Counter",
				Lifetime: LIFETIME_TEMPORARY,
				ServiceName: "CountServer",
			},
			ChildSpecification{
				ChildGen: func() Child { return &Server{ &CountHandler{} } },
				ChildId: "CallCounter",
				Lifetime: LIFETIME_PERMANENT,
				ServiceName: "CallCounter",
				OnPanic: ON_PANIC_KILL_CHILD,
			},
		},
		RestartStrategy: STRATEGY_ONE_FOR_ALL,
		RestartIntensity: 5,
		RestartPeriod: 10 * time.Second,
	}

	spec2 := SupervisorSpec{
		ChildSpecs: []ChildSpecification{
			ChildSpecification{
				ChildGen: func() Child { return spec.CreateSupervisor() },
				ChildId: "CounterSupervisor",
				Lifetime: LIFETIME_PERMANENT,
			},
		},
	}

	supervisor := spec2.CreateSupervisor()
	superProc := supervisor.Start()

	Spawn(func (p *Proc) error {
		err := SupervisorStartChildPid(p, superProc.Pid,
			&ChildSpecification{
				ChildGen: func() Child { return &TransientChild{} },
				ChildId: "Transient",
				Lifetime: LIFETIME_TRANSIENT,
			},
		)
		if err != nil {
			fmt.Printf("Failed to start child: %s\n", err)
		}
		return err
	})
		
	<- time.After(20 * time.Second)
	state, err := superProc.Stop(5 * time.Second)
	fmt.Printf("STATE: %#v, ERR: %#v\n", state, err)

	result := superProc.Wait()
	if result.Status != STATUS_SHUTDOWN {
		if result.Err != nil {
			fmt.Printf("Supervisor failed: %s\n", result.Err)
		} else {
			fmt.Printf("Supervisor failed for unspecified reasons.\n")
		}
	} else {
		fmt.Printf("Supervisor shut down.\n")
	}

//	for i := 0; i < 10; i++{
//		<- time.After(5 * time.Second)
//		pid, ok := LookupPid("CountServer")
//		if ok {
//			fmt.Printf("CountServer has pid %d\n", pid)
//			err := Send("CountServer", &Control{0})
//			if err != nil {
//				fmt.Printf("Failed to control CountServer: %s\n", err)
//			}
//		} else {
//			fmt.Printf("No such server CountServer.\n")
//		}

//		Spawn(func(p *Proc) {
//			reply, err := p.Call("CallCounter", &Control{0})
//			if err != nil {
//				fmt.Printf("Error: %s\n", err)
//			} else {
//				fmt.Printf("GOT REPLY: %v\n", reply)
//			}
//
//			err = Send("CallCounter", &Control{0})
//			if err != nil {
//				fmt.Printf("Error: %s\n", err)
//			}
//
//			err = p.Cast("CallCounter", &Control{0})
//			if err != nil {
//				fmt.Printf("Error: %s\n", err)
//			}
//		})
//
//	}
}
