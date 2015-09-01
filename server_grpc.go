package main

import (
	"crypto/rand"
	"crypto/tls"
	"github.com/microplatform-io/platform"
	pb "github.com/microplatform-io/platform-grpc"
	"google.golang.org/grpc"
	"io"
	"log"
	"net"
	"net/url"
	"sync"
)

func ListenForGrpcServer(routerUri string, grpcServerConfig *ServerConfig) {
	defer func() {
		if r := recover(); r != nil {
			log.Println("> grpc server has died: %s", r)
		}
	}()

	lis, err := net.Listen("tcp", ":"+grpcServerConfig.Port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	cert, err := tls.LoadX509KeyPair(SSL_CERT_FILE, SSL_KEY_FILE)
	if err != nil {
		log.Fatalf("> failed to load x509 key pair: %s", err)
	}

	s := grpc.NewServer()
	pb.RegisterRouterServer(s, newServer(routerUri, publisher, subscriber))
	s.Serve(tls.NewListener(lis, &tls.Config{
		Certificates: []tls.Certificate{cert},
		Rand:         rand.Reader,
	}))
}

type server struct {
	routerUri  string
	publisher  platform.Publisher
	subscriber platform.Subscriber
	clients    map[string]pb.Router_RouteServer
	mu         *sync.Mutex
}

func (s *server) runResponder() {
	log.Printf("[server] subscribing to: %s", s.routerUri)

	s.subscriber.Subscribe(s.routerUri, platform.ConsumerHandlerFunc(func(payload []byte) error {
		// log.Printf("[server.Subscriber] got payload: %s", payload)

		platformResponse := &platform.Request{}
		if err := platform.Unmarshal(payload, platformResponse); err != nil {
			log.Printf("[server.Subscriber] failed to unmarshal platform response: %s", err)
			return err
		}

		log.Printf("[server.Subscriber] got platform response: %s", platformResponse)

		lastElementIndex := len(platformResponse.Routing.RouteTo) - 1

		destination := platformResponse.Routing.RouteTo[lastElementIndex]

		// Append the router as the source, and remove the tail destination
		platformResponse.Routing.RouteFrom = append(platformResponse.Routing.RouteFrom, &platform.Route{
			Uri: platform.String(s.routerUri),
		})
		platformResponse.Routing.RouteTo = platformResponse.Routing.RouteTo[:lastElementIndex]

		log.Printf("[server.Subscriber] searching for grpc client by uri: %s", destination.GetUri())

		s.mu.Lock()
		grpcClient, exists := s.clients[destination.GetUri()]
		s.mu.Unlock()

		if exists {
			log.Printf("[server.Subscriber] found grpc client by uri: %s", grpcClient)

			payloadBytes, err := platform.Marshal(platformResponse)
			if err != nil {
				log.Printf("[server.Subscriber] failed to marshal platform response: %s", err)
				return err
			}

			if err := grpcClient.Send(&pb.Request{Payload: payloadBytes}); err != nil {
				log.Printf("[server.Subscriber] failed to send platform response: %s", err)
				return err
			}
		} else {
			log.Printf("[server.Subscriber] failed to discover grpc client by uri: %s", destination.GetUri())
		}

		return nil
	}), 3)

	go s.subscriber.Run()
}

func (s *server) Route(routeServer pb.Router_RouteServer) error {
	clientUuid := "client-" + platform.CreateUUID()

	s.mu.Lock()
	s.clients[clientUuid] = routeServer
	s.mu.Unlock()

	for i := 0; i < 1000; i++ {
		log.Printf("[server.Route] waiting for request from: %s", clientUuid)

		routerRequest, err := routeServer.Recv()
		if err != nil {
			if err == io.EOF {
				log.Println("[server.Route] client has disconnected")
			} else {
				log.Printf("[server.Route] client has disconnected due to unexpected error: %s", err)
			}

			s.mu.Lock()
			delete(s.clients, clientUuid)
			s.mu.Unlock()

			return err
		}

		log.Printf("[server.Route] got router request: %s", routerRequest)

		platformRequest := &platform.Request{}
		if err := platform.Unmarshal(routerRequest.Payload, platformRequest); err != nil {
			log.Printf("[server.Route] failed to unmarshal platform request: %s", err)
			continue
		}

		platformRequest.Routing.RouteFrom = []*platform.Route{
			&platform.Route{
				Uri: platform.String(clientUuid),
			},
			&platform.Route{
				Uri: platform.String(s.routerUri),
			},
		}

		log.Printf("[server.Route] got platform request: %s", platformRequest)

		platformRequestPayload, err := platform.Marshal(platformRequest)
		if err != nil {
			log.Printf("[server.Route] failed to marshal platform request: %s", err)
			continue
		}

		// TODO: Introduce nil / boundary checks
		targetUrl, err := url.Parse(platformRequest.Routing.RouteTo[0].GetUri())
		if err != nil {
			log.Printf("[server.Route] failed to parse the target uri: %s", err)
			continue
		}

		log.Printf("[server.Route] parsed url: %s", targetUrl)

		switch targetUrl.Scheme {
		case "microservice":
			s.publisher.Publish(targetUrl.Path, platformRequestPayload)
		}
	}

	return nil
}

func newServer(routerUri string, publisher platform.Publisher, subscriber platform.Subscriber) *server {
	server := &server{
		routerUri:  routerUri,
		publisher:  publisher,
		subscriber: subscriber,
		clients:    make(map[string]pb.Router_RouteServer),
		mu:         &sync.Mutex{},
	}

	server.runResponder()

	return server
}
