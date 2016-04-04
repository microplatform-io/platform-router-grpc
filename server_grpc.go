package main

import (
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"github.com/microplatform-io/platform"
	pb "github.com/microplatform-io/platform-grpc"
	"google.golang.org/grpc"
)

func ListenForGrpcServer(router platform.Router, grpcServerConfig *ServerConfig) error {
	defer func() {
		if r := recover(); r != nil {
			logger.Println("> grpc server has died: %s", r)
		}
	}()

	lis, err := net.Listen("tcp", ":"+grpcServerConfig.Port)
	if err != nil {
		logger.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterRouterServer(s, newServer(router))
	return s.Serve(lis)
}

type server struct {
	router               platform.Router
	closed               chan interface{}
	totalPendingRequests int32
}

func (s *server) Route(routeServer pb.Router_RouteServer) error {
	clientUuid := "client-" + platform.CreateUUID()

	clientClosed := make(chan interface{})

	for {
		select {
		case <-clientClosed:
			logger.Printf("[server.Route] %s - client is closed! goodbye!", clientUuid)

			return nil
		case <-s.closed:
			logger.Printf("[server.Route] %s - server is closed! goodbye!", clientUuid)

			return nil

		default:
			logger.Printf("[server.Route] %s - waiting for request", clientUuid)

			routerRequest, err := routeServer.Recv()
			if err != nil {
				close(clientClosed)

				if err == io.EOF {
					logger.Printf("[server.Route] %s - client has disconnected", clientUuid)

					return nil
				} else {
					logger.Printf("[server.Route] %s - client has disconnected due to unexpected error: %s", clientUuid, err)

					return err
				}
			}

			logger.Printf("[server.Route] %s -  got router request: %s", clientUuid, routerRequest)

			platformRequest := &platform.Request{}
			if err := platform.Unmarshal(routerRequest.Payload, platformRequest); err != nil {
				logger.Printf("[server.Route] %s -  failed to unmarshal platform request: %s", clientUuid, err)
				continue
			}

			if platformRequest.Routing == nil {
				platformRequest.Routing = &platform.Routing{}
			}

			if !platform.RouteToSchemeMatches(platformRequest, "microservice") {
				logger.Printf("[server.Route] %s -  unsupported scheme provided: %s", clientUuid, platformRequest.Routing.RouteTo)
				continue
			}

			platformRequest.Routing.RouteFrom = []*platform.Route{
				&platform.Route{
					Uri: platform.String("client://" + clientUuid),
				},
			}

			requestUuidPrefix := clientUuid + "::"

			platformRequest.Uuid = platform.String(requestUuidPrefix + platformRequest.GetUuid())

			atomic.AddInt32(&s.totalPendingRequests, 1)

			responses, timeout := s.router.Route(platformRequest)

			go func() {
				defer atomic.AddInt32(&s.totalPendingRequests, -1)

				for {
					select {
					case <-clientClosed:
						return

					case response := <-responses:
						logger.Printf("[server.Route] %s - got a response for request: %s", clientUuid, platformRequest.GetUuid())
						logger.PrettyPrint(response)

						response.Uuid = platform.String(strings.Replace(response.GetUuid(), requestUuidPrefix, "", -1))

						// Strip off the tail for routing
						response.Routing.RouteTo = response.Routing.RouteTo[:len(response.Routing.RouteTo)-1]

						payloadBytes, err := platform.Marshal(response)
						if err != nil {
							logger.Printf("[server.pRoute] failed to marshal platform response: %s", err)
							return
						}

						logger.Printf("[server.Route] sending!")

						if err := routeServer.Send(&pb.Request{Payload: payloadBytes}); err != nil {
							logger.Printf("[server.Route] failed to send platform response: %s", err)
							return
						}

						logger.Printf("[server.Route] sent!")

						if response.GetCompleted() {
							logger.Printf("[server.Route] got final response, closing down!")
							return
						}

					case <-timeout:
						logger.Printf("[server.Route] %s - got a timeout for request: %s", clientUuid, platformRequest.GetUuid())
						return

					}
				}
			}()
		}
	}

	return nil
}

func (s *server) monitorKillSignal() {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	capturedSignal := <-sigc

	close(s.closed)

	for i := 0; i < 60; i++ {
		totalPendingRequests := atomic.LoadInt32(&s.totalPendingRequests)

		logger.Printf("attempting to close server due to %s signal, total pending requests: %d", capturedSignal, totalPendingRequests)

		if totalPendingRequests <= 0 {
			os.Exit(0)
		}

		time.Sleep(time.Second * 1)
	}

	logger.Println("failed to wait for all pending requests, %d were still remaining", atomic.LoadInt32(&s.totalPendingRequests))

	os.Exit(1)
}

func newServer(router platform.Router) *server {
	server := &server{
		router:               router,
		closed:               make(chan interface{}),
		totalPendingRequests: 0,
	}

	go server.monitorKillSignal()

	return server
}
