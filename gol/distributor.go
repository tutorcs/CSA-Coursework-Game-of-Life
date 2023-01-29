https://tutorcs.com
WeChat: cstutorcs
QQ: 749389476
Email: tutorcs@163.com
package gol

import (
	"fmt"
	"sync"
	"time"

	"uk.ac.bris.cs/gameoflife/util"
)

type distributorChannels struct {
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	ioInput    <-chan uint8
	keyPresses <-chan rune
}

// World type the world struct
type World struct {
	Width       int
	Height      int
	Turns       int
	Threads     int
	CurLattice  *Slice
	PrevLattice *Slice
}

// Slice is a 2D slice of cells.
type Slice struct {
	Width   int
	Height  int
	Threads int
	Cells   [][]bool
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p Params, c distributorChannels) {

	//Create a 2D slice to store the world.
	world := &World{
		Height:  p.ImageHeight,
		Width:   p.ImageWidth,
		Turns:   p.Turns,
		Threads: p.Threads,
	}
	world.CurLattice = NewSlice(p)
	world.PrevLattice = NewSlice(p)

	//load the file    world <- file
	filename := fmt.Sprintf("%dx%d", p.ImageHeight, p.ImageWidth)
	c.ioCommand <- ioInput
	c.ioFilename <- filename
	for y := 0; y < world.Height; y++ {
		for x := 0; x < world.Width; x++ {
			world.CurLattice.InitialCellState(x, y, <-c.ioInput == 255)
		}
	}

	// Execute all turns of the Game of Life.
	turn := 0
	world.CellFlipped(turn, c) // send the initial state to the channel
	ticker := time.NewTicker(time.Second * 2)
	for turn < p.Turns {
		select {
		case <-ticker.C:
			world.AliveCount(turn, c)
		case key := <-c.keyPresses:
			switch key {
			case 's':
				world.GenerateFile(filename, turn, p, c)
			case 'q':
				world.GenerateFile(filename, turn, p, c)
				// Make sure that the Io has finished any output before exiting.
				c.ioCommand <- ioCheckIdle
				<-c.ioIdle
				c.events <- StateChange{turn, Quitting}
				close(c.events)
				return
			case 'p':
				c.events <- StateChange{turn, Paused}
				for {
					key := <-c.keyPresses
					if key == 'p' {
						c.events <- StateChange{turn, Executing}
						fmt.Println("Continuing")
						break
					}
				}
			}
		default:
			world.Run()
			world.CellFlipped(turn, c)
			c.events <- TurnComplete{CompletedTurns: turn} //send the completed turn to the channel
			turn++
		}
	}

	//Report the final state using FinalTurnCompleteEvent.
	world.FinalTurnComplete(c)
	//save the file and output.
	filename = fmt.Sprintf("%dx%dx%d-%d", p.ImageHeight, p.ImageWidth, p.Threads, turn)
	c.ioCommand <- ioOutput
	c.ioFilename <- filename
	for y := 0; y < world.Height; y++ {
		for x := 0; x < world.Width; x++ {
			var value uint8
			if world.CurLattice.CellState(x, y) {
				value = 255 //alive
			} else {
				value = 0 //dead
			}
			c.ioOutput <- value
		}
	}
	c.events <- ImageOutputComplete{
		Filename:       filename,
		CompletedTurns: turn,
	}

	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle
	c.events <- StateChange{turn, Quitting}
	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}

// NewSlice creates a new slice.
func NewSlice(p Params) *Slice {
	slice := &Slice{
		Width:   p.ImageWidth,
		Height:  p.ImageHeight,
		Threads: p.Threads,
	}
	slice.Cells = make([][]bool, slice.Height)
	for i := range slice.Cells {
		slice.Cells[i] = make([]bool, slice.Width)
	}
	return slice
}

// Run runs the distributor. (go by example mockup)
func (w *World) Run() {
	wg := sync.WaitGroup{}
	for y := 0; y < w.Height; {
		for i := 0; i < w.Threads; i++ {
			wg.Add(1)
			go func(wg *sync.WaitGroup, y int) {
				defer wg.Done()
				for x := 0; x < w.Width; x++ {
					w.PrevLattice.InitialCellState(x, y, w.CurLattice.NextStep(x, y))
				}
			}(&wg, y)
			y++
			if y == w.Height {
				break
			}
		}
	}
	wg.Wait()
	w.PrevLattice, w.CurLattice = w.CurLattice, w.PrevLattice
}

// AliveCount get the alive cells number and send to channel.
func (w *World) AliveCount(turn int, c distributorChannels) {
	var count = 0
	for y := 0; y < w.Height; y++ {
		for x := 0; x < w.Width; x++ {
			if w.CurLattice.CellState(x, y) {
				count++
			}
		}
	}
	c.events <- AliveCellsCount{
		CompletedTurns: turn,
		CellsCount:     count,
	}
}

// CellFlipped send the flipped cell to the channel.
func (w *World) CellFlipped(turn int, c distributorChannels) {
	for y := 0; y < w.Height; y++ {
		for x := 0; x < w.Width; x++ {
			if w.CurLattice.CellState(x, y) != w.PrevLattice.CellState(x, y) { //cell position changed
				c.events <- CellFlipped{
					CompletedTurns: turn, Cell: util.Cell{
						X: x, Y: y,
					},
				}
			}
		}
	}
}

// FinalTurnComplete finish the turns and send to channel.
func (w *World) FinalTurnComplete(c distributorChannels) {
	var cells []util.Cell
	for y := 0; y < w.Height; y++ {
		for x := 0; x < w.Width; x++ {
			if w.CurLattice.CellState(x, y) {
				cells = append(cells, util.Cell{X: x, Y: y})
			}
		}
	}
	c.events <- FinalTurnComplete{ //send to channel
		CompletedTurns: w.Turns,
		Alive:          cells,
	}
}

func (s *Slice) InitialCellState(x, y int, alive bool) {
	if x >= 0 && x < s.Width && y >= 0 && y < s.Height {
		s.Cells[x][y] = alive
	}
}

// CellState returns the position of the cell .
func (s *Slice) CellState(x, y int) bool {
	if x >= 0 && x < s.Width && y >= 0 && y < s.Height {
		return s.Cells[x][y]
	} else {
		if x == s.Width || x == (-1) {
			x = (x + s.Width) % s.Width //closed domain
		}
		if y == s.Height || y == (-1) {
			y = (y + s.Height) % s.Height //closed domain
		}
		return s.Cells[x][y]
	}
}

func (s *Slice) NextStep(x, y int) bool {
	alive := 0
	for i := -1; i <= 1; i++ {
		for j := -1; j <= 1; j++ {
			if s.CellState(x+i, y+j) && (i != 0 || j != 0) {
				alive++
			}
		}
	}
	return alive == 3 || (s.CellState(x, y) && alive == 2)
}

func (w *World) GenerateFile(filename string, turn int, p Params, c distributorChannels) {
	filename = fmt.Sprintf("%dx%dx%d-%d", p.ImageHeight, p.ImageWidth, p.Threads, turn)
	c.events <- ImageOutputComplete{ //generate file
		Filename:       filename,
		CompletedTurns: turn,
	}
}
