package discord

import "sync"

// Store caches the latest Presence for the single tracked user and fans out
// updates to any subscribers (e.g. the websocket hub).
type Store struct {
	mu   sync.RWMutex
	last *Presence

	subMu sync.Mutex
	subs  map[chan *Presence]struct{}
}

func NewStore() *Store {
	return &Store{
		subs: make(map[chan *Presence]struct{}),
	}
}

func (s *Store) Get() *Presence {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.last
}

func (s *Store) Set(p *Presence) {
	s.mu.Lock()
	s.last = p
	s.mu.Unlock()

	s.subMu.Lock()
	defer s.subMu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- p:
		default:
		}
	}
}

// Subscribe returns a channel that receives every future presence update.
// Call the returned func to unsubscribe and release the channel.
func (s *Store) Subscribe() (<-chan *Presence, func()) {
	ch := make(chan *Presence, 4)

	s.subMu.Lock()
	s.subs[ch] = struct{}{}
	s.subMu.Unlock()

	unsubscribe := func() {
		s.subMu.Lock()
		defer s.subMu.Unlock()
		if _, ok := s.subs[ch]; ok {
			delete(s.subs, ch)
			close(ch)
		}
	}
	return ch, unsubscribe
}
