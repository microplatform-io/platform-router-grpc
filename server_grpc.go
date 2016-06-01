package main

import (
	"encoding/json"
	"io"
	"net"
	"strings"
	"syscall"

	"github.com/bmoyles0117/go-drain"
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

	drainListener, err := drain.Listen(lis)
	if err != nil {
		logger.Fatalf("failed to listen: %v", err)
	}

	drainListener.ShutdownWhenSignalsNotified(syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	s := grpc.NewServer()
	pb.RegisterRouterServer(s, newServer(router, drainListener))
	return s.Serve(drainListener)
}

type server struct {
	drainListener *drain.Listener
	router        platform.Router
}

func (s *server) Route(routeServer pb.Router_RouteServer) error {
	streamUUID := "stream-" + platform.CreateUUID()

	shutdownNotifier := s.drainListener.NotifyShutdown()
	defer shutdownNotifier.Shutdown()

	for {
		routerRequest, err := routeServer.Recv()
		if err != nil {
			logger.Printf("[server.Route] %s - stream has been disconnected", streamUUID)

			if err == io.EOF {
				return nil
			}

			return err
		}

		platformRequest := &platform.Request{}
		if err := platform.Unmarshal(routerRequest.Payload, platformRequest); err != nil {
			logger.Printf("[server.Route] %s -  failed to unmarshal platform request: %s", streamUUID, err)
			return err
		}

		logger.Printf("[server.Route] %s - got router request: %#v", streamUUID, platformRequest)

		if platformRequest.Routing == nil {
			platformRequest.Routing = &platform.Routing{}
		}

		if !platform.RouteToSchemeMatches(platformRequest, "microservice") {
			logger.Printf("[server.Route] %s -  unsupported scheme provided: %s", streamUUID, platformRequest.Routing.RouteTo)
			return err
		}

		platformRequest.Routing.RouteFrom = []*platform.Route{
			&platform.Route{
				Uri: platform.String("client://" + streamUUID),
			},
		}

		requestUuidPrefix := streamUUID + "::"

		platformRequest.Uuid = platform.String(requestUuidPrefix + platformRequest.GetUuid())

		responses, timeout := s.router.Route(platformRequest)

		loopResponses := true

		for loopResponses {
			select {
			case response := <-responses:
				responseJSON, _ := json.Marshal(response)
				logger.Printf("[server.Route] %s - got a response for request: %s", streamUUID, responseJSON)

				response.Uuid = platform.String(strings.Replace(response.GetUuid(), requestUuidPrefix, "", -1))

				// Strip off the tail for routing
				response.Routing.RouteTo = response.Routing.RouteTo[:len(response.Routing.RouteTo)-1]

				payloadBytes, err := platform.Marshal(response)
				if err != nil {
					logger.Printf("[server.Route] %s - failed to marshal platform response: %s", streamUUID, err)
					continue
				}

				logger.Printf("[server.Route] %s - sending!", streamUUID)

				if err := routeServer.Send(&pb.Request{Payload: payloadBytes}); err != nil {
					logger.Printf("[server.Route] %s - failed to send platform response: %s", streamUUID, err)
					return err
				}

				logger.Printf("[server.Route] %s - sent!", streamUUID)

				if response.GetCompleted() {
					logger.Printf("[server.Route] %s - got final response, closing down!", streamUUID)

					loopResponses = false
				}

			case <-timeout:
				logger.Printf("[server.Route] %s - got a timeout for request: %s", streamUUID, platformRequest.GetUuid())

				loopResponses = false

			}
		}
	}

	return nil
}

func newServer(router platform.Router, drainListener *drain.Listener) *server {
	server := &server{
		router:        router,
		drainListener: drainListener,
	}

	return server
}
