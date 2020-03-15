package ot

import (
	"sync"
	"sync/atomic"

	"github.com/fmpwizard/go-quilljs-delta/delta"
)

type client struct {
	lastSubmittedVersion uint32
	update               chan Change
	stopping             int32
}

type request struct {
	clientId uint32
	d        delta.Delta
	version  uint32
}

type newClient struct {
	c        *client
	clientId uint32
}

type Server struct {
	file *File

	requests     chan request
	newClients   chan newClient
	closeClients chan uint32
	stoppingChan chan bool

	lastClientId uint32
	clients      map[uint32]*client

	dataMux sync.Mutex

	running int32
}

func NewServer(d delta.Delta) *Server {
	return &Server{
		file:         NewFile(d),
		clients:      make(map[uint32]*client),
		requests:     make(chan request),
		newClients:   make(chan newClient),
		closeClients: make(chan uint32),
		stoppingChan: make(chan bool),
		running:      0,
	}
}

func (s *Server) CurrentChange() Change {
	s.dataMux.Lock()
	defer s.dataMux.Unlock()

	return s.file.CurrentChange()
}

func (s *Server) NewClient() (uint32, <-chan Change, error) {
	c := &client{
		lastSubmittedVersion: 0,
		update:               make(chan Change),
		stopping:             0,
	}
	clientId := atomic.AddUint32(&s.lastClientId, 1)
	s.newClients <- newClient{
		c:        c,
		clientId: clientId,
	}

	return clientId, c.update, nil
}

func (s *Server) Submit(clientId uint32, d delta.Delta, version uint32) {
	s.requests <- request{
		clientId: clientId,
		d:        d,
		version:  version,
	}
}

func (s *Server) Close(clientId uint32) {
	s.closeClients <- clientId
}

func (s *Server) Running() bool {
	return atomic.LoadInt32(&s.running) != 0
}

func (s *Server) Stop() {
	s.stoppingChan <- true
	<-s.stoppingChan
}

func (s *Server) Start() {
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return
	}
	stopping := false
	for !stopping {
		select {
		case <-s.stoppingChan:
			stopping = true
		case newClient := <-s.newClients:
			if _, ok := s.clients[newClient.clientId]; ok {
				// Client ID conflicts, this shouldn't happen
				close(newClient.c.update)
				continue
			}
			newClient.c.update <- s.CurrentChange()
			s.clients[newClient.clientId] = newClient.c
		case clientIdToClose := <-s.closeClients:
			if c, ok := s.clients[clientIdToClose]; ok {
				close(c.update)
				delete(s.clients, clientIdToClose)
			}
		case request := <-s.requests:
			s.dataMux.Lock()
			lastSubmittedVersion := request.version
			newChange, err := s.file.Submit(Change{
				Delta:   &request.d,
				Version: request.version,
			})
			s.dataMux.Unlock()
			if err != nil {
				continue
			}

			channels := make([]chan Change, 0, len(s.clients))
			data := make([]Change, 0, len(s.clients))
			for clientId, c := range s.clients {
				channels = append(channels, c.update)
				if clientId == request.clientId {
					c.lastSubmittedVersion = lastSubmittedVersion
					data = append(data, Change{Version: newChange.Version})
				} else {
					data = append(data, newChange)
				}
			}

			for i, c := range channels {
				c <- data[i]
			}
		}
	}
	// Close all clients
	for _, c := range s.clients {
		close(c.update)
	}
	s.clients = make(map[uint32]*client)
	atomic.CompareAndSwapInt32(&s.running, 1, 0)
	s.stoppingChan <- true
}
