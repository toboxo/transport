// Copyright (c) 2014 The gomqtt Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package transport

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"gopkg.in/tomb.v2"
)

type WebSocketServer struct {
	listener net.Listener
	upgrader *websocket.Upgrader
	incoming chan *WebSocketConn

	tomb tomb.Tomb
}

func newWebSocketServer(listener net.Listener) *WebSocketServer {
	return &WebSocketServer{
		listener: listener,
		upgrader: &websocket.Upgrader{
			HandshakeTimeout: 60 * time.Second,
			Subprotocols:     []string{"mqtt"},
		},
		incoming: make(chan *WebSocketConn),
	}
}

func NewWebSocketServer(address string) (*WebSocketServer, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, newTransportError(NetworkError, err)
	}

	s := newWebSocketServer(listener)
	s.serveHTTP()

	return s, nil
}

func NewSecureWebSocketServer(address string, config *tls.Config) (*WebSocketServer, error) {
	listener, err := tls.Listen("tcp", address, config)
	if err != nil {
		return nil, newTransportError(NetworkError, err)
	}

	s := newWebSocketServer(listener)
	s.serveHTTP()

	return s, nil
}

func (s *WebSocketServer) serveHTTP() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.requestHandler)

	h := &http.Server{
		Handler: mux,
	}

	s.tomb.Go(func() error {
		err := h.Serve(s.listener)
		if err != nil {
			newTransportError(NetworkError, err)
		}

		return nil
	})
}

func (s *WebSocketServer) requestHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// upgrader already responded to request
		return
	}

	webSocketConn := NewWebSocketConn(conn)

	select {
	case s.incoming <- webSocketConn:
	case <-s.tomb.Dying():
		webSocketConn.Close()
	}
}

func (s *WebSocketServer) Accept() (Conn, error) {
	select {
	case <-s.tomb.Dying():
		// return the previously caught error
		return nil, s.tomb.Err()
	case conn := <-s.incoming:
		return conn, nil
	}
}

func (s *WebSocketServer) Close() error {
	s.tomb.Kill(nil)

	err := s.listener.Close()
	s.tomb.Wait()

	if err != nil {
		return newTransportError(NetworkError, err)
	}

	return nil
}