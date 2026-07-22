package vending

import (
	"sync"
	"testing"
)

func TestKioskLockerSerializesSameKiosk(t *testing.T) {
	locker := newKioskLocker()
	firstEntered := make(chan struct{})
	firstRelease := make(chan struct{})
	secondEntered := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		unlock := locker.lock("00010001")
		defer unlock()
		close(firstEntered)
		<-firstRelease
	}()
	<-firstEntered
	go func() {
		defer wg.Done()
		unlock := locker.lock("00010001")
		close(secondEntered)
		unlock()
	}()

	select {
	case <-secondEntered:
		t.Fatal("same-kiosk transaction entered before prior transaction released")
	default:
	}
	close(firstRelease)
	<-secondEntered
	wg.Wait()
}
