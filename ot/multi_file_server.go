package ot

import (
	"sync/atomic"

	"github.com/fmpwizard/go-quilljs-delta/delta"
)

type ChangeFunction func(d delta.Delta) delta.Delta
type ChangeAllFunction func(changes []MultiFileChange) []MultiFileChange

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

type multiFileChange struct {
	f        ChangeFunction
	fileId   uint32
	clientId uint32
}

type multiFileChangeAll struct {
	f        ChangeAllFunction
	clientId uint32
}

type multiFileSubmit struct {
	change   MultiFileChange
	clientId uint32
}

type multiFileCloseFile struct {
	fileId     uint32
	notifyChan chan<- bool
}

type multiFileFetch struct {
	fileId    uint32
	fetchChan chan<- *MultiFileChange
}

type multiFileCommand struct {
	newClient     *multiFileNewClient
	closeClientId *uint32
	newFile       *multiFileNewFile
	closeFile     *multiFileCloseFile
	submit        *multiFileSubmit
	change        *multiFileChange
	changeAll     *multiFileChangeAll
	fetch         *multiFileFetch
	fetchAll      chan<- []MultiFileChange
}

type MultiFileServer struct {
	files   map[uint32]*File
	clients map[uint32]*multiFileClient

	commands     chan multiFileCommand
	stoppingChan chan bool

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
	c := make(chan *MultiFileChange)

	s.commands <- multiFileCommand{
		fetch: &multiFileFetch{
			fileId:    fileId,
			fetchChan: c,
		},
	}
	return <-c
}

func (s *MultiFileServer) AllChanges() []MultiFileChange {
	c := make(chan []MultiFileChange)

	s.commands <- multiFileCommand{
		fetchAll: c,
	}
	return <-c
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

func (s *MultiFileServer) FetchAndChange(clientId uint32, fileId uint32, f ChangeFunction) {
	s.commands <- multiFileCommand{
		change: &multiFileChange{
			clientId: clientId,
			fileId:   fileId,
			f:        f,
		},
	}
}

func (s *MultiFileServer) FetchAndChangeAll(clientId uint32, f ChangeAllFunction) {
	s.commands <- multiFileCommand{
		changeAll: &multiFileChangeAll{
			clientId: clientId,
			f:        f,
		},
	}
}

func (s *MultiFileServer) Append(clientId uint32, fileId uint32, text string) {
	s.FetchAndChange(clientId, fileId, func(d delta.Delta) delta.Delta {
		return *delta.New(nil).Retain(d.Length(), nil).Insert(text, nil)
	})
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
					for fileId, file := range s.files {
						change := file.CurrentChange()
						c.versions[fileId] = change.Version
						c.update <- MultiFileChange{
							Id:     fileId,
							Change: change,
						}
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
				_, ok := s.files[newFile.fileId]
				if ok {
					// File ID exists, abort file creation
					newFile.setup <- false
				} else {
					file := NewFile(newFile.d)
					s.files[newFile.fileId] = file
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
				if _, ok := s.files[closeFile.fileId]; ok {
					delete(s.files, closeFile.fileId)
					for _, c := range s.clients {
						delete(c.versions, closeFile.fileId)
					}
				}
				if closeFile.notifyChan != nil {
					closeFile.notifyChan <- true
				}
			}
			if submit := command.submit; submit != nil {
				var newChange *Change
				if file, ok := s.files[submit.change.Id]; ok {
					c, err := file.Submit(submit.change.Change)
					if err == nil {
						newChange = &c
					}
				}

				if newChange != nil {
					s.broadcastChange(*newChange, submit.change.Id, submit.clientId)
				}
			}
			if changeCommand := command.change; changeCommand != nil {
				var newChange *Change
				if file, ok := s.files[changeCommand.fileId]; ok {
					currentChange := file.CurrentChange()
					d := changeCommand.f(*currentChange.Delta)
					c, err := file.Submit(Change{
						Version: currentChange.Version,
						Delta:   &d,
					})
					if err == nil {
						newChange = &c
					}
				}

				if newChange != nil {
					s.broadcastChange(*newChange, changeCommand.fileId, changeCommand.clientId)
				}
			}
			if changeAllCommand := command.changeAll; changeAllCommand != nil {
				contents := make([]MultiFileChange, 0)
				for id, file := range s.files {
					contents = append(contents, MultiFileChange{
						Id:     id,
						Change: file.CurrentChange(),
					})
				}
				changes := changeAllCommand.f(contents)
				broadcastChanges := make([]MultiFileChange, 0)
				for _, change := range changes {
					if file, ok := s.files[change.Id]; ok {
						c, err := file.Submit(change.Change)
						if err == nil {
							broadcastChanges = append(broadcastChanges, MultiFileChange{
								Id:     change.Id,
								Change: c,
							})
						}
					}
				}
				for _, change := range broadcastChanges {
					s.broadcastChange(change.Change, change.Id, changeAllCommand.clientId)
				}
			}
			if fetchCommand := command.fetch; fetchCommand != nil {
				if file, ok := s.files[fetchCommand.fileId]; ok {
					fetchCommand.fetchChan <- &MultiFileChange{
						Id:     fetchCommand.fileId,
						Change: file.CurrentChange(),
					}
				} else {
					fetchCommand.fetchChan <- nil
				}
			}
			if fetchAll := command.fetchAll; fetchAll != nil {
				changes := make([]MultiFileChange, 0)
				for id, file := range s.files {
					changes = append(changes, MultiFileChange{
						Id:     id,
						Change: file.CurrentChange(),
					})
				}
				fetchAll <- changes
			}
		}
	}
	// Close all clients
	for _, c := range s.clients {
		close(c.update)
	}
	s.clients = make(map[uint32]*multiFileClient)
	// Remove all files
	s.files = make(map[uint32]*File)
	atomic.CompareAndSwapInt32(&s.running, 1, 0)
	s.stoppingChan <- true
}

func (s *MultiFileServer) broadcastChange(change Change, fileId uint32, sourceClientId uint32) {
	for clientId, c := range s.clients {
		if clientId == sourceClientId {
			c.update <- MultiFileChange{
				Id: fileId,
				Change: Change{
					Version: change.Version,
				},
			}
		} else {
			c.update <- MultiFileChange{
				Id: fileId,
				Change: Change{
					Delta:   cloneDelta(change.Delta),
					Version: change.Version,
				},
			}
		}
	}
}
