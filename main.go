package main

import (
	"log"
	"net/http"
	"strings"

	"github.com/coder/websocket"
)

type Command struct {
	Player *Player
	Text   string
}

type Player struct {
	Name string
	Send chan string
}

type World struct {
	Join    chan *Player
	Leave   chan *Player
	Command chan Command

	Players map[*Player]bool
}

func NewWorld() *World {
	return &World{
		Join:    make(chan *Player),
		Leave:   make(chan *Player),
		Command: make(chan Command),
		Players: make(map[*Player]bool),
	}
}

func (w *World) Broadcast(msg string) {
	for p := range w.Players {
		select {
		case p.Send <- msg:
		default:
		}
	}
}

func (w *World) Run() {
	for {
		select {
		case p := <-w.Join:
			w.Players[p] = true
			p.Send <- "Welcome to MaoXianMUD!"
			p.Send <- "Type 'help'"
			w.Broadcast("*** " + p.Name + " entered the world")

		case p := <-w.Leave:
			delete(w.Players, p)
			close(p.Send)
			w.Broadcast("*** " + p.Name + " left the world")

		case cmd := <-w.Command:
			w.handleCommand(cmd)
		}
	}
}

func (w *World) handleCommand(cmd Command) {
	line := strings.TrimSpace(cmd.Text)

	if line == "" {
		return
	}

	parts := strings.SplitN(line, " ", 2)

	switch parts[0] {

	case "help":
		cmd.Player.Send <- `
Commands:
help
look
say <message>
`

	case "look":
		cmd.Player.Send <- `
You are standing in an empty room.

Exits:
  none

Players:
`
		for p := range w.Players {
			cmd.Player.Send <- " - " + p.Name
		}

	case "say":
		if len(parts) < 2 {
			cmd.Player.Send <- "Usage: say <message>"
			return
		}

		w.Broadcast(cmd.Player.Name + ": " + parts[1])

	default:
		cmd.Player.Send <- "Unknown command"
	}
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
			Name: "guest",
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
