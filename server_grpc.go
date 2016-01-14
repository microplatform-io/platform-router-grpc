package main

import (
	"crypto/rand"
	"crypto/tls"
	"io"
	"log"
	"net"
	"strings"
	"time"

	"github.com/kr/pretty"
	"github.com/microplatform-io/platform"
	pb "github.com/microplatform-io/platform-grpc"
	"google.golang.org/grpc"
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

	router := platform.NewStandardRouter(publisher, subscriber)
	router.SetHeartbeatTimeout(7 * time.Second)

	pb.RegisterRouterServer(s, newServer(router))
	s.Serve(tls.NewListener(lis, &tls.Config{
		Certificates: []tls.Certificate{cert},
		Rand:         rand.Reader,
	}))
}

type server struct {
	router platform.Router
}

func (s *server) Route(routeServer pb.Router_RouteServer) error {
	clientUuid := "client-" + platform.CreateUUID()

	closed := false

	for {
		log.Printf("[server.Route] %s - waiting for request", clientUuid)

		routerRequest, err := routeServer.Recv()
		if err != nil {
			closed = true

			if err == io.EOF {
				log.Printf("[server.Route] %s - client has disconnected", clientUuid)
			} else {
				log.Printf("[server.Route] %s - client has disconnected due to unexpected error: %s", clientUuid, err)
			}

			return err
		}

		log.Printf("[server.Route] %s -  got router request: %s", clientUuid, routerRequest)

		platformRequest := &platform.Request{}
		if err := platform.Unmarshal(routerRequest.Payload, platformRequest); err != nil {
			log.Printf("[server.Route] %s -  failed to unmarshal platform request: %s", clientUuid, err)
			continue
		}

		if platformRequest.Routing == nil {
			platformRequest.Routing = &platform.Routing{}
		}

		if !platform.RouteToSchemeMatches(platformRequest, "microservice") {
			log.Printf("[server.Route] %s -  unsupported scheme provided: %s", clientUuid, platformRequest.Routing.RouteTo)
			continue
		}

		platformRequest.Routing.RouteFrom = []*platform.Route{
			&platform.Route{
				Uri: platform.String("client://" + clientUuid),
			},
		}

		requestUuidPrefix := clientUuid + "::"

		platformRequest.Uuid = platform.String(requestUuidPrefix + platformRequest.GetUuid())

		responses, timeout := s.router.Route(platformRequest)

		go func() {
			for {
				select {
				case response := <-responses:
					log.Printf("[server.Route] %s - got a response for request: %s", clientUuid, platformRequest.GetUuid())
					pretty.Println(response)

					response.Uuid = platform.String(strings.Replace(response.GetUuid(), requestUuidPrefix, "", -1))

					// Strip off the tail for routing
					response.Routing.RouteTo = response.Routing.RouteTo[:len(response.Routing.RouteTo)-1]

					payloadBytes, err := platform.Marshal(response)
					if err != nil {
						log.Printf("[server.Route] failed to marshal platform response: %s", err)
						return
					}

					if closed {
						return
					}

					log.Printf("[server.Route] sending!")

					if err := routeServer.Send(&pb.Request{Payload: payloadBytes}); err != nil {
						log.Printf("[server.Route] failed to send platform response: %s", err)
						return
					}

					log.Printf("[server.Route] sent!")

					if response.GetCompleted() {
						log.Printf("[server.Route] got final response, closing down!")
						return
					}

				case <-timeout:
					log.Printf("[server.Route] %s - got a timeout for request: %s", clientUuid, platformRequest.GetUuid())
					return

				}
			}
		}()
	}

	return nil
}

func newServer(router platform.Router) *server {
	return &server{
		router: router,
	}
}
