package ot

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/fmpwizard/go-quilljs-delta/delta"
)

func TestMultiFileSingleUpdate(t *testing.T) {
	s := NewMultiFileServer()
	go func() {
		s.Start()
	}()
	d := *delta.New(nil).Insert("Lorem ipsum", nil)
	if !(<-s.NewFile(1, d)) {
		t.Fatalf("Failed to create file 1")
	}
	client1Success, client1Updates := s.NewClient(1)
	if !(<-client1Success) {
		t.Fatalf("Failed to setup client 1")
	}
	<-client1Updates
	client2Success, client2Updates := s.NewClient(2)
	if !(<-client2Success) {
		t.Fatalf("Failed to setup client 2")
	}
	<-client2Updates
	d1 := delta.New(nil).Retain(5, nil).Insert("1", nil)
	s.Submit(1, MultiFileChange{
		Id: 1,
		Change: Change{
			Version: 0,
			Delta:   d1,
		},
	})
	count := 0
	var change1 MultiFileChange
	var change2 MultiFileChange
	for count < 2 {
		select {
		case change1 = <-client1Updates:
			count += 1
		case change2 = <-client2Updates:
			count += 1
		}
	}
	if change1.Id != 1 {
		t.Fatalf("Invalid file ID: %d", change1.Id)
	}
	if change1.Change.Version != 1 {
		t.Fatalf("Invalid version: %d", change1.Change.Version)
	}
	if change1.Change.Delta != nil {
		t.Fatalf("Ack should not have delta!")
	}
	if change2.Id != 1 {
		t.Fatalf("Invalid file ID: %d", change2.Id)
	}
	if change2.Change.Version != 1 {
		t.Fatalf("Invalid version: %d", change2.Change.Version)
	}
	if !reflect.DeepEqual(d1, change2.Change.Delta) {
		t.Fatalf("Invalid change!")
	}
	text := deltaToText(*s.CurrentChange(1).Change.Delta)
	if text != "Lorem1 ipsum" {
		t.Fatalf("Invalid text: %s", text)
	}
}

func TestFetchFileContent(t *testing.T) {
	s := NewMultiFileServer()
	go func() {
		s.Start()
	}()
	d := *delta.New(nil).Insert("Lorem ipsum", nil)
	if !(<-s.NewFile(10, d)) {
		t.Fatalf("Failed to create file")
	}

	change := s.CurrentChange(10)
	if change == nil {
		t.Fatalf("Failed to fetch file content")
	}
	if change.Id != 10 {
		t.Fatalf("Invalid file ID: %d", change.Id)
	}
	text := deltaToText(*change.Change.Delta)
	if text != "Lorem ipsum" {
		t.Fatalf("Invalid text: %s", text)
	}

	change2 := s.CurrentChange(20)
	if change2 != nil {
		t.Fatalf("Fetching not existed file should fail!")
	}
}

func TestMultiFileServerStop(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(2)
	wgChan := make(chan bool)

	s := NewMultiFileServer()
	go func() {
		s.Start()
		wg.Done()
	}()
	d := *delta.New(nil).Insert("Lorem ipsum", nil)
	if !(<-s.NewFile(10, d)) {
		t.Fatalf("Failed to create file")
	}
	clientSuccess, clientUpdates := s.NewClient(1)
	if !(<-clientSuccess) {
		t.Fatalf("Failed to setup client")
	}
	go func() {
		for range clientUpdates {
		}
		wg.Done()
	}()
	go func() {
		wg.Wait()
		wgChan <- true
	}()

	s.Stop()
	<-wgChan
	if s.Running() {
		t.Fatalf("Server is still running")
	}
}

func TestMultiFileServerCloseClient(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	wgChan := make(chan bool)

	s := NewMultiFileServer()
	go func() {
		s.Start()
	}()
	d := *delta.New(nil).Insert("Lorem ipsum", nil)
	if !(<-s.NewFile(10, d)) {
		t.Fatalf("Failed to create file")
	}
	clientSuccess, clientUpdates := s.NewClient(1)
	if !(<-clientSuccess) {
		t.Fatalf("Failed to setup client")
	}
	go func() {
		for {
			_, ok := <-clientUpdates
			if !ok {
				break
			}
		}
		wg.Done()
	}()
	go func() {
		wg.Wait()
		wgChan <- true
	}()

	s.CloseClient(1)
	<-wgChan
}

func TestMultiFileServerCloseFile(t *testing.T) {
	s := NewMultiFileServer()
	go func() {
		s.Start()
	}()
	d := *delta.New(nil).Insert("Lorem ipsum", nil)
	if !(<-s.NewFile(10, d)) {
		t.Fatalf("Failed to create file")
	}
	clientSuccess, clientUpdates := s.NewClient(1)
	if !(<-clientSuccess) {
		t.Fatalf("Failed to setup client")
	}
	go func() {
		for range clientUpdates {
		}
	}()
	notifyChan := make(chan bool)
	s.CloseFile(10, notifyChan)
	<-notifyChan

	if s.CurrentChange(10) != nil {
		t.Fatalf("Fetching not existed file should fail!")
	}
}

func TestMultiFileServerCloseFileNoNotify(t *testing.T) {
	s := NewMultiFileServer()
	go func() {
		s.Start()
	}()
	d := *delta.New(nil).Insert("Lorem ipsum", nil)
	if !(<-s.NewFile(10, d)) {
		t.Fatalf("Failed to create file")
	}
	clientSuccess, clientUpdates := s.NewClient(1)
	if !(<-clientSuccess) {
		t.Fatalf("Failed to setup client")
	}
	go func() {
		for range clientUpdates {
		}
	}()
	s.CloseFile(10, nil)

	for i := 0; i < 10; i++ {
		if s.CurrentChange(10) == nil {
			return
		}
		time.Sleep(100 + time.Millisecond)
	}
}

func TestMultiFileOnlyOneCanStart(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)

	s := NewMultiFileServer()
	go func() {
		s.Start()
		wg.Done()
	}()
	go func() {
		s.Start()
		wg.Done()
	}()
	wg.Wait()
}

func TestMultiFileClientIdConflict(t *testing.T) {
	s := NewMultiFileServer()
	go func() {
		s.Start()
	}()
	clientSuccess, _ := s.NewClient(1)
	if !(<-clientSuccess) {
		t.Fatalf("Failed to setup client")
	}
	client2Success, _ := s.NewClient(1)
	if <-client2Success {
		t.Fatalf("Different clients should not have the same ID!")
	}
}

func TestMultiFileFileIdConflict(t *testing.T) {
	s := NewMultiFileServer()
	go func() {
		s.Start()
	}()
	d := *delta.New(nil).Insert("Lorem ipsum", nil)
	if !(<-s.NewFile(10, d)) {
		t.Fatalf("Failed to create file")
	}
	if <-s.NewFile(10, d) {
		t.Fatalf("Different files should not have the same ID!")
	}
}

func TestMultiFileBroadcastNewFile(t *testing.T) {
	s := NewMultiFileServer()
	go func() {
		s.Start()
	}()
	client1Success, client1Updates := s.NewClient(1)
	if !(<-client1Success) {
		t.Fatalf("Failed to setup client 1")
	}
	client2Success, client2Updates := s.NewClient(2)
	if !(<-client2Success) {
		t.Fatalf("Failed to setup client 2")
	}
	d := delta.New(nil).Insert("Lorem ipsum", nil)
	if !(<-s.NewFile(123, *d)) {
		t.Fatalf("Failed to create file 1")
	}
	count := 0
	var change1 MultiFileChange
	var change2 MultiFileChange
	for count < 2 {
		select {
		case change1 = <-client1Updates:
			count += 1
		case change2 = <-client2Updates:
			count += 1
		}
	}
	if change1.Id != 123 {
		t.Fatalf("Invalid file ID: %d", change1.Id)
	}
	if change1.Change.Version != 0 {
		t.Fatalf("Invalid version: %d", change1.Change.Version)
	}
	if !reflect.DeepEqual(d, change1.Change.Delta) {
		t.Fatalf("Invalid change 1 delta!")
	}
	if change2.Id != 123 {
		t.Fatalf("Invalid file ID: %d", change2.Id)
	}
	if change2.Change.Version != 0 {
		t.Fatalf("Invalid version: %d", change2.Change.Version)
	}
	if !reflect.DeepEqual(d, change2.Change.Delta) {
		t.Fatalf("Invalid change 2 delta!")
	}
}
