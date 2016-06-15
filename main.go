package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/microplatform-io/platform"
	"github.com/microplatform-io/platform/amqp"
)

var (
	rabbitmqEndpoints = strings.Split(os.Getenv("RABBITMQ_ENDPOINTS"), ",")
	logger            = platform.GetLogger("platform-router-grpc")

	GRPC_PORT = platform.Getenv("GRPC_PORT", "4772")
	HTTP_PORT = platform.Getenv("HTTP_PORT", "4773")
	SSL_CERT  = platform.Getenv("SSL_CERT", "")
	SSL_KEY   = platform.Getenv("SSL_KEY", "")
)

type ServerConfig struct {
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     string `json:"port"`
}

func main() {
	hostname, _ := os.Hostname()

	routerUri := "router-" + hostname

	amqpDialers := amqp.NewCachingDialers(rabbitmqEndpoints)

	dialerInterfaces := []amqp.DialerInterface{}
	for i := range amqpDialers {
		dialerInterfaces = append(dialerInterfaces, amqpDialers[i])
	}

	publisher, err := amqp.NewMultiPublisher(dialerInterfaces)
	if err != nil {
		logger.Fatalf("> failed to create multi publisher: %s", err)
	}

	subscriber, err := amqp.NewMultiSubscriber(dialerInterfaces, routerUri)
	if err != nil {
		logger.Fatalf("> failed to create multi subscriber: %s", err)
	}

	router := platform.NewStandardRouter(publisher, subscriber)
	router.SetHeartbeatTimeout(7 * time.Second)

	ip, err := platform.GetMyIp()
	if err != nil {
		logger.Fatalf("> failed to get ip address: %s", err)
	}

	logger.Println("We got our IP it is : ", ip)

	grpcServerConfig := &ServerConfig{
		Protocol: "https",
		Host:     formatHostAddress(ip),
		Port:     GRPC_PORT, // we just use this here because this is where it reports it
	}

	wg := sync.WaitGroup{}
	wg.Add(1)

	go func() {
		logger.Println("grpc server died: %s", ListenForGrpcServer(router, grpcServerConfig))

		wg.Done()
	}()

	logger.Printf("http server died: %s", ListenForHttpServer(router, CreateServeMux(grpcServerConfig)))

	wg.Wait()
}

func formatHostAddress(ip string) string {
	hostAddress := strings.Replace(ip, ".", "-", -1)

	return fmt.Sprintf("%s.%s", hostAddress, "microplatform.io")
}
