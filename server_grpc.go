package main

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"net"
	"os"
	"strings"

	"github.com/microplatform-io/platform"
	pb "github.com/microplatform-io/platform-grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
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

	var opts []grpc.ServerOption

	if SSL_CERT != "" && SSL_KEY != "" {
		certFile, err := ioutil.TempFile("", "cert")
		if err != nil {
			logger.Fatalf("failed to write cert file: %s", err)
		}
		defer certFile.Close()

		keyFile, err := ioutil.TempFile("", "key")
		if err != nil {
			logger.Fatalf("failed to write key file: %s", err)
		}
		defer keyFile.Chdir()

		ioutil.WriteFile(certFile.Name(), []byte(strings.Replace(SSL_CERT, "\\n", "\n", -1)), os.ModeTemporary)
		ioutil.WriteFile(keyFile.Name(), []byte(strings.Replace(SSL_KEY, "\\n", "\n", -1)), os.ModeTemporary)

		creds, err := credentials.NewServerTLSFromFile(certFile.Name(), keyFile.Name())
		if err != nil {
			return err
		}

		opts = []grpc.ServerOption{grpc.Creds(creds)}
	}

	grpcServer := grpc.NewServer(opts...)
	pb.RegisterRouterServer(grpcServer, newServer(router))
	return grpcServer.Serve(lis)
}

type server struct {
	router platform.Router
}

func (s *server) Route(stream pb.Router_RouteServer) error {
	streamUUID := "stream-" + platform.CreateUUID()

	for {
		routerRequest, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}

			logger.Printf("[server.Route] %s - stream has been disconnected: %s", streamUUID, err)

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

		requestUUIDPrefix := streamUUID + "::"

		platformRequest.Uuid = platform.String(requestUUIDPrefix + platformRequest.GetUuid())

		responses, timeout := s.router.Route(platformRequest)

		loopResponses := true

		for loopResponses {
			select {
			case response := <-responses:
				responseJSON, _ := json.Marshal(response)
				logger.Printf("[server.Route] %s - got a response for request: %s", streamUUID, responseJSON)

				response.Uuid = platform.String(strings.Replace(response.GetUuid(), requestUUIDPrefix, "", -1))

				// Strip off the tail for routing
				response.Routing.RouteTo = response.Routing.RouteTo[:len(response.Routing.RouteTo)-1]

				payloadBytes, err := platform.Marshal(response)
				if err != nil {
					logger.Printf("[server.Route] %s - failed to marshal platform response: %s", streamUUID, err)
					continue
				}

				logger.Printf("[server.Route] %s - sending!", streamUUID)

				if err := stream.Send(&pb.Request{Payload: payloadBytes}); err != nil {
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

func newServer(router platform.Router) *server {
	return &server{
		router: router,
	}
}
