package main

import (
	"encoding/json"
	"fmt"
	"github.com/JacobSquires/negroni"
	"log"
	"net/http"
	"os"
)

func ListenForHttpServer(routerUri string, grpcServerConfig *ServerConfig) {
	defer func() {
		if r := recover(); r != nil {
			log.Println("> http server has died: %s", r)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/server", serverHandler(grpcServerConfig))
	mux.HandleFunc("/", serverHandler(grpcServerConfig))

	n := negroni.Classic()
	n.Use(negroni.HandlerFunc(func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Add("Access-Control-Allow-Origin", origin)
		} else {
			w.Header().Add("Access-Control-Allow-Origin", "null")
		}

		w.Header().Add("Access-Control-Allow-Methods", "GET,PUT,POST,DELETE")
		w.Header().Add("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Add("Access-Control-Allow-Credentials", "true")
		w.Header().Add("Connection", "keep-alive")

		next(w, r)
	}))
	n.UseHandler(mux)

	httpPort := os.Getenv("HTTP_PORT")
	if httpPort == "" {
		httpPort = "4773"
	}

	n.RunTLS(":"+httpPort, SSL_CERT_FILE, SSL_KEY_FILE)
}

func serverHandler(grpcServerConfig *ServerConfig) func(w http.ResponseWriter, req *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		cb := req.FormValue("callback")

		jsonBytes, _ := json.Marshal(grpcServerConfig)
		if cb == "" {
			w.Header().Set("Content-Type", "application/json")
			w.Write(jsonBytes)
			return
		}

		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprintf(w, fmt.Sprintf("%s(%s)", cb, jsonBytes))
	}
}
