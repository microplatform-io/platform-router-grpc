package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/microplatform-io/platform"
)

const (
	SSL_CERT_FILE = "/tmp/ssl_cert"
	SSL_KEY_FILE  = "/tmp/ssl_key"
)

var (
	rabbitUser = os.Getenv("RABBITMQ_USER")
	rabbitPass = os.Getenv("RABBITMQ_PASS")
	rabbitAddr = os.Getenv("RABBITMQ_PORT_5672_TCP_ADDR")
	rabbitPort = os.Getenv("RABBITMQ_PORT_5672_TCP_PORT")

	publisher  platform.Publisher
	subscriber platform.Subscriber
)

type ServerConfig struct {
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     string `json:"port"`
}

func main() {
	hostname, _ := os.Hostname()

	routerUri := "router-" + hostname

	grpcPort := platform.Getenv("GRPC_PORT", "4772")

	connectionManager := platform.NewAmqpConnectionManager(rabbitUser, rabbitPass, rabbitAddr+":"+rabbitPort, "")
	publisher = getDefaultPublisher(connectionManager)
	subscriber = getDefaultSubscriber(connectionManager, routerUri)

	if err := ioutil.WriteFile(SSL_CERT_FILE, []byte(strings.Replace(os.Getenv("SSL_CERT"), "\\n", "\n", -1)), 0755); err != nil {
		log.Fatalf("> failed to write SSL cert file: %s", err)
	}

	if err := ioutil.WriteFile(SSL_KEY_FILE, []byte(strings.Replace(os.Getenv("SSL_KEY"), "\\n", "\n", -1)), 0755); err != nil {
		log.Fatalf("> failed to write SSL cert file: %s", err)
	}

	ip, err := platform.GetMyIp()
	if err != nil {
		log.Fatalf("> failed to get ip address: %s", err)
	}

	log.Println("We got our IP it is : ", ip)

	grpcServerConfig := &ServerConfig{
		Protocol: "https",
		Host:     formatHostAddress(ip),
		Port:     grpcPort, // we just use this here because this is where it reports it
	}

	go func() {
		for {
			ListenForGrpcServer(routerUri, grpcServerConfig)
		}
	}()

	go func() {
		for {
			ListenForHttpServer(routerUri, CreateServeMux(grpcServerConfig))
		}
	}()

	manageRouterState(&platform.RouterConfigList{
		RouterConfigs: []*platform.RouterConfig{
			&platform.RouterConfig{
				RouterType:   platform.RouterConfig_ROUTER_TYPE_GRPC.Enum(),
				ProtocolType: platform.RouterConfig_PROTOCOL_TYPE_HTTP.Enum(),
				Host:         platform.String(ip),
				Port:         platform.String(grpcPort),
			},
		},
	})

	// Block indefinitely
	<-make(chan bool)
}

func getDefaultPublisher(connectionManager *platform.AmqpConnectionManager) platform.Publisher {
	publisher, err := platform.NewAmqpPublisher(connectionManager)
	if err != nil {
		log.Fatalf("Could not create publisher. %s", err)
	}

	return publisher
}

func getDefaultSubscriber(connectionManager *platform.AmqpConnectionManager, queue string) platform.Subscriber {
	subscriber, err := platform.NewAmqpSubscriber(connectionManager, queue)
	if err != nil {
		log.Fatalf("Could not create subscriber. %s", err)
	}

	return subscriber
}

func formatHostAddress(ip string) string {
	hostAddress := strings.Replace(ip, ".", "-", -1)

	return fmt.Sprintf("%s.%s", hostAddress, "microplatform.io")
}

func manageRouterState(routerConfigList *platform.RouterConfigList) {
	log.Printf("%+v", routerConfigList)

	routerConfigListBytes, _ := platform.Marshal(routerConfigList)

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	// Emit a router offline signal if we catch an interrupt
	go func() {
		select {
		case <-sigc:
			publisher.Publish("router.offline", routerConfigListBytes)

			os.Exit(0)
		}
	}()

	// Wait for the servers to come online, and then repeat the router.online every 30 seconds
	time.AfterFunc(10*time.Second, func() {
		publisher.Publish("router.online", routerConfigListBytes)

		for {
			time.Sleep(30 * time.Second)
			publisher.Publish("router.online", routerConfigListBytes)
		}
	})
}
