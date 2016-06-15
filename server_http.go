package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	"github.com/microplatform-io/platform"
)

func ListenForHttpServer(router platform.Router, mux *http.ServeMux) error {
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

		cfg := &tls.Config{
			MinVersion:               tls.VersionTLS12,
			CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
			PreferServerCipherSuites: true,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
				tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_RSA_WITH_AES_256_CBC_SHA,
			},
		}
		srv := &http.Server{
			Addr:         ":" + HTTP_PORT,
			Handler:      mux,
			TLSConfig:    cfg,
			TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler), 0),
		}

		return srv.ListenAndServeTLS(certFile.Name(), keyFile.Name())
	} else {
		return http.ListenAndServe(":"+HTTP_PORT, mux)
	}
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
