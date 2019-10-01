package main

import (
	"fmt"
	"time"
	"net"
	"bufio"
)

// Proc registry names
var echoClientSupervisor string = "EchoClientSupervisor"
var echoServer string = "EchoServer"


type ClientHandler struct {
	conn net.Conn
}

func (c *ClientHandler) Run(proc *Proc) error {
	fmt.Println("ClientHandler starting.")
	defer c.conn.Close()
	messages := make(chan string)
	go func() {
		defer func() { fmt.Println("Exiting client reader.") }()
		defer close(messages)
		reader := bufio.NewReader(c.conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				fmt.Printf("Error while ReadString: %s\n", err)
				return
			}
			fmt.Printf("GOT: [%s]\n", line)
			messages <- line
		}
	}()

	for {
		select {
		case line, ok := <-messages:
			if !ok {
				fmt.Println("Lost connection to client.")
				return nil
			}
			fmt.Printf("RECEIVED FROM CLIENT: %s\n", line)
			proc.Call("EchoServer", line)
		case <- proc.Control:
			break
		}
	}
}

func connectionListener(p *Proc) error {
	l, err := net.Listen("tcp", "0.0.0.0:9000")
	if err != nil {
		return err
	}

	// Hack to shutdown Listener.
	go func() {
		<- p.Control
		l.Close()
	}()

	// Close the listener when the application closes.
	defer l.Close()
	fmt.Println("Listening on 0.0.0.0:9000")
	for {
		// Listen for an incoming connection.
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		err = SupervisorStartChild(p, echoClientSupervisor,
			&ChildSpec{
				ChildGen: func() Child { return &ClientHandler{ conn } },
				ChildId: "ChildHandler",
				Lifetime: LIFETIME_TRANSIENT,
			},
		)
		if err != nil {
			return err
		}
	}
}

func main() {
	spec := SupervisorSpec{
		ChildSpecs: []ChildSpec{
			ChildSpec{
				ChildGen: func() Child { return &FuncChild{ connectionListener } },
				ChildId: "ConnectionListener",
				Lifetime: LIFETIME_PERMANENT,
			},
			ChildSpec{
				ChildGen: func() Child { return &Server{ &EchoServer{} } },
				ChildId: "EchoServer",
				Lifetime: LIFETIME_PERMANENT,
				ServiceName: "EchoServer",
			},
		},
		RestartStrategy: STRATEGY_ONE_FOR_ALL,
		RestartIntensity: 5,
		RestartPeriod: 10 * time.Second,
	}
	super := spec.CreateSupervisor()
	superProc := super.Start()
	RegisterName(echoClientSupervisor, superProc.Pid)
	superProc.Wait()
}

type EchoServer struct {
	DefaultHandler
	clients []Pid
}

func (server *EchoServer) Init(proc *Proc) error {
	return nil
}

func (server *EchoServer) HandleCall(call Call) (interface{}, error) {
	
	fmt.Printf("EchoServer: GOT CALL: %#v\n", call)
	return 10, nil
}

func (server *EchoServer) HandleCast(cast Cast) error {
	return nil
}
