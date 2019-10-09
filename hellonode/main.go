package main

import (
	"context"
	"fmt"
	"github.com/hashicorp/consul/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	hs "grpc-p2p-chat/helloservice"
	"log"
	"net"
	"os"
	"time"
)

type node struct {
	// Self information
	Name string
	Addr string

	// Variables related to `consul`
	SDAddress string
	SDKV      api.KV

	// List of neighbor clients
	Clients map[string]hs.HelloServiceClient
}

// `SayHello` implements `helloservice.HelloServiceClient` interface.
func (n *node) SayHello(ctx context.Context, in *hs.HelloRequest) (*hs.HelloReply, error) {
	return &hs.HelloReply{ Message: "Hello from " + n.Name }, nil
}

// `StartListening` starts listening service.
func (n *node) StartListening() {
	// listening on `n`'s address
	listener, err := net.Listen("tcp", n.Addr)
	if err != nil {
		log.Fatalf("faild to start listening: %v\n", err)
	}
	_n := grpc.NewServer()
	hs.RegisterHelloServiceServer(_n, n)
	reflection.Register(_n)

	if err := _n.Serve(listener); err != nil {
		log.Fatalf("failed to start serving: %v\n", err)
	}
}

func (n *node) registerMyselfToConsul() {
	config := api.DefaultConfig()
	config.Address = n.SDAddress
	consul, err := api.NewClient(config)
	if err != nil {
		log.Panicln("unable to connect `ServiceDiscovery` service of consul")
	}
	kv := consul.KV()
	p := &api.KVPair{Key: n.Name, Value: []byte(n.Addr)}
	_, err = kv.Put(p, nil)
	if err != nil {
		log.Panicln("unable to contact with `ServiceDiscovery` service of consul")
	}
	n.SDKV = *kv
	log.Println("INFO: Successfully registered with consul")
}

// `Start` starts the node.
func (n *node) Start() {
	n.Clients = make(map[string]hs.HelloServiceClient)

	// start listening
	go n.StartListening()

	// register itself to `consul`
	n.registerMyselfToConsul()

	// main loop is here
	for {
		time.Sleep(2 * time.Second)
		n.GreetAllNeighbors()
	}
}

// `SetupNewClient` sets up a new client for contacting the given server.
func (n *node) SetupNewClient(peerName string, peerAddr string) {
	conn, err := grpc.Dial(peerAddr, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("failed to connect to server (name: %s, addr: %s): %v\n", peerName, peerAddr, err)
	}
	defer conn.Close()
	n.Clients[peerName] = hs.NewHelloServiceClient(conn)
	reply, err := n.Clients[peerName].SayHello(context.Background(), &hs.HelloRequest{Name: n.Name})
	if err != nil {
		log.Fatalf("could not greet to server (name: %s, addr: %s): %v\n", peerName, peerAddr, err)
	}
	log.Printf("INFO: Greeting from the other node: %s\n", reply.Message)
}

// `GreetToAllNeighbors` greets all the nodes.
func (n *node) GreetAllNeighbors() {
	kvpairs, _, err := n.SDKV.List("Node", nil)
	log.Println(kvpairs)
	if err != nil {
		log.Panicln(err)
		return
	}
	for _, kventry := range kvpairs {
		log.Println(kventry.Key)
		if kventry.Key == n.Name {
			// It's me!
			continue
		}
		if n.Clients[kventry.Key] == nil {
			log.Println("INFO: new neighbor:", kventry.Key)
			n.SetupNewClient(kventry.Key, string(kventry.Value))
		}
	}
}

func main() {
	args := os.Args[1:]
	if len(args) < 3 {
		fmt.Println("Arguments required: <node name> <listening addr> <consul addr>")
		os.Exit(1)
	}

	name := args[0]
	addr := args[1]
	consul := args[2]
	me := node{
		Name: name,
		Addr: addr,
		SDAddress: consul,
		Clients: nil,
	}
	me.Start()
}
