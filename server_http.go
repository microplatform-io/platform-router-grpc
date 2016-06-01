package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"syscall"

	"github.com/bmoyles0117/go-drain"
	"github.com/microplatform-io/platform"
)

func ListenForHttpServer(router platform.Router, mux *http.ServeMux) error {
	defer func() {
		if r := recover(); r != nil {
			logger.Println("> http server has died: %s", r)
		}
	}()

	lis, err := net.Listen("tcp", ":"+HTTP_PORT)
	if err != nil {
		return err
	}

	drainListener, err := drain.Listen(lis)
	if err != nil {
		return err
	}

	drainListener.ShutdownWhenSignalsNotified(syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	return http.Serve(drainListener, mux)
}

func CreateServeMux(serverConfig *ServerConfig) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/server", serverHandler(serverConfig))
	mux.HandleFunc("/", serverHandler(serverConfig))
	return mux
}

func serverHandler(serverConfig *ServerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Add("Access-Control-Allow-Origin", origin)
		} else {
			w.Header().Add("Access-Control-Allow-Origin", "null")
		}

		w.Header().Add("Access-Control-Allow-Methods", "GET,PUT,POST,DELETE")
		w.Header().Add("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Add("Access-Control-Allow-Credentials", "true")
		w.Header().Add("Connection", "keep-alive")

		cb := r.FormValue("callback")

		jsonBytes, _ := json.Marshal(serverConfig)
		if cb == "" {
			w.Header().Set("Content-Type", "application/json")
			w.Write(jsonBytes)
			return
		}

		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprintf(w, fmt.Sprintf("%s(%s)", cb, jsonBytes))
	}
}
