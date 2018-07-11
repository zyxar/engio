package engine

import (
	"log"
	"net/http"
	"sync"
	"time"
)

type Server struct {
	pingInterval time.Duration
	pingTimeout  time.Duration
	ßchan        chan *session
	done         chan struct{}
	once         sync.Once
	*sessionManager
	*eventHandlers
	*emitter
}

func NewServer(interval, timeout time.Duration) (*Server, error) {
	done := make(chan struct{})
	s := &Server{
		pingInterval:   interval,
		pingTimeout:    timeout,
		ßchan:          make(chan *session, 1),
		done:           done,
		sessionManager: newSessionManager(),
		eventHandlers:  newEventHandlers(),
		emitter:        newEmitter(64, done),
	}

	go s.emitter.loop()

	go func() {
		for {
			select {
			case ß, ok := <-s.ßchan:
				if !ok {
					return
				}
				s.fire(ß.Socket, EventOpen, MessageTypeString, nil)
				go func() {
					so := ß.Socket
					defer so.Close()
					defer s.sessionManager.Remove(ß.id)
					for {
						if err := so.Handle(); err != nil {
							if err == ErrPollingConnPaused {
								ß.CheckPaused()
								continue
							}
							log.Println("handle:", err.Error())
							so.fire(so, EventClose, MessageTypeString, nil)
							return
						}
					}
				}()
			}
		}
	}()
	return s, nil
}

func (s *Server) Close() (err error) {
	s.once.Do(func() {
		close(s.done)
		close(s.ßchan)
	})
	return
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Println(r.RemoteAddr, r.Method, r.URL.RawQuery)

	query := r.URL.Query()
	if query.Get(queryEIO) != Version {
		http.Error(w, "protocol version incompatible", http.StatusBadRequest)
		return
	}

	acceptor := getAcceptor(query.Get(queryTransport))
	if acceptor == nil {
		http.Error(w, "invalid transport", http.StatusBadRequest)
		return
	}

	var ß *session
	sid := query.Get(querySession)
	if sid == "" {
		conn, err := acceptor.Accept(w, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		ß = s.NewSession(conn, s.emitter, s.pingTimeout+s.pingInterval, s.pingTimeout)
		ß.transport = acceptor.Transport()
		ß.Emit(EventOpen, &Parameters{
			SID:          ß.id,
			Upgrades:     []string{"websocket"},
			PingInterval: int(s.pingInterval / time.Millisecond),
			PingTimeout:  int(s.pingTimeout / time.Millisecond),
		})
		s.ßchan <- ß
	} else {
		var exists bool
		ß, exists = s.sessionManager.Get(sid)
		if !exists {
			http.Error(w, "invalid session", http.StatusBadRequest)
			return
		}
		transport := acceptor.Transport()
		if ß.transport != transport {
			conn, err := acceptor.Accept(w, r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			ß.Pause()
			ß.upgrade(transport, conn)
			ß.Resume()
		}
	}
	ß.ServeHTTP(w, r)
	return
}

func (s *Server) BindAndListen(srv *http.Server) error {
	if srv == nil {
		panic("nil http server")
	}
	srv.Handler = s
	return srv.ListenAndServe()
}
