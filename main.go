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
	fmt.Println("Starting TestChild")
	for {
		select {

		case <-proc.Control:
			return nil
			
		case <-time.After(1 * time.Second):
			f := rand.Float64()
			if f > 0.90 {
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

func (h *CountHandler) Init() error {
	fmt.Println("Starting up CountHandler")
	return nil
}

//func (h *CountHandler) HandleCast(cast Cast) error {
//	fmt.Println("Got CAST")
//	return nil
//}

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
				ChildGen: func() Child { return &ServerChild{ &CountHandler{} } },
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

	_, err := spec.Start()
	if err != nil {
		fmt.Printf("ERR: %v\n", err)
		return
	}
	for i := 0; i < 10; i++{
		<- time.After(5 * time.Second)
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

		Spawn(func(p *Proc) {
			reply, err := p.Call("CallCounter", &Control{0})
			if err != nil {
				fmt.Printf("Error: %s\n", err)
			} else {
				fmt.Printf("GOT REPLY: %v\n", reply)
			}

			err = Send("CallCounter", &Control{0})
			if err != nil {
				fmt.Printf("Error: %s\n", err)
			}

			err = p.Cast("CallCounter", &Control{0})
			if err != nil {
				fmt.Printf("Error: %s\n", err)
			}
		})
		
	}
}
