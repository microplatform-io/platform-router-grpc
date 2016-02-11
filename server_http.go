package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/codegangsta/negroni"
	"github.com/microplatform-io/platform"
)

func ListenForHttpServer(router platform.Router, mux *http.ServeMux) error {
	defer func() {
		if r := recover(); r != nil {
			log.Println("> http server has died: %s", r)
		}
	}()

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

	n.Run(":" + HTTP_PORT)

	return errors.New("server unexpected died")
}

func CreateServeMux(serverConfig *ServerConfig) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/server", serverHandler(serverConfig))
	mux.HandleFunc("/", serverHandler(serverConfig))
	return mux
}

func serverHandler(serverConfig *ServerConfig) func(w http.ResponseWriter, req *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		cb := req.FormValue("callback")

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
