package game

import (
	"errors"
	"fmt"
	"sync"
)

const BoardSize = 16
const Width = 4

type Board [BoardSize]int

var solvedBoard = Board{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 0}

type Game struct {
	mu     sync.RWMutex
	id     string
	board  Board
	step   int
	solved bool
}

func NewGame(id string, b Board) *Game {
	return &Game{id: id, board: b}
}

func (g *Game) ID() string { return g.id }

func (g *Game) Snapshot() (Board, int, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.board, g.step, g.solved
}

func (g *Game) IsSolved() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.solved
}

// Move applies the tile move. Returns the new board and step on success.
// Errors mirror the spec: "does not exist" or "is not adjacent".
func (g *Game) Move(tile int) (Board, int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if tile < 1 || tile > 15 {
		return g.board, g.step, fmt.Errorf("tile %d does not exist on the board", tile)
	}
	tileIdx, emptyIdx := -1, -1
	for i, v := range g.board {
		if v == tile {
			tileIdx = i
		}
		if v == 0 {
			emptyIdx = i
		}
	}
	if tileIdx < 0 {
		return g.board, g.step, fmt.Errorf("tile %d does not exist on the board", tile)
	}

	tr, tc := tileIdx/Width, tileIdx%Width
	er, ec := emptyIdx/Width, emptyIdx%Width
	adj := (tr == er && abs(tc-ec) == 1) || (tc == ec && abs(tr-er) == 1)
	if !adj {
		return g.board, g.step, fmt.Errorf("tile %d is not adjacent to the empty cell", tile)
	}

	g.board[emptyIdx] = tile
	g.board[tileIdx] = 0
	g.step++
	return g.board, g.step, nil
}

func (g *Game) SetSolved(v bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.solved = v
}

func (g *Game) Result() Result {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return Result{
		GameID:     g.id,
		Solved:     g.solved,
		TotalSteps: g.step,
	}
}

type Result struct {
	GameID     string `json:"gameId"`
	Solved     bool   `json:"solved"`
	TotalSteps int    `json:"totalSteps"`
}

// Validate ensures the board has 16 distinct values 0..15.
func Validate(b []int) (Board, error) {
	if len(b) != BoardSize {
		return Board{}, fmt.Errorf("board must contain exactly 16 numbers")
	}
	var out Board
	seen := make(map[int]bool, BoardSize)
	for i, v := range b {
		if v < 0 || v > 15 {
			return Board{}, fmt.Errorf("board contains out-of-range value: %d", v)
		}
		if seen[v] {
			return Board{}, fmt.Errorf("board contains duplicate value: %d", v)
		}
		seen[v] = true
		out[i] = v
	}
	return out, nil
}

// IsSolvable applies the standard 4x4 sliding puzzle solvability rule:
// for an even-width board, solvable iff (inversions + row-of-blank-from-bottom) is odd.
func IsSolvable(b Board) bool {
	inv := 0
	emptyIdx := -1
	for i := 0; i < BoardSize; i++ {
		if b[i] == 0 {
			emptyIdx = i
			continue
		}
		for j := i + 1; j < BoardSize; j++ {
			if b[j] != 0 && b[i] > b[j] {
				inv++
			}
		}
	}
	if emptyIdx < 0 {
		return false
	}
	rowFromBottom := Width - emptyIdx/Width
	return (inv+rowFromBottom)%2 == 1
}

// ErrUnsolvable matches the spec error string.
var ErrUnsolvable = errors.New("board is not solvable")

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
