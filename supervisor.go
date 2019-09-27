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
	LIFETIME_TRANSIENT = iota
)

const (
	ON_PANIC_KILL_PROGRAM = iota
	ON_PANIC_KILL_CHILD = iota
)

type Child interface {
	Run(proc *Proc) error
}

type FuncChild struct {
	f func (proc *Proc) error
}

func (fc *FuncChild) Run(proc *Proc) error {
	if fc.f == nil {
		return fmt.Errorf("FuncChild has no function to execute.")
	}
	return fc.f(proc)
}

type ChildSpec struct {
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
	ChildSpecs []ChildSpec
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

func (super *Supervisor) checkRestartIntensity() error {
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

func (super *Supervisor)makeSelectCases(proc *Proc) []reflect.SelectCase {
	cases := make([]reflect.SelectCase, 0, len(super.childHandles) + 1)
	cases = append(cases,
		reflect.SelectCase{
			Dir: reflect.SelectRecv,
			Chan: reflect.ValueOf(proc.Control),
		},
	)
	cases = append(cases,
		reflect.SelectCase{
			Dir: reflect.SelectRecv,
			Chan: reflect.ValueOf(proc.Mailbox),
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

func (super *Supervisor) childNeedsRestart(i int) bool {
	return super.childHandles[i] == nil &&
		super.ChildSpecs[i].Lifetime == LIFETIME_PERMANENT ||
		(super.ChildSpecs[i].Lifetime == LIFETIME_TEMPORARY &&
		(super.childStates[i].Status == STATUS_ERROR ||
		super.childStates[i].Status == STATUS_RESTART))
}

func (super *Supervisor) restartChildren() {
	for s := range super.ChildSpecs {
		if super.childNeedsRestart(s) {
			fmt.Printf("Restarting child (%s)\n", super.ChildSpecs[s].ChildId)
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
	return Spawn(super.Run)
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
		cases := super.makeSelectCases(proc)
		chosen, value, _ := reflect.Select(cases)
		if(chosen == 0) {
			// This is the control channel.
			fmt.Printf("Got Control Message: %#v\n", value)
			break
		} else if (chosen == 1) {
			// This is the mailbox channel.
			switch mbValue := value.Interface().(type) {
			case Call:
				switch msg := mbValue.msg.(type) {
				case *ChildSpec:
					fmt.Println("LAUNCH NEW CHILD")
					proc, err := super.StartChild(msg)
					if err != nil {
						Reply(mbValue.from, err)
					} else {
						Reply(mbValue.from, proc)
					}
				default:
					Reply(mbValue.from, fmt.Errorf("Can't handle unknown call."))
				}
			}
			continue
		} else {
			chosen -= 2
		}

		childState := value.Interface().(State)
		if childState.Status == STATUS_SHUTDOWN {
			fmt.Printf("Child (%s) proc<%v> shut down.\n", super.ChildSpecs[chosen].ChildId, super.childHandles[chosen].proc.Pid)
		} else if childState.Err != nil {
			fmt.Printf("Child (%s) proc<%v> died: %s\n", super.ChildSpecs[chosen].ChildId, super.childHandles[chosen].proc.Pid, childState.Err)
		} else {
			fmt.Printf("Child (%s) proc<%v> died for unspecified reasons.\n", super.ChildSpecs[chosen].ChildId, super.childHandles[chosen].proc.Pid)
		}

		super.childHandles[chosen] = nil
		super.childStates[chosen] = childState

		if super.ChildSpecs[chosen].Lifetime == LIFETIME_TRANSIENT {
			super.deleteChild(chosen)
			continue
		}

		if super.RestartIntensity > 0 && super.childNeedsRestart(chosen) {
			super.childRestarts = append(super.childRestarts, time.Now())
			fmt.Printf("RESTARTS: %d\n", len(super.childRestarts))
			err := super.checkRestartIntensity()
			if err != nil {
				return err
			}
		}

		if super.childNeedsRestart(chosen) &&
			super.RestartStrategy == STRATEGY_ONE_FOR_ALL {
			super.stopAllChildren(&State{ Status: STATUS_RESTART })
		}

		super.restartChildren()
	}

	return nil
}

func (super *Supervisor) StartChild(childSpec *ChildSpec) (*Proc, error) {
	if childSpec.Lifetime != LIFETIME_TRANSIENT {
		return nil, fmt.Errorf("Can only start children with LIFETIME_TRANSIENT on demand.")
	}

	handle, err := childSpec.start()
	if err != nil {
		return nil, err
	}

	super.ChildSpecs = append(super.ChildSpecs, *childSpec)
	super.childHandles = append(super.childHandles, &handle)
	super.childStates = append(super.childStates, State{})
	return handle.proc, nil
}

func SupervisorStartChildPid(self *Proc, pid Pid, childSpec *ChildSpec) error {
	msg, err := self.CallPid(pid, childSpec)
	if err != nil {
		return err
	}
	switch result := msg.(type) {
	case error:
		return result
	default:
		fmt.Printf("SupervisorStartChildPid: RESULT: %#v\n", result)
	}
	return nil
}

func SupervisorStartChild(self *Proc, name string, childSpec *ChildSpec) error {
	msg, err := self.Call(name, childSpec)
	if err != nil {
		return err
	}
	switch result := msg.(type) {
	case error:
		return result
	default:
		fmt.Printf("SupervisorStartChild: RESULT: %#v\n", result)
	}
	return nil
}

func (super *Supervisor) deleteChild(i int) {
	copy(super.ChildSpecs[i:], super.ChildSpecs[i+1:])
	super.ChildSpecs[len(super.ChildSpecs)-1] = ChildSpec{}
	super.ChildSpecs = super.ChildSpecs[:len(super.ChildSpecs)-1]

	copy(super.childHandles[i:], super.childHandles[i+1:])
	super.childHandles[len(super.childHandles)-1] = nil
	super.childHandles = super.childHandles[:len(super.childHandles)-1]

	copy(super.childStates[i:], super.childStates[i+1:])
	super.childStates[len(super.childStates)-1] = State{}
	super.childStates = super.childStates[:len(super.childStates)-1]
}

func (spec *SupervisorSpec) CreateSupervisor() *Supervisor {
	s := &Supervisor{}
	s.supervisorHandle = make(chan State)
	s.childHandles = make([]*ChildProc, len(spec.ChildSpecs))
	s.childStates = make([]State, len(spec.ChildSpecs))
	s.SupervisorSpec = *spec
	return s
}


func (cSpec *ChildSpec) start() (ch ChildProc, e error) {
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
	fmt.Printf("CHILD<%v> STARTED\n", proc.Pid)
	ch.proc = proc
	if cSpec.ServiceName != "" {
		RegisterName(cSpec.ServiceName, ch.proc.Pid)
	}
	return
}
