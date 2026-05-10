package game

import "sync"

// Registry stores all games by id, plus a pointer to the currently active game.
// MCP tools (get_state / move) operate on the active game.
type Registry struct {
	mu     sync.RWMutex
	games  map[string]*Game
	active *Game
}

func NewRegistry() *Registry {
	return &Registry{games: make(map[string]*Game)}
}

func (r *Registry) Register(g *Game) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.games[g.ID()] = g
	r.active = g
}

func (r *Registry) Active() *Game {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.active
}

func (r *Registry) Get(id string) (*Game, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	g, ok := r.games[id]
	return g, ok
}
