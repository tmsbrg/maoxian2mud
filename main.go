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

type ItemKind string

const (
	ItemGeneric    ItemKind = ""
	ItemWeapon     ItemKind = "weapon"
	ItemWeaponRack ItemKind = "weapon_rack"
)

type Item struct {
	Name        string
	Description string
	Kind        ItemKind
	Fixed       bool
}

type Hand string

const (
	HandRight Hand = "right"
	HandLeft  Hand = "left"
)

type Player struct {
	Name          string
	Room          *Room
	Inventory     []*Item
	RightHand     *Item
	LeftHand      *Item
	TookRackSword bool
	LastAction    time.Time
	Send          chan string
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
		Description: "You are in a grand palace. A weapon rack stands against the wall.",
		Exits:       make(map[Direction]*Room),
		Items: []*Item{
			{
				Name:        "weapon rack",
				Description: "A sturdy rack lined with swords along the wall. You can take a sword with: take sword",
				Kind:        ItemWeaponRack,
				Fixed:       true,
			},
			{
				Name:        "golden scepter",
				Description: "A heavy ceremonial scepter, ornate and gilded.",
			},
		},
	}
	square := &Room{
		Name:        "Town Square",
		Description: "You are in the bustling town square.",
		Exits:       make(map[Direction]*Room),
		Items: []*Item{
			{
				Name:        "market flyer",
				Description: "A flyer advertising market stalls in the town square. Fresh goods arrive each morning.",
			},
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
			p.RightHand = nil
			p.LeftHand = nil
			p.TookRackSword = false
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
			if wielding := formatWielding(other); wielding != "" {
				b.WriteString(" (")
				b.WriteString(wielding)
				b.WriteString(")")
			}
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
	verb := strings.ToLower(parts[0])

	if !isInfoCommand(verb) {
		if remaining := actionCooldown - time.Since(cmd.Player.LastAction); remaining > 0 {
			sendCooldown(cmd.Player, remaining)
			cmd.Player.Send <- fmt.Sprintf("You need to wait %.1f seconds.", remaining.Seconds())
			return
		}
	}

	switch verb {

	case "help":
		cmd.Player.Send <- `
Commands:
help
look
inv
examine <target> (x)
take <item>
drop <item>
equip <item> [left|right]
unequip <item|left|right>
say <message>
north, east, south, west, up, down
`

	case "look":
		cmd.Player.Send <- w.describeRoom(cmd.Player)

	case "inv":
		cmd.Player.Send <- w.describeInventory(cmd.Player)

	case "examine", "x":
		if len(parts) < 2 {
			cmd.Player.Send <- "Usage: examine <target>"
			return
		}
		w.examine(cmd.Player, parts[1])

	case "take":
		if len(parts) < 2 {
			cmd.Player.Send <- "Usage: take <item>"
			return
		}
		if w.takeItem(cmd.Player, parts[1]) {
			applyActionCooldown(cmd.Player)
		}

	case "drop":
		if len(parts) < 2 {
			cmd.Player.Send <- "Usage: drop <item>"
			return
		}
		if w.dropItem(cmd.Player, parts[1]) {
			applyActionCooldown(cmd.Player)
		}

	case "equip":
		if len(parts) < 2 {
			cmd.Player.Send <- "Usage: equip <item> [left|right]"
			return
		}
		if w.equipItem(cmd.Player, parts[1]) {
			applyActionCooldown(cmd.Player)
		}

	case "unequip":
		if len(parts) < 2 {
			cmd.Player.Send <- "Usage: unequip <item|left|right>"
			return
		}
		if w.unequipItem(cmd.Player, parts[1]) {
			applyActionCooldown(cmd.Player)
		}

	case "say":
		if len(parts) < 2 {
			cmd.Player.Send <- "Usage: say <message>"
			return
		}

		w.broadcastToRoom(cmd.Player.Room, cmd.Player.Name+": "+parts[1], nil)
		applyActionCooldown(cmd.Player)

	default:
		if dir, ok := parseDirection(parts[0]); ok {
			w.movePlayer(cmd.Player, dir)
			applyActionCooldown(cmd.Player)
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

func isInfoCommand(word string) bool {
	switch word {
	case "help", "look", "inv", "examine", "x":
		return true
	default:
		return false
	}
}

func formatWielding(p *Player) string {
	var parts []string
	if p.RightHand != nil {
		parts = append(parts, p.RightHand.Name)
	}
	if p.LeftHand != nil {
		parts = append(parts, p.LeftHand.Name)
	}
	return strings.Join(parts, ", ")
}

func (w *World) examine(p *Player, query string) {
	query = strings.TrimSpace(query)
	if query == "" {
		p.Send <- "Usage: examine <target>"
		return
	}

	switch count := countPlayersInRoom(w, p.Room, query); {
	case count == 1:
		p.Send <- w.describeExaminedPlayer(findPlayerInRoom(w, p.Room, query))
		return
	case count > 1:
		p.Send <- "Which one?"
		return
	}

	item, _, err := findItem(p.Room.Items, query)
	if err == errItemAmbiguous {
		p.Send <- "Which one?"
		return
	}
	if err == nil {
		p.Send <- item.Description
		return
	}

	p.Send <- "You don't see that here."
}

func (w *World) describeExaminedPlayer(target *Player) string {
	var b strings.Builder

	b.WriteString(target.Name)
	b.WriteString(" is here.\n\nWielding:\n")
	writeHandLine(&b, target.RightHand, HandRight)
	writeHandLine(&b, target.LeftHand, HandLeft)

	if len(target.Inventory) == 0 {
		b.WriteString("\nCarrying nothing.")
		return b.String()
	}

	b.WriteString("\nCarrying:\n")
	for _, item := range target.Inventory {
		b.WriteString("  ")
		b.WriteString(item.Name)
		b.WriteString("\n")
	}
	return b.String()
}

func findPlayerInRoom(w *World, room *Room, query string) *Player {
	query = strings.ToLower(strings.TrimSpace(query))

	var match *Player
	for p := range w.Players {
		if p.Room != room {
			continue
		}
		name := strings.ToLower(p.Name)
		if name == query || strings.Contains(name, query) {
			match = p
		}
	}
	return match
}

func countPlayersInRoom(w *World, room *Room, query string) int {
	query = strings.ToLower(strings.TrimSpace(query))
	matches := 0

	for p := range w.Players {
		if p.Room != room {
			continue
		}
		name := strings.ToLower(p.Name)
		if name == query || strings.Contains(name, query) {
			matches++
		}
	}
	return matches
}

func (w *World) describeInventory(p *Player) string {
	var b strings.Builder

	b.WriteString("Wielding:\n")
	writeHandLine(&b, p.RightHand, HandRight)
	writeHandLine(&b, p.LeftHand, HandLeft)

	if len(p.Inventory) == 0 {
		b.WriteString("\nYou aren't carrying anything.")
		return b.String()
	}

	b.WriteString("\nYou are carrying:\n")
	for _, item := range p.Inventory {
		b.WriteString("  ")
		b.WriteString(item.Name)
		b.WriteString("\n")
	}
	return b.String()
}

func writeHandLine(b *strings.Builder, item *Item, hand Hand) {
	b.WriteString("  ")
	b.WriteString(string(hand))
	b.WriteString(" hand: ")
	if item == nil {
		b.WriteString("empty")
	} else {
		b.WriteString(item.Name)
	}
	b.WriteString("\n")
}

func newSword() *Item {
	return &Item{
		Name:        "sword",
		Description: "A sharp blade, well balanced for combat.",
		Kind:        ItemWeapon,
	}
}

func (w *World) takeItem(p *Player, query string) bool {
	lower := strings.ToLower(strings.TrimSpace(query))
	if lower == "sword from rack" || lower == "sword rack" {
		return w.takeSwordFromRack(p)
	}

	item, idx, err := findItem(p.Room.Items, query)
	if err == errItemNotFound {
		if isSwordQuery(lower) && roomHasWeaponRack(p.Room) && !roomHasSword(p.Room) {
			return w.takeSwordFromRack(p)
		}
		p.Send <- "You don't see that here."
		return false
	}
	if err == errItemAmbiguous {
		p.Send <- "Which one?"
		return false
	}
	if item.Fixed {
		p.Send <- "You can't take that."
		return false
	}

	p.Room.Items = removeItemAt(p.Room.Items, idx)
	w.giveTakenItem(p, item)
	w.broadcastToRoom(p.Room, "*** "+p.Name+" takes the "+item.Name+".", p)
	return true
}

func (w *World) takeSwordFromRack(p *Player) bool {
	if !roomHasWeaponRack(p.Room) {
		p.Send <- "There is no weapon rack here."
		return false
	}
	if p.TookRackSword {
		p.Send <- "You have already taken a sword from the rack."
		return false
	}

	p.TookRackSword = true
	sword := newSword()
	w.giveTakenItem(p, sword)
	w.broadcastToRoom(p.Room, "*** "+p.Name+" takes a sword from the rack.", p)
	return true
}

func (w *World) giveTakenItem(p *Player, item *Item) {
	if item.Kind == ItemWeapon && p.RightHand == nil {
		p.RightHand = item
		p.Send <- "You take the " + item.Name + " and equip it in your right hand."
		return
	}

	p.Inventory = append(p.Inventory, item)
	p.Send <- "You take the " + item.Name + "."
}

func (w *World) dropItem(p *Player, query string) bool {
	if item, hand, ok := findEquippedItem(p, query); ok {
		return w.dropFromHand(p, hand, item)
	}

	item, idx, err := findItem(p.Inventory, query)
	if err == errItemNotFound {
		p.Send <- "You aren't carrying that."
		return false
	}
	if err == errItemAmbiguous {
		p.Send <- "Which one?"
		return false
	}

	p.Inventory = removeItemAt(p.Inventory, idx)
	p.Room.Items = append(p.Room.Items, item)
	p.Send <- "You drop the " + item.Name + "."
	w.broadcastToRoom(p.Room, "*** "+p.Name+" drops the "+item.Name+".", p)
	return true
}

func (w *World) dropFromHand(p *Player, hand Hand, item *Item) bool {
	clearHand(p, hand)
	p.Room.Items = append(p.Room.Items, item)
	p.Send <- "You drop the " + item.Name + "."
	w.broadcastToRoom(p.Room, "*** "+p.Name+" drops the "+item.Name+".", p)
	return true
}

func (w *World) equipItem(p *Player, query string) bool {
	args := strings.Fields(strings.TrimSpace(query))
	if len(args) == 0 {
		p.Send <- "Usage: equip <item> [left|right]"
		return false
	}

	itemName := args[0]
	preferredHand := Hand("")
	if len(args) >= 2 {
		hand, ok := parseHand(args[1])
		if !ok {
			p.Send <- "Usage: equip <item> [left|right]"
			return false
		}
		preferredHand = hand
	}

	item, idx, err := findItem(p.Inventory, itemName)
	if err == errItemNotFound {
		p.Send <- "You aren't carrying that."
		return false
	}
	if err == errItemAmbiguous {
		p.Send <- "Which one?"
		return false
	}

	targetHand := preferredHand
	if targetHand == "" {
		switch {
		case p.RightHand == nil:
			targetHand = HandRight
		case p.LeftHand == nil:
			targetHand = HandLeft
		default:
			p.Send <- "Both hands are full."
			return false
		}
	}

	if handItem(p, targetHand) != nil {
		p.Send <- "Your " + string(targetHand) + " hand is already full."
		return false
	}

	p.Inventory = removeItemAt(p.Inventory, idx)
	setHand(p, targetHand, item)
	p.Send <- "You equip the " + item.Name + " in your " + string(targetHand) + " hand."
	w.broadcastToRoom(p.Room, "*** "+p.Name+" equips a "+item.Name+".", p)
	return true
}

func (w *World) unequipItem(p *Player, query string) bool {
	args := strings.Fields(strings.TrimSpace(strings.ToLower(query)))
	if len(args) == 0 {
		p.Send <- "Usage: unequip <item|left|right>"
		return false
	}

	if len(args) == 1 {
		if hand, ok := parseHand(args[0]); ok {
			return w.unequipHand(p, hand)
		}
		return w.unequipByItem(p, args[0])
	}

	item, hand, ok := findEquippedItem(p, strings.Join(args, " "))
	if !ok {
		if h, handOK := parseHand(args[len(args)-1]); handOK {
			itemName := strings.Join(args[:len(args)-1], " ")
			item, hand, ok = findEquippedItemInHand(p, itemName, h)
		}
	}
	if !ok {
		p.Send <- "You aren't wielding that."
		return false
	}

	return w.unequipHandItem(p, hand, item)
}

func (w *World) unequipByItem(p *Player, query string) bool {
	item, hand, ok := findEquippedItem(p, query)
	if !ok {
		p.Send <- "You aren't wielding that."
		return false
	}
	if other := handItem(p, otherHand(hand)); other != nil {
		if strings.Contains(strings.ToLower(other.Name), strings.ToLower(query)) ||
			strings.Contains(strings.ToLower(query), strings.ToLower(other.Name)) {
			p.Send <- "Which hand?"
			return false
		}
	}
	return w.unequipHandItem(p, hand, item)
}

func (w *World) unequipHand(p *Player, hand Hand) bool {
	item := handItem(p, hand)
	if item == nil {
		p.Send <- "Your " + string(hand) + " hand is empty."
		return false
	}
	return w.unequipHandItem(p, hand, item)
}

func (w *World) unequipHandItem(p *Player, hand Hand, item *Item) bool {
	clearHand(p, hand)
	p.Inventory = append(p.Inventory, item)
	p.Send <- "You unequip the " + item.Name + " from your " + string(hand) + " hand."
	w.broadcastToRoom(p.Room, "*** "+p.Name+" unequips a "+item.Name+".", p)
	return true
}

func handItem(p *Player, hand Hand) *Item {
	if hand == HandLeft {
		return p.LeftHand
	}
	return p.RightHand
}

func setHand(p *Player, hand Hand, item *Item) {
	if hand == HandLeft {
		p.LeftHand = item
		return
	}
	p.RightHand = item
}

func clearHand(p *Player, hand Hand) {
	setHand(p, hand, nil)
}

func otherHand(hand Hand) Hand {
	if hand == HandLeft {
		return HandRight
	}
	return HandLeft
}

func parseHand(word string) (Hand, bool) {
	switch strings.ToLower(word) {
	case "right", "r":
		return HandRight, true
	case "left", "l":
		return HandLeft, true
	default:
		return "", false
	}
}

func findEquippedItem(p *Player, query string) (*Item, Hand, bool) {
	if item, hand, ok := findEquippedItemInHand(p, query, HandRight); ok {
		return item, hand, true
	}
	return findEquippedItemInHand(p, query, HandLeft)
}

func findEquippedItemInHand(p *Player, query string, hand Hand) (*Item, Hand, bool) {
	item := handItem(p, hand)
	if item == nil {
		return nil, "", false
	}
	query = strings.ToLower(strings.TrimSpace(query))
	name := strings.ToLower(item.Name)
	if name == query || strings.Contains(name, query) {
		return item, hand, true
	}
	return nil, "", false
}

func isSwordQuery(query string) bool {
	return query == "sword" || strings.Contains(query, "sword")
}

func roomHasWeaponRack(room *Room) bool {
	for _, item := range room.Items {
		if item.Kind == ItemWeaponRack {
			return true
		}
	}
	return false
}

func roomHasSword(room *Room) bool {
	for _, item := range room.Items {
		if item.Kind == ItemWeapon {
			return true
		}
	}
	return false
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

func applyActionCooldown(p *Player) {
	p.LastAction = time.Now()
	sendCooldown(p, actionCooldown)
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
