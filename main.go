package main

import (
	"log"
	"net/http"
	"strings"

	"github.com/coder/websocket"
)

type Direction string

const (
	North Direction = "north"
	East  Direction = "east"
	South Direction = "south"
	West  Direction = "west"
	Up    Direction = "up"
	Down  Direction = "down"
)

var (
	allDirections = []Direction{North, East, South, West, Up, Down}
	opposite      = map[Direction]Direction{
		North: South,
		South: North,
		East:  West,
		West:  East,
		Up:    Down,
		Down:  Up,
	}
)

type Command struct {
	Player *Player
	Text   string
}

type Room struct {
	Name        string
	Description string
	Exits       map[Direction]*Room
}

type Player struct {
	Name string
	Room *Room
	Send chan string
}

type World struct {
	Join      chan *Player
	Leave     chan *Player
	Command   chan Command
	Players   map[*Player]bool
	StartRoom *Room
}

func NewWorld() *World {
	palace := &Room{
		Name:        "Palace",
		Description: "You are in a grand palace.",
		Exits:       make(map[Direction]*Room),
	}
	square := &Room{
		Name:        "Town Square",
		Description: "You are in the bustling town square.",
		Exits:       make(map[Direction]*Room),
	}

	palace.Exits[South] = square
	square.Exits[North] = palace

	return &World{
		Join:      make(chan *Player),
		Leave:     make(chan *Player),
		Command:   make(chan Command),
		Players:   make(map[*Player]bool),
		StartRoom: palace,
	}
}

func (w *World) broadcastToRoom(room *Room, msg string, except *Player) {
	for p := range w.Players {
		if p.Room == room && p != except {
			select {
			case p.Send <- msg:
			default:
			}
		}
	}
}

func (w *World) Run() {
	for {
		select {
		case p := <-w.Join:
			w.Players[p] = true
			p.Room = w.StartRoom
			p.Send <- "Welcome to MaoXianMUD!"
			p.Send <- "Type 'help'"
			p.Send <- w.describeRoom(p)
			w.broadcastToRoom(p.Room, "*** "+p.Name+" enters.", p)

		case p := <-w.Leave:
			if p.Room != nil {
				w.broadcastToRoom(p.Room, "*** "+p.Name+" leaves.", p)
			}
			delete(w.Players, p)
			close(p.Send)

		case cmd := <-w.Command:
			w.handleCommand(cmd)
		}
	}
}

func (w *World) describeRoom(p *Player) string {
	room := p.Room
	var b strings.Builder

	b.WriteString(room.Name)
	b.WriteString("\n")
	b.WriteString(room.Description)
	b.WriteString("\n\nExits:\n")

	hasExits := false
	for _, dir := range allDirections {
		if _, ok := room.Exits[dir]; ok {
			b.WriteString("  ")
			b.WriteString(string(dir))
			b.WriteString("\n")
			hasExits = true
		}
	}
	if !hasExits {
		b.WriteString("  none\n")
	}

	b.WriteString("\nPlayers:\n")
	hasPlayers := false
	for other := range w.Players {
		if other.Room == room {
			b.WriteString(" - ")
			b.WriteString(other.Name)
			b.WriteString("\n")
			hasPlayers = true
		}
	}
	if !hasPlayers {
		b.WriteString("  none\n")
	}

	return b.String()
}

func (w *World) movePlayer(p *Player, dir Direction) {
	dest, ok := p.Room.Exits[dir]
	if !ok {
		p.Send <- "You can't go that way."
		return
	}

	w.broadcastToRoom(p.Room, "*** "+p.Name+" leaves "+string(dir)+".", p)
	p.Room = dest
	w.broadcastToRoom(p.Room, "*** "+p.Name+" enters from the "+string(opposite[dir])+".", p)
	p.Send <- w.describeRoom(p)
}

func (w *World) handleCommand(cmd Command) {
	line := strings.TrimSpace(cmd.Text)

	if line == "" {
		return
	}

	parts := strings.SplitN(line, " ", 2)

	switch strings.ToLower(parts[0]) {

	case "help":
		cmd.Player.Send <- `
Commands:
help
look
say <message>
north, east, south, west, up, down
`

	case "look":
		cmd.Player.Send <- w.describeRoom(cmd.Player)

	case "say":
		if len(parts) < 2 {
			cmd.Player.Send <- "Usage: say <message>"
			return
		}

		w.broadcastToRoom(cmd.Player.Room, cmd.Player.Name+": "+parts[1], nil)

	default:
		if dir, ok := parseDirection(parts[0]); ok {
			w.movePlayer(cmd.Player, dir)
			return
		}
		cmd.Player.Send <- "Unknown command"
	}
}

func parseDirection(word string) (Direction, bool) {
	dir := Direction(strings.ToLower(word))
	for _, d := range allDirections {
		if dir == d {
			return d, true
		}
	}
	return "", false
}

func playerName(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "guest"
	}
	if len(name) > 32 {
		name = name[:32]
	}
	return name
}

func main() {
	world := NewWorld()
	go world.Run()

	http.Handle("/", http.FileServer(http.Dir("./static")))

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		player := &Player{
			Name: playerName(r.URL.Query().Get("name")),
			Send: make(chan string, 32),
		}

		world.Join <- player

		go func() {
			for msg := range player.Send {
				conn.Write(r.Context(), websocket.MessageText, []byte(msg))
			}
		}()

		for {
			_, data, err := conn.Read(r.Context())
			if err != nil {
				break
			}

			world.Command <- Command{
				Player: player,
				Text:   string(data),
			}
		}

		world.Leave <- player
	})

	log.Println("Listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
