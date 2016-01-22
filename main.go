package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/microplatform-io/platform"
)

var (
	rabbitmqEndpoints = strings.Split(os.Getenv("RABBITMQ_ENDPOINTS"), ",")

	GRPC_PORT = platform.Getenv("GRPC_PORT", "4772")
	HTTP_PORT = platform.Getenv("HTTP_PORT", "4773")
)

type ServerConfig struct {
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     string `json:"port"`
}

func main() {
	hostname, _ := os.Hostname()

	routerUri := "router-" + hostname

	connectionManagers := platform.NewAmqpConnectionManagersWithEndpoints(rabbitmqEndpoints)

	publisher, err := platform.NewAmqpMultiPublisher(connectionManagers)
	if err != nil {
		log.Fatalf("> failed to create multi publisher: %s", err)
	}

	subscriber, err := platform.NewAmqpMultiSubscriber(connectionManagers, routerUri)
	if err != nil {
		log.Fatalf("> failed to create multi subscriber: %s", err)
	}

	router := platform.NewStandardRouter(publisher, subscriber)
	router.SetHeartbeatTimeout(7 * time.Second)

	ip, err := platform.GetMyIp()
	if err != nil {
		log.Fatalf("> failed to get ip address: %s", err)
	}

	log.Println("We got our IP it is : ", ip)

	grpcServerConfig := &ServerConfig{
		Protocol: "https",
		Host:     formatHostAddress(ip),
		Port:     GRPC_PORT, // we just use this here because this is where it reports it
	}

	go func() {
		for {
			ListenForGrpcServer(router, grpcServerConfig)
		}
	}()

	go func() {
		for {
			ListenForHttpServer(router, CreateServeMux(grpcServerConfig))
		}
	}()

	// Block indefinitely
	<-make(chan bool)
}

func formatHostAddress(ip string) string {
	hostAddress := strings.Replace(ip, ".", "-", -1)

	return fmt.Sprintf("%s.%s", hostAddress, "microplatform.io")
}
