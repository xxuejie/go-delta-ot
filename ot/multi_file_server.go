package ot

import (
	"sync"
	"sync/atomic"

	"github.com/fmpwizard/go-quilljs-delta/delta"
)

type multiFileClient struct {
	versions map[uint32]uint32
	update   chan MultiFileChange
}

type multiFileNewClient struct {
	update   chan MultiFileChange
	setup    chan bool
	clientId uint32
}

type multiFileNewFile struct {
	setup  chan bool
	fileId uint32
	d      delta.Delta
}

type multiFileSubmit struct {
	change   MultiFileChange
	clientId uint32
}

type multiFileCloseFile struct {
	fileId     uint32
	notifyChan chan<- bool
}

type multiFileCommand struct {
	newClient     *multiFileNewClient
	closeClientId *uint32
	newFile       *multiFileNewFile
	closeFile     *multiFileCloseFile
	submit        *multiFileSubmit
}

type MultiFileServer struct {
	files   map[uint32]*File
	clients map[uint32]*multiFileClient

	commands     chan multiFileCommand
	stoppingChan chan bool

	fileMux sync.Mutex
	running int32
}

func NewMultiFileServer() *MultiFileServer {
	return &MultiFileServer{
		files:        make(map[uint32]*File),
		clients:      make(map[uint32]*multiFileClient),
		commands:     make(chan multiFileCommand),
		stoppingChan: make(chan bool),
		running:      0,
	}
}

func (s *MultiFileServer) CurrentChange(fileId uint32) *MultiFileChange {
	s.fileMux.Lock()
	defer s.fileMux.Unlock()

	if file, ok := s.files[fileId]; ok {
		change := MultiFileChange{
			Id:     fileId,
			Change: file.CurrentChange(),
		}
		return &change
	}
	return nil
}

func (s *MultiFileServer) AllChanges() []MultiFileChange {
	s.fileMux.Lock()
	defer s.fileMux.Unlock()

	changes := make([]MultiFileChange, 0)
	for id, file := range s.files {
		changes = append(changes, MultiFileChange{
			Id:     id,
			Change: file.CurrentChange(),
		})
	}

	return changes
}

func (s *MultiFileServer) NewClient(clientId uint32) (<-chan bool, <-chan MultiFileChange) {
	setup := make(chan bool)
	update := make(chan MultiFileChange)

	s.commands <- multiFileCommand{
		newClient: &multiFileNewClient{
			update:   update,
			setup:    setup,
			clientId: clientId,
		},
	}

	return setup, update
}

func (s *MultiFileServer) NewFile(fileId uint32, d delta.Delta) <-chan bool {
	setup := make(chan bool)

	s.commands <- multiFileCommand{
		newFile: &multiFileNewFile{
			setup:  setup,
			fileId: fileId,
			d:      d,
		},
	}

	return setup
}

// Unlike NewFile, notifyChan is provided as a parameter so caller can choose
// if they want to be notified.
func (s *MultiFileServer) CloseFile(fileId uint32, notifyChan chan<- bool) {
	s.commands <- multiFileCommand{
		closeFile: &multiFileCloseFile{
			fileId:     fileId,
			notifyChan: notifyChan,
		},
	}
}

func (s *MultiFileServer) Submit(clientId uint32, change MultiFileChange) {
	s.commands <- multiFileCommand{
		submit: &multiFileSubmit{
			clientId: clientId,
			change:   change,
		},
	}
}

func (s *MultiFileServer) CloseClient(clientId uint32) {
	s.commands <- multiFileCommand{
		closeClientId: &clientId,
	}
}

func (s *MultiFileServer) Running() bool {
	return atomic.LoadInt32(&s.running) != 0
}

func (s *MultiFileServer) Stop() {
	s.stoppingChan <- true
	<-s.stoppingChan
}

func (s *MultiFileServer) Start() {
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return
	}
	stopping := false
	for !stopping {
		select {
		case <-s.stoppingChan:
			stopping = true
		case command := <-s.commands:
			if newClient := command.newClient; newClient != nil {
				if _, ok := s.clients[newClient.clientId]; ok {
					// Client ID conflicts, abort client creation
					newClient.setup <- false
				} else {
					newClient.setup <- true
					c := &multiFileClient{
						versions: make(map[uint32]uint32),
						update:   newClient.update,
					}
					for _, change := range s.AllChanges() {
						c.versions[change.Id] = change.Change.Version
						c.update <- change
					}
					s.clients[newClient.clientId] = c
				}
			}
			if id := command.closeClientId; id != nil {
				if c, ok := s.clients[*id]; ok {
					close(c.update)
					delete(s.clients, *id)
				}
			}
			if newFile := command.newFile; newFile != nil {
				s.fileMux.Lock()
				_, ok := s.files[newFile.fileId]
				s.fileMux.Unlock()
				if ok {
					// File ID exists, abort file creation
					newFile.setup <- false
				} else {
					file := NewFile(newFile.d)
					s.fileMux.Lock()
					s.files[newFile.fileId] = file
					s.fileMux.Unlock()
					newFile.setup <- true
					for _, c := range s.clients {
						fileContent := file.CurrentChange()
						c.versions[newFile.fileId] = fileContent.Version
						c.update <- MultiFileChange{
							Id:     newFile.fileId,
							Change: fileContent,
						}
					}
				}
			}
			if closeFile := command.closeFile; closeFile != nil {
				s.fileMux.Lock()
				if _, ok := s.files[closeFile.fileId]; ok {
					delete(s.files, closeFile.fileId)
					for _, c := range s.clients {
						delete(c.versions, closeFile.fileId)
					}
				}
				s.fileMux.Unlock()
				if closeFile.notifyChan != nil {
					closeFile.notifyChan <- true
				}
			}
			if submit := command.submit; submit != nil {
				var newChange *Change
				s.fileMux.Lock()
				if file, ok := s.files[submit.change.Id]; ok {
					c, err := file.Submit(submit.change.Change)
					if err == nil {
						newChange = &c
					}
				}
				s.fileMux.Unlock()

				if newChange != nil {
					for clientId, c := range s.clients {
						if clientId == submit.clientId {
							c.update <- MultiFileChange{
								Id: submit.change.Id,
								Change: Change{
									Version: newChange.Version,
								},
							}
						} else {
							c.update <- MultiFileChange{
								Id: submit.change.Id,
								Change: Change{
									Delta:   cloneDelta(newChange.Delta),
									Version: newChange.Version,
								},
							}
						}
					}
				}
			}
		}
	}
	// Close all clients
	for _, c := range s.clients {
		close(c.update)
	}
	s.clients = make(map[uint32]*multiFileClient)
	// Remove all files
	s.fileMux.Lock()
	s.files = make(map[uint32]*File)
	s.fileMux.Unlock()
	atomic.CompareAndSwapInt32(&s.running, 1, 0)
	s.stoppingChan <- true
}
