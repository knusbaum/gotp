package main

import (
	"fmt"
	"time"
	"reflect"
)



const (
	STRATEGY_ONE_FOR_ONE = iota
	STRATEGY_ONE_FOR_ALL = iota
)

const (
	LIFETIME_PERMANENT = iota
	LIFETIME_TEMPORARY = iota
	LIFETIME_TRANSIONT = iota
)

const (
	ON_PANIC_KILL_PROGRAM = iota
	ON_PANIC_KILL_CHILD = iota
)

type Child interface {
	Run(proc *Proc) error
}

type ChildSpecification struct {
	ChildGen func() Child
	ChildId string
	Lifetime uint8
	ServiceName string // Optional string to register the child as
	OnPanic uint8
}

type ChildProc struct {
	proc *Proc
}

type SupervisorSpec struct {
	ChildSpecs []ChildSpecification
	RestartStrategy uint8
	RestartIntensity int
	RestartPeriod time.Duration
}

type Supervisor struct {
	SupervisorSpec
	supervisorHandle chan State
	childHandles []*ChildProc
	childStates []State
	childRestarts []time.Time
}

func (super *Supervisor) checkRestarts() error {
	now := time.Now()
	recentRestarts := make([]time.Time, 0)
	for _, restart := range super.childRestarts {
		if now.Sub(restart) < super.RestartPeriod {
			recentRestarts = append(recentRestarts, restart)
		}
	}
	super.childRestarts = recentRestarts

	if len(super.childRestarts) >= super.RestartIntensity {
		return fmt.Errorf("Supervisor encountered %d restarts within %s",
			len(super.childRestarts), super.RestartPeriod)
	}
	return nil
}

func (super *Supervisor)makeSelectCases(control chan Control) []reflect.SelectCase {
	cases := make([]reflect.SelectCase, 0, len(super.childHandles) + 1)
	cases = append(cases,
		reflect.SelectCase{
			Dir: reflect.SelectRecv,
			Chan: reflect.ValueOf(control),
		},
	)
	for _, ch := range super.childHandles {
		if ch != nil {
			cases = append(cases,
				reflect.SelectCase{
					Dir: reflect.SelectRecv,
					Chan: reflect.ValueOf(ch.proc.stateHandle),
				},
			)
		}
	}
	return cases
}

func (super *Supervisor) stopAllChildren(state *State) {
	for h := range super.childHandles {
		if super.childHandles[h] != nil {
			result, err := super.childHandles[h].proc.Stop(3 * time.Second)
			if err != nil {
				fmt.Printf("Failed to stop child: %s\n", err)
			}
			super.childHandles[h] = nil
			if state != nil {
				super.childStates[h] = *state
			} else {
				super.childStates[h] = result
			}
		}
	}
}

func (super *Supervisor) restartChildren() {
	for s := range super.ChildSpecs {
		if super.childHandles[s] == nil &&
			super.ChildSpecs[s].Lifetime == LIFETIME_PERMANENT ||
			(super.ChildSpecs[s].Lifetime == LIFETIME_TEMPORARY &&
			 (super.childStates[s].status == STATUS_ERROR ||
			  super.childStates[s].status == STATUS_RESTART)) {
			//fmt.Printf("Restarting child (%s)\n", super.ChildSpecs[chosen].ChildId)
			handle, err := super.ChildSpecs[s].start()
			if err != nil {
				// TODO: DO SOME SORT OF RECOVERY!
				fmt.Printf("Failed to start child: %s\n", err)
			}
			super.childHandles[s] = &handle
			super.childStates[s] = State{}
		}
	}
}

func (super *Supervisor) Start() *Proc {
//	proc := Spawn(func (p *Proc) {
//		superShutdown := false
//		defer func () {
//			if !superShutdown {
//				p.stateHandle <- State { STATUS_ERROR, nil }
//			}
//		}()
//
//		err := super.Run(p)
//		if err != nil {
//			superShutdown = true
//			p.stateHandle <- State { STATUS_ERROR, err }
//		} else {
//			superShutdown = true
//			p.stateHandle <- State { STATUS_SHUTDOWN, nil }
//		}
//	})
	proc := Spawn(super.Run)
	return proc
}

func (super *Supervisor) Run(proc *Proc) error {
	defer func() {
		fmt.Printf("Supervisor<%v> shutting down children.\n", proc.Pid)
		super.stopAllChildren(nil)
	}()

	// Start up the children
	for cSpec := range super.ChildSpecs {
		handle, err := super.ChildSpecs[cSpec].start()
		if err != nil {
			fmt.Printf("Failed to start child: %s\n", err)
			for liveSpec := 0; liveSpec < cSpec; liveSpec++ {
				// TODO: SHUT DOWN OTHER CHILDREN AND SIGNAL ERROR?
			}
			return err
		}
		super.childHandles[cSpec] = &handle
	}

	for {
		cases := super.makeSelectCases(proc.Control)
		chosen, value, _ := reflect.Select(cases)
		if(chosen == 0) {
			// This is the control channel.
			fmt.Printf("Got Control Message: %#v\n", value)
			break
		} else {
			chosen -= 1
		}

		childState := value.Interface().(State)
		if childState.status == STATUS_SHUTDOWN {
			fmt.Printf("Child (%s) proc<%v> shut down.\n", super.ChildSpecs[chosen].ChildId, super.childHandles[chosen].proc.Pid)
		} else if childState.e != nil {
			fmt.Printf("Child (%s) proc<%v> died: %s\n", super.ChildSpecs[chosen].ChildId, super.childHandles[chosen].proc.Pid, childState.e)
		} else {
			fmt.Printf("Child (%s) proc<%v> died for unspecified reasons.\n", super.ChildSpecs[chosen].ChildId, super.childHandles[chosen].proc.Pid)
		}

		super.childHandles[chosen] = nil
		super.childStates[chosen] = childState

		if super.RestartIntensity > 0 {
			super.childRestarts = append(super.childRestarts, time.Now())
			fmt.Printf("RESTARTS: %d\n", len(super.childRestarts))
			err := super.checkRestarts()
			if err != nil {
				return err
			}
		}

		if childState.status != STATUS_SHUTDOWN &&
			super.RestartStrategy == STRATEGY_ONE_FOR_ALL {
			super.stopAllChildren(&State{status:STATUS_RESTART})
		}

		super.restartChildren()
	}

	return nil
}

func (spec *SupervisorSpec) CreateSupervisor() *Supervisor {
	s := &Supervisor{}
	s.supervisorHandle = make(chan State)
	s.childHandles = make([]*ChildProc, len(spec.ChildSpecs))
	s.childStates = make([]State, len(spec.ChildSpecs))
	s.SupervisorSpec = *spec
	return s
}


func (cSpec *ChildSpecification) start() (ch ChildProc, e error) {
	child := cSpec.ChildGen()
	proc := Spawn(func(p *Proc) (e error) {
		defer func() {
			if cSpec.OnPanic == ON_PANIC_KILL_CHILD {
				if r := recover(); r != nil {
					fmt.Println("Recovered in f", r)
					e = fmt.Errorf("%#v", r)
				}
			}
		}()

		return child.Run(p)
	})
	ch.proc = proc
	if cSpec.ServiceName != "" {
		RegisterName(cSpec.ServiceName, ch.proc.Pid)
	}
	return
}

//func (child *ChildProc) stop() {
////	child.proc.Control <- Control{ CONTROL_SHUTDOWN }
////	select {
////	case result := <- child.proc.stateHandle:
////		fmt.Printf("STOPPED CHILDHANDLE: %#v\n", result)
////	case <- time.After(3 * time.Second):
////		panic("Tried to stop ChildProc, but timed out after 3 seconds.")
////	}
//	result, err := child.proc.Stop(3 * time.Second)
//	if err != nil {
//		panic(err.Error())
//	} else {
//		fmt.Printf("STOPPED CHILDHANDLE: %#v\n", result)
//	}
//}

type ServerHandler interface {
	Init(proc *Proc) error
	HandleCast(cast Cast) error
	HandleCall(call Call) (reply interface{}, err error)
	HandleInfo(msg interface {}) error
}

type ServerChild struct {
	handler ServerHandler
}

func (child *ServerChild) Run(proc *Proc) error {
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
