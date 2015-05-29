package main

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/codegangsta/negroni"
	"github.com/microplatform-io/platform"
	pb "github.com/microplatform-io/platform-grpc"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
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
	publisher              platform.Publisher
	serverConfig           *ServerConfig
)

type ServerConfig struct {
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     string `json:"port"`
}

type server struct{}

func (s *server) Route(ctx context.Context, in *pb.Request) (*pb.Request, error) {

	log.Println("A Request came in like : ", in)

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
	publisher = getDefaultPublisher()
	standardRouter = platform.NewStandardRouter(publisher, subscriber)

	// we default to Port 80 here
	if port == "" {
		port = "8080"
	}

	go ListenForServer()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	//if we have SSL CERT and KEY lets use them.
	cert, err := tls.LoadX509KeyPair("./cert", "./key")
	if err == nil {
		log.Println("> err was nil for loading 509 key pair")

		config := &tls.Config{Certificates: []tls.Certificate{cert}}
		config.Rand = rand.Reader

		lis = tls.NewListener(lis, config)
	}

	log.Printf("certificate: %s", cert.Certificate)
	log.Printf("key: %#v", cert.PrivateKey)

	s := grpc.NewServer()

	log.Println("Server is : ", s)

	pb.RegisterRouterServer(s, &server{})
	s.Serve(lis)
	os.Exit(0)
}

func writePid() {
	if pidfile := os.Getenv("PIDFILE"); pidfile != "" {
		ioutil.WriteFile(pidfile, []byte(strconv.Itoa(os.Getpid())), os.ModePerm)
	}
}

func ListenForServer() {

	ip, err := getMyIp()
	if err != nil {
		log.Fatal(err)
	}
	log.Println("We got our IP it is : ", ip)

	port = "4772"

	serverConfig = &ServerConfig{
		Protocol: "http",
		Host:     fmt.Sprintf("%s.microplatform.io", strings.Replace(ip, ".", "-", -1)),
		Port:     port, // we just use this here because this is where it reports it
	}

	routerConfig := &platform.RouterConfig{
		RouterType:   platform.RouterConfig_ROUTER_TYPE_GRPC.Enum(),
		ProtocolType: platform.RouterConfig_PROTOCOL_TYPE_HTTP.Enum(),
		Host:         platform.String(ip),
		Port:         platform.String(port),
	}
	routerConfigBytes, err := platform.Marshal(routerConfig)
	if err != nil {
		publisher.Publish("router.online", routerConfigBytes)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/server", serverHandler)
	mux.HandleFunc("/", serverHandler)

	n := negroni.Classic()
	n.UseHandler(mux)

	writePid()

	n.Run(":4772") //actually runs on :4772 just so we can get server information

	os.Exit(0)
}

func serverHandler(rw http.ResponseWriter, req *http.Request) {
	cb := req.FormValue("callback")
	jsonBytes, _ := json.Marshal(serverConfig)

	if cb == "" {
		rw.Header().Set("Content-Type", "application/json")
		rw.Write(jsonBytes)
		return
	}

	rw.Header().Set("Content-Type", "application/javascript")
	fmt.Fprintf(rw, fmt.Sprintf("%s(%s)", cb, jsonBytes))
}

func getMyIp() (string, error) {
	urls := []string{"http://ifconfig.me/ip", "http://curlmyip.com", "http://icanhazip.com"}
	respChan := make(chan *http.Response)

	for _, url := range urls {
		go func(url string, responseChan chan *http.Response) {
			res, err := http.Get(url)
			if err == nil {
				responseChan <- res
			}
		}(url, respChan)
	}

	select {
	case res := <-respChan:
		defer res.Body.Close()
		body, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return "", err
		}
		return strings.Trim(string(body), "\n "), nil
	case <-time.After(time.Second * 5):
		return "", errors.New("Timed out trying to fetch ip address.")
	}
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
