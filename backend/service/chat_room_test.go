package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// joinAndLeave does what one WebSocket connection does to a room: take it,
// register, then unregister and let it go.
func joinAndLeave(s *ChatService, orderID, chatID uuid.UUID) {
	room := s.getOrCreateRoom(context.Background(), orderID, chatID)
	client := &ChatClient{Send: make(chan []byte, 1)}
	room.Register <- client
	room.Unregister <- client
	s.releaseRoom(room)
}

// A room must survive while any connection still holds it, and disappear once
// the last one leaves.
func TestChatRoomLifecycle(t *testing.T) {
	s := NewChatService(nil, nil)
	orderID, chatID := uuid.New(), uuid.New()

	first := s.getOrCreateRoom(context.Background(), orderID, chatID)
	second := s.getOrCreateRoom(context.Background(), orderID, chatID)
	if first != second {
		t.Fatal("two connections to the same order got different rooms")
	}

	s.releaseRoom(first)
	s.mu.RLock()
	_, stillThere := s.rooms[orderID]
	s.mu.RUnlock()
	if !stillThere {
		t.Error("room was retired while a connection still held it")
	}

	s.releaseRoom(second)
	s.mu.RLock()
	_, gone := s.rooms[orderID]
	s.mu.RUnlock()
	if gone {
		t.Error("room outlived its last connection")
	}

	select {
	case <-first.done:
	case <-time.After(time.Second):
		t.Error("the room's goroutine was not signalled to stop")
	}
}

// Connections joining and leaving the same order concurrently must not wedge.
//
// This is the failure the reference count exists to prevent: a connection that
// had taken the room pointer, and reached its blocking send on Register just
// after the previous connection's departure retired the room, blocked forever
// on a goroutine that had already returned. Run with -race, this also covers
// the map access itself.
func TestChatRoomChurnDoesNotBlock(t *testing.T) {
	s := NewChatService(nil, nil)
	orderID, chatID := uuid.New(), uuid.New()

	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 20; j++ {
					joinAndLeave(s, orderID, chatID)
				}
			}()
		}
		wg.Wait()
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("connection churn deadlocked: a room was retired while a connection was still joining it")
	}

	s.mu.RLock()
	remaining := len(s.rooms)
	s.mu.RUnlock()
	if remaining != 0 {
		t.Errorf("%d rooms left behind after every connection closed", remaining)
	}
}
