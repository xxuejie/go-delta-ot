package ot

import (
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fmpwizard/go-quilljs-delta/delta"
)

func deltaToText(d delta.Delta) string {
	result := make([]rune, 0)
	for _, op := range d.Ops {
		if op.Insert != nil {
			result = append(result, op.Insert...)
		}
	}
	return string(result)
}

func TestNewClient(t *testing.T) {
	d := *delta.New(nil).Insert("Lorem ipsum", nil)
	s := NewServer(d)
	go func() {
		s.Start()
	}()
	_, change1Updates, err := s.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	change1 := <-change1Updates
	if change1.Version != 0 {
		t.Fatalf("Invalid version: %d", change1.Version)
	}
	text := deltaToText(*change1.Delta)
	if text != "Lorem ipsum" {
		t.Fatalf("Invalid text: %s", text)
	}
}

func TestSingleUpdate(t *testing.T) {
	d := *delta.New(nil).Insert("Lorem ipsum", nil)
	s := NewServer(d)
	go func() {
		s.Start()
	}()
	client1, client1Updates, err := s.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	<-client1Updates
	_, client2Updates, err := s.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	<-client2Updates
	d1 := *delta.New(nil).Retain(5, nil).Insert("1", nil)
	s.Submit(client1, d1, 0)
	count := 0
	var change1 Change
	var change2 Change
	for count < 2 {
		select {
		case change1 = <-client1Updates:
			count += 1
		case change2 = <-client2Updates:
			count += 1
		}
	}
	if change1.Version != 1 {
		t.Fatalf("Invalid version: %d", change1.Version)
	}
	if change1.Delta != nil {
		t.Fatalf("Ack should not have delta!")
	}
	if change2.Version != 1 {
		t.Fatalf("Invalid version: %d", change2.Version)
	}
	if !reflect.DeepEqual(d1, *change2.Delta) {
		t.Fatalf("Invalid change!")
	}
	text := deltaToText(*s.CurrentChange().Delta)
	if text != "Lorem1 ipsum" {
		t.Fatalf("Invalid text: %s", text)
	}
}

func TestNewClientWithVersions(t *testing.T) {
	d := *delta.New(nil).Insert("Lorem ipsum", nil)
	s := NewServer(d)
	go func() {
		s.Start()
	}()
	client1, client1Updates, err := s.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	<-client1Updates
	d1 := *delta.New(nil).Retain(5, nil).Insert("1", nil)
	s.Submit(client1, d1, 0)
	<-client1Updates
	_, change2Updates, err := s.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	change2 := <-change2Updates
	if change2.Version != 1 {
		t.Fatalf("Invalid version: %d", change2.Version)
	}
	text := deltaToText(*change2.Delta)
	if text != "Lorem1 ipsum" {
		t.Fatalf("Invalid text: %s", text)
	}
}

func TestMultipleUpdate(t *testing.T) {
	d := *delta.New(nil).Insert("Lorem ipsum", nil)
	s := NewServer(d)
	go func() {
		s.Start()
	}()
	d1 := *delta.New(nil).Retain(5, nil).Insert("a", nil)
	d2 := *delta.New(nil).Retain(8, nil).Insert("1", nil)
	transformedD2 := *delta.New(nil).Retain(9, nil).Insert("1", nil)

	c1ch := make(chan uint32)
	c2ch := make(chan uint32)
	errChan := make(chan error)
	wgChan := make(chan bool)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		client1, client1Updates, err := s.NewClient()
		if err != nil {
			errChan <- err
			return
		}
		<-client1Updates
		c1ch <- client1
		change1 := <-client1Updates
		if change1.Version != 1 {
			errChan <- fmt.Errorf("Invalid version: %d", change1.Version)
			return
		}
		if change1.Delta != nil {
			errChan <- fmt.Errorf("Ack should not have delta!")
			return
		}
		change2 := <-client1Updates
		if change2.Version != 2 {
			errChan <- fmt.Errorf("Invalid version: %d", change2.Version)
			return
		}
		if !reflect.DeepEqual(*change2.Delta, transformedD2) {
			errChan <- fmt.Errorf("Invalid version 2 change to client 1!")
			return
		}
		wg.Done()
	}()
	go func() {
		client2, client2Updates, err := s.NewClient()
		if err != nil {
			errChan <- err
			return
		}
		<-client2Updates
		c2ch <- client2
		change3 := <-client2Updates
		if change3.Version != 1 {
			errChan <- fmt.Errorf("Invalid version: %d", change3.Version)
			return
		}
		if !reflect.DeepEqual(*change3.Delta, d1) {
			errChan <- fmt.Errorf("Invalid version 1 change to client 2!")
			return
		}
		change4 := <-client2Updates
		if change4.Version != 2 {
			errChan <- fmt.Errorf("Invalid veresion: %d", change4.Version)
			return
		}
		if change4.Delta != nil {
			errChan <- fmt.Errorf("Ack should not have delta!")
			return
		}
		wg.Done()
	}()
	client1 := <-c1ch
	client2 := <-c2ch
	s.Submit(client1, d1, 0)
	s.Submit(client2, d2, 0)
	go func() {
		wg.Wait()
		wgChan <- true
	}()

	select {
	case <-wgChan:
		text := deltaToText(*s.CurrentChange().Delta)
		if text != "Lorema ip1sum" {
			t.Fatalf("Invalid text: %s", text)
		}
	case err := <-errChan:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatalf("Test is timed out! Maybe there's a deadlock?")
	}
}

func TestMultipleUpdateMultipleVersions(t *testing.T) {
	d := *delta.New(nil).Insert("Lorem ipsum", nil)
	s := NewServer(d)
	go func() {
		s.Start()
	}()
	d1 := *delta.New(nil).Retain(1, nil).Delete(1)
	d2 := *delta.New(nil).Retain(6, nil).Insert("ab", nil)
	d3 := *delta.New(nil).Retain(11, nil).Insert("4", nil)
	transformedD3 := *delta.New(nil).Retain(12, nil).Insert("4", nil)

	c1ch := make(chan uint32)
	c2ch := make(chan uint32)
	errChan := make(chan error)
	wgChan := make(chan bool)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		client1, client1Updates, err := s.NewClient()
		if err != nil {
			errChan <- err
			return
		}
		<-client1Updates
		c1ch <- client1
		change1 := <-client1Updates
		if change1.Version != 1 {
			errChan <- fmt.Errorf("Invalid version: %d", change1.Version)
			return
		}
		if change1.Delta != nil {
			errChan <- fmt.Errorf("Ack should not have delta!")
			return
		}
		change2 := <-client1Updates
		if change2.Version != 2 {
			errChan <- fmt.Errorf("Invalid version: %d", change2.Version)
			return
		}
		if change2.Delta != nil {
			errChan <- fmt.Errorf("Ack should not have delta!")
			return
		}
		change3 := <-client1Updates
		if change3.Version != 3 {
			errChan <- fmt.Errorf("Invalid version: %d", change3.Version)
			return
		}
		if !reflect.DeepEqual(*change3.Delta, transformedD3) {
			errChan <- fmt.Errorf("Invalid version 3 change to client 1!")
			return
		}
		wg.Done()
	}()
	go func() {
		client2, client2Updates, err := s.NewClient()
		if err != nil {
			errChan <- err
			return
		}
		<-client2Updates
		c2ch <- client2
		change3 := <-client2Updates
		if change3.Version != 1 {
			errChan <- fmt.Errorf("Invalid version: %d", change3.Version)
			return
		}
		if !reflect.DeepEqual(*change3.Delta, d1) {
			errChan <- fmt.Errorf("Invalid version 1 change to client 2!")
			return
		}
		change4 := <-client2Updates
		if change4.Version != 2 {
			errChan <- fmt.Errorf("Invalid veresion: %d", change4.Version)
			return
		}
		if !reflect.DeepEqual(*change4.Delta, d2) {
			errChan <- fmt.Errorf("Invalid version 2 change to client 2!")
			return
		}
		change5 := <-client2Updates
		if change5.Version != 3 {
			errChan <- fmt.Errorf("Invalid veresion: %d", change5.Version)
			return
		}
		if change5.Delta != nil {
			errChan <- fmt.Errorf("Ack should not have delta!")
			return
		}
		wg.Done()
	}()
	client1 := <-c1ch
	client2 := <-c2ch
	s.Submit(client1, d1, 0)
	s.Submit(client1, d2, 1)
	s.Submit(client2, d3, 0)
	go func() {
		wg.Wait()
		wgChan <- true
	}()

	select {
	case <-wgChan:
		text := deltaToText(*s.CurrentChange().Delta)
		if text != "Lrem iabpsum4" {
			t.Fatalf("Invalid text: %s", text)
		}
	case err := <-errChan:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatalf("Test is timed out! Maybe there's a deadlock?")
	}
}

func TestStop(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	wgChan := make(chan bool)

	d := *delta.New(nil).Insert("Lorem ipsum", nil)
	s := NewServer(d)
	go func() {
		s.Start()
		wg.Done()
	}()
	_, u, err := s.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	<-u
	go func() {
		wg.Wait()
		wgChan <- true
	}()

	s.Stop()
	select {
	case <-wgChan:
		if s.Running() {
			t.Fatalf("Server is still running")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Test is timed out! Maybe there's a deadlock?")
	}
}

func TestCloseClient(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	wgChan := make(chan bool)

	d := *delta.New(nil).Insert("Lorem ipsum", nil)
	s := NewServer(d)
	go func() {
		s.Start()
	}()
	client1, client1Updates, err := s.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			_, ok := <-client1Updates
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

	s.Close(client1)
	select {
	case <-wgChan:
	case <-time.After(5 * time.Second):
		t.Fatalf("Test is timed out! Maybe there's a deadlock?")
	}
}

func TestConflictIds(t *testing.T) {
	d := *delta.New(nil).Insert("Lorem ipsum", nil)
	s := NewServer(d)
	go func() {
		s.Start()
	}()
	client1, u, err := s.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	<-u
	atomic.StoreUint32(&s.lastClientId, client1-1)
	_, c, err := s.NewClient()
	_, ok := <-c
	if ok {
		t.Fatalf("Channel is not closed!")
	}
}

func TestOnlyOneCanStart(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)

	d := *delta.New(nil).Insert("Lorem ipsum", nil)
	s := NewServer(d)
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

func TestSubmitInvalidVersion(t *testing.T) {
	d := *delta.New(nil).Insert("Lorem ipsum", nil)
	s := NewServer(d)
	go func() {
		s.Start()
	}()
	client1, client1Updates, err := s.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for i := 0; i < 2; i++ {
			<-client1Updates
		}
		wg.Done()
	}()
	s.Submit(client1, *delta.New(nil).Insert("a", nil), 10)
	s.Submit(client1, *delta.New(nil).Insert("b", nil), 0)
	wg.Wait()
	text := deltaToText(*s.CurrentChange().Delta)
	if text != "bLorem ipsum" {
		t.Fatalf("Invalid text: %s", text)
	}
}

func TestSubmitTooOldVersion(t *testing.T) {
	d := *delta.New(nil).Insert("Lorem ipsum", nil)
	s := NewServer(d)
	s.version = 10
	go func() {
		s.Start()
	}()
	client1, client1Updates, err := s.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for i := 0; i < 2; i++ {
			<-client1Updates
		}
		wg.Done()
	}()
	s.Submit(client1, *delta.New(nil).Insert("a", nil), 6)
	s.Submit(client1, *delta.New(nil).Insert("b", nil), 10)
	wg.Wait()
	text := deltaToText(*s.CurrentChange().Delta)
	if text != "bLorem ipsum" {
		t.Fatalf("Invalid text: %s", text)
	}
}

func TestChangeAfterInitialUpdate(t *testing.T) {
	s := NewServer(*delta.New(nil))
	go func() {
		s.Start()
	}()

	client1, change1Updates, err := s.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	change1 := <-change1Updates
	d1 := *delta.New(nil).Insert("12", nil)
	s.Submit(client1, d1, 0)
	<-change1Updates

	text := deltaToText(*change1.Delta)
	if text != "" {
		t.Fatalf("Unexpected text: %s", text)
	}
}
