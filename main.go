package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

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
	actionCooldown = 5 * time.Second
)

type Command struct {
	Player *Player
	Text   string
}

type Room struct {
	Name        string
	Description string
	Exits       map[Direction]*Room
	Items       []*Item
}

type Item struct {
	Name string
}

type Player struct {
	Name       string
	Room       *Room
	Inventory  []*Item
	LastAction time.Time
	Send       chan string
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
		Items: []*Item{
			{Name: "golden scepter"},
		},
	}
	square := &Room{
		Name:        "Town Square",
		Description: "You are in the bustling town square.",
		Exits:       make(map[Direction]*Room),
		Items: []*Item{
			{Name: "market flyer"},
		},
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
			p.Inventory = nil
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

	b.WriteString("\nItems:\n")
	if len(room.Items) == 0 {
		b.WriteString("  none\n")
	} else {
		for _, item := range room.Items {
			b.WriteString("  ")
			b.WriteString(item.Name)
			b.WriteString("\n")
		}
	}

	b.WriteString("\nPlayers:\n")
	hasPlayers := false
	for other := range w.Players {
		if other.Room == room {
			b.WriteString(" - ")
			b.WriteString(other.Name)
			b.WriteString("\n")
			for _, item := range other.Inventory {
				b.WriteString("     ")
				b.WriteString(item.Name)
				b.WriteString("\n")
			}
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

	if remaining := actionCooldown - time.Since(cmd.Player.LastAction); remaining > 0 {
		sendCooldown(cmd.Player, remaining)
		cmd.Player.Send <- fmt.Sprintf("You need to wait %.1f seconds.", remaining.Seconds())
		return
	}

	cmd.Player.LastAction = time.Now()
	sendCooldown(cmd.Player, actionCooldown)

	parts := strings.SplitN(line, " ", 2)

	switch strings.ToLower(parts[0]) {

	case "help":
		cmd.Player.Send <- `
Commands:
help
look
inv
take <item>
drop <item>
say <message>
north, east, south, west, up, down
`

	case "look":
		cmd.Player.Send <- w.describeRoom(cmd.Player)

	case "inv":
		cmd.Player.Send <- w.describeInventory(cmd.Player)

	case "take":
		if len(parts) < 2 {
			cmd.Player.Send <- "Usage: take <item>"
			return
		}
		w.takeItem(cmd.Player, parts[1])

	case "drop":
		if len(parts) < 2 {
			cmd.Player.Send <- "Usage: drop <item>"
			return
		}
		w.dropItem(cmd.Player, parts[1])

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

func (w *World) describeInventory(p *Player) string {
	if len(p.Inventory) == 0 {
		return "You aren't carrying anything."
	}

	var b strings.Builder
	b.WriteString("You are carrying:\n")
	for _, item := range p.Inventory {
		b.WriteString("  ")
		b.WriteString(item.Name)
		b.WriteString("\n")
	}
	return b.String()
}

func (w *World) takeItem(p *Player, query string) {
	item, idx, err := findItem(p.Room.Items, query)
	if err == errItemNotFound {
		p.Send <- "You don't see that here."
		return
	}
	if err == errItemAmbiguous {
		p.Send <- "Which one?"
		return
	}

	p.Room.Items = removeItemAt(p.Room.Items, idx)
	p.Inventory = append(p.Inventory, item)
	p.Send <- "You take the " + item.Name + "."
	w.broadcastToRoom(p.Room, "*** "+p.Name+" takes the "+item.Name+".", p)
}

func (w *World) dropItem(p *Player, query string) {
	item, idx, err := findItem(p.Inventory, query)
	if err == errItemNotFound {
		p.Send <- "You aren't carrying that."
		return
	}
	if err == errItemAmbiguous {
		p.Send <- "Which one?"
		return
	}

	p.Inventory = removeItemAt(p.Inventory, idx)
	p.Room.Items = append(p.Room.Items, item)
	p.Send <- "You drop the " + item.Name + "."
	w.broadcastToRoom(p.Room, "*** "+p.Name+" drops the "+item.Name+".", p)
}

var (
	errItemNotFound  = errors.New("item not found")
	errItemAmbiguous = errors.New("item ambiguous")
)

func findItem(items []*Item, query string) (*Item, int, error) {
	query = strings.ToLower(strings.TrimSpace(query))

	var match *Item
	var matchIdx int
	matches := 0

	for i, item := range items {
		name := strings.ToLower(item.Name)
		if name == query || strings.Contains(name, query) {
			matches++
			match = item
			matchIdx = i
		}
	}

	switch matches {
	case 0:
		return nil, -1, errItemNotFound
	case 1:
		return match, matchIdx, nil
	default:
		return nil, -1, errItemAmbiguous
	}
}

func removeItemAt(items []*Item, i int) []*Item {
	return append(items[:i], items[i+1:]...)
}

func sendCooldown(p *Player, d time.Duration) {
	ms := d.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	p.Send <- fmt.Sprintf("@@cooldown:%d", ms)
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
