package main

import (
	"errors"
	"fmt"
	"github.com/microplatform-io/platform"
	pb "github.com/microplatform-io/platform-grpc"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"log"
	"net"
	"os"
	"regexp"
	"time"
)

var (
	rabbitUser = os.Getenv("RABBITMQ_USER")
	rabbitPass = os.Getenv("RABBITMQ_PASS")
	rabbitAddr = os.Getenv("RABBITMQ_PORT_5672_TCP_ADDR")
	rabbitPort = os.Getenv("RABBITMQ_PORT_5672_TCP_PORT")
	port       = os.Getenv("PORT")

	rabbitRegex            = regexp.MustCompile("RABBITMQ_[0-9]_PORT_5672_TCP_(ADDR|PORT)")
	amqpConnectionManagers []*platform.AmqpConnectionManager
	standardRouter         platform.Router
)

type server struct{}

func (s *server) Route(ctx context.Context, in *pb.Request) (*pb.Request, error) {
	routedMessage, err := standardRouter.Route(&platform.RoutedMessage{
		Method:   platform.Int32(in.Method),
		Resource: platform.Int32(in.Resource),
		Body:     in.Body,
	}, 5*time.Second)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Failed to route message: %s", err))
	}

	return &pb.Request{
		Method:   routedMessage.GetMethod(),
		Resource: routedMessage.GetResource(),
		Body:     routedMessage.GetBody(),
	}, nil

}

func main() {
	hostname, _ := os.Hostname()
	subscriber := getDefaultSubscriber("router_" + hostname)
	standardRouter = platform.NewStandardRouter(getDefaultPublisher(), subscriber)

	if port == "" {
		port = "8752"
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterRouterServer(s, &server{})
	s.Serve(lis)
}

func getDefaultPublisher() platform.Publisher {

	publishers := []platform.Publisher{}

	connMgrs := getAmqpConnectionManagers()
	for _, connMgr := range connMgrs {

		publisher, err := platform.NewAmqpPublisher(connMgr)
		if err != nil {
			log.Printf("Could not create publisher. %s", err)
			continue
		}
		publishers = append(publishers, publisher)
	}

	if len(publishers) == 0 {

		log.Fatalln("Failed to create a single publisher.\n")
	}

	return platform.NewMultiPublisher(publishers...)

}

func getDefaultSubscriber(queue string) platform.Subscriber {
	subscribers := []platform.Subscriber{}
	connMgrs := getAmqpConnectionManagers()
	for _, connMgr := range connMgrs {

		subscriber, err := platform.NewAmqpSubscriber(connMgr, queue)
		if err != nil {
			log.Printf("Could not create subscriber. %s", err)
			continue
		}
		subscribers = append(subscribers, subscriber)
	}

	if len(subscribers) == 0 {
		log.Fatalln("Failed to create a single subscriber.\n")
	}

	return platform.NewMultiSubscriber(subscribers...)
}

func getAmqpConnectionManagers() []*platform.AmqpConnectionManager {
	if amqpConnectionManagers != nil {
		return amqpConnectionManagers
	}

	amqpConnectionManagers := []*platform.AmqpConnectionManager{}

	count := 0
	for _, v := range os.Environ() {
		if rabbitRegex.MatchString(v) {
			count++
		}
	}

	if count == 0 { // No match for multiple rabbitmq servers, try and use single rabbitmq environment variables
		amqpConnectionManagers = append(amqpConnectionManagers, platform.NewAmqpConnectionManager(rabbitUser, rabbitPass, rabbitAddr+":"+rabbitPort, ""))
	} else if count%2 == 0 { // looking for a piar or rabbitmq addr and port
		for i := 0; i < count/2; i++ {
			amqpConnectionManagers = append(amqpConnectionManagers, platform.NewAmqpConnectionManager(rabbitUser, rabbitPass, fmt.Sprintf("%s:%s", os.Getenv(fmt.Sprintf("RABBITMQ_%d_PORT_5672_TCP_ADDR", i+1)), os.Getenv(fmt.Sprint("RABBITMQ_%d_PORT_5672_TCP_PORT", i+1))), ""))
		}
	}

	return amqpConnectionManagers
}
