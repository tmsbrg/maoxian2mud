package main

import (
	"errors"
	"fmt"
	"log"
	"math/rand"
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
	actionCooldown   = 5 * time.Second
	mobAttackPeriod  = 5 * time.Second
	playerMaxHP      = 20
	playerRegenPeriod = 1 * time.Minute
	unarmedMinDamage = 1
	unarmedMaxDamage = 3

	bloodSpatterLifetime = 1 * time.Minute
	bloodPoolToSpatter   = 1 * time.Minute
	ratCorpseToSkeleton  = 1 * time.Minute
	ratSkeletonLifetime  = 5 * time.Minute

	maxRats         = 10
	ratSpawnBaseSec = 30
	ratRoamMinSec   = 5
	ratRoamMaxSec   = 15
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
	ItemGeneric      ItemKind = ""
	ItemWeapon       ItemKind = "weapon"
	ItemWeaponRack   ItemKind = "weapon_rack"
	ItemBloodSpatter ItemKind = "blood_spatter"
	ItemBloodPool    ItemKind = "blood_pool"
	ItemRatCorpse    ItemKind = "rat_corpse"
	ItemRatSkeleton  ItemKind = "rat_skeleton"
)

type Item struct {
	Name        string
	Description string
	Kind        ItemKind
	Fixed       bool
	MinDamage   int
	MaxDamage   int
	CreatedAt   time.Time
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
	HP            int
	MaxHP         int
	LastRegenAt   time.Time
	TookRackSword bool
	LastAction    time.Time
	Send          chan string
}

type Mob struct {
	Name          string
	Room          *Room
	HP            int
	MaxHP         int
	Alive         bool
	AttackMin     int
	AttackMax     int
	LastDirection Direction
	NextRoamAt    time.Time
}

type World struct {
	Join           chan *Player
	Leave          chan *Player
	Command        chan Command
	Players        map[*Player]bool
	Mobs           []*Mob
	Rooms          []*Room
	RatsNest       *Room
	NextRatSpawnAt time.Time
	StartRoom      *Room
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
		Description: "You are in the bustling town square. An open sewer grating in the pavement leads down into darkness.",
		Exits:       make(map[Direction]*Room),
		Items: []*Item{
			{
				Name:        "market flyer",
				Description: "A flyer advertising market stalls in the town square. Fresh goods arrive each morning.",
			},
		},
	}

	sewerIntersection := &Room{
		Name:        "Sewer Intersection",
		Description: "Stagnant water covers this four-way sewer junction. Rusted pipes and brick arches meet beneath the town above. Passages stretch off in every direction, and a ladder leads up to a grating of daylight.",
		Exits:       make(map[Direction]*Room),
	}
	northSewerTunnel := &Room{
		Name:        "North Sewer Tunnel",
		Description: "A narrow brick tunnel runs north through the sewers. Slimy moss coats the walls and the air smells of rust and rot.",
		Exits:       make(map[Direction]*Room),
	}
	undergroundTemple := &Room{
		Name:        "Underground Temple",
		Description: "Carved pillars rise from flooded flagstones in this hidden shrine. Faded murals depict robed figures beneath a ceiling lost in shadow.",
		Exits:       make(map[Direction]*Room),
	}
	eastSewerTunnel := &Room{
		Name:        "East Sewer Tunnel",
		Description: "This east-west sewer tunnel is wider than the others. Chains hang from the ceiling and shallow water ripples around your feet.",
		Exits:       make(map[Direction]*Room),
	}
	treasureRoom := &Room{
		Name:        "Treasure Room",
		Description: "A sealed chamber opens off the sewers. Cracked urns and an overturned chest lie scattered across the floor, long since emptied by looters.",
		Exits:       make(map[Direction]*Room),
	}
	southSewerTunnel := &Room{
		Name:        "South Sewer Tunnel",
		Description: "The tunnel slopes gently south. You hear skittering echoes somewhere ahead and smell something foul on the damp air.",
		Exits:       make(map[Direction]*Room),
	}
	ratsNest := &Room{
		Name:        "Rat's Nest",
		Description: "Piles of refuse and gnawed bones fill this widened chamber. Scratch marks cover the walls and the stench of vermin is overwhelming.",
		Exits:       make(map[Direction]*Room),
	}
	westSewerTunnel := &Room{
		Name:        "West Sewer Tunnel",
		Description: "The western tunnel is cramped and uneven. Roots poke through crumbled mortar and water drips steadily from above.",
		Exits:       make(map[Direction]*Room),
	}
	dampSewerTunnel := &Room{
		Name:        "Damp Sewer Tunnel",
		Description: "The passage turns and narrows. Flowing water has worn smooth channels in the stone floor and the brickwork gives way to bare rock.",
		Exits:       make(map[Direction]*Room),
	}
	naturalCave := &Room{
		Name:        "Natural Cave",
		Description: "The sewer's brick arches end here in a natural cavern. Stalactites hang over a dark pool and faint drafts stir the cold air.",
		Exits:       make(map[Direction]*Room),
	}
	ancientSecretPassage := &Room{
		Name:        "Ancient Secret Passage",
		Description: "Ancient stonework, older than the sewers around it, forms a hidden corridor. Dust lies undisturbed and carved symbols flank the walls.",
		Exits:       make(map[Direction]*Room),
	}
	crackedSewerPipe := &Room{
		Name:        "Cracked Sewer Pipe",
		Description: "A cramped stretch of pipe connects distant parts of the sewer. Water seeps through cracked masonry and the ceiling is low enough to touch.",
		Exits:       make(map[Direction]*Room),
	}
	lowCavern := &Room{
		Name:        "Low Cavern",
		Description: "The ceiling drops low over uneven stone. Pools of black water reflect your movement and the sound of dripping echoes all around.",
		Exits:       make(map[Direction]*Room),
	}
	echoingCavern := &Room{
		Name:        "Echoing Cavern",
		Description: "Every footstep rings through this tall cavern. Stalactites loom overhead and narrow gaps in the rock hint at passages beyond.",
		Exits:       make(map[Direction]*Room),
	}
	rootCrackedCavern := &Room{
		Name:        "Root-Cracked Cavern",
		Description: "Tree roots burst through the stone here, bridging the natural caves and the foul chambers nearby. The air grows warmer and smells of rot.",
		Exits:       make(map[Direction]*Room),
	}

	palace.Exits[South] = square
	square.Exits[North] = palace

	linkRooms(square, Down, sewerIntersection)
	linkRooms(sewerIntersection, North, northSewerTunnel)
	linkRooms(northSewerTunnel, North, undergroundTemple)
	linkRooms(sewerIntersection, East, eastSewerTunnel)
	linkRooms(eastSewerTunnel, East, treasureRoom)
	linkRooms(sewerIntersection, South, southSewerTunnel)
	linkRooms(southSewerTunnel, South, ratsNest)
	linkRooms(sewerIntersection, West, westSewerTunnel)
	linkRooms(westSewerTunnel, West, dampSewerTunnel)
	linkRooms(dampSewerTunnel, West, naturalCave)

	linkRooms(undergroundTemple, East, ancientSecretPassage)
	linkRooms(treasureRoom, North, ancientSecretPassage)
	linkRooms(treasureRoom, South, crackedSewerPipe)
	linkRooms(ratsNest, East, crackedSewerPipe)
	linkRooms(naturalCave, South, lowCavern)
	linkRooms(lowCavern, South, echoingCavern)
	linkRooms(echoingCavern, East, rootCrackedCavern)
	linkRooms(ratsNest, West, rootCrackedCavern)

	rat := newRat(ratsNest)

	world := &World{
		Join:      make(chan *Player),
		Leave:     make(chan *Player),
		Command:   make(chan Command),
		Players:   make(map[*Player]bool),
		Mobs:      []*Mob{rat},
		Rooms: []*Room{
			palace, square, sewerIntersection, northSewerTunnel, undergroundTemple,
			eastSewerTunnel, treasureRoom, southSewerTunnel, ratsNest, westSewerTunnel,
			dampSewerTunnel, naturalCave, ancientSecretPassage, crackedSewerPipe,
			lowCavern, echoingCavern, rootCrackedCavern,
		},
		RatsNest:  ratsNest,
		StartRoom: palace,
	}
	world.scheduleNextRatSpawn()
	return world
}

func linkRooms(from *Room, dir Direction, to *Room) {
	from.Exits[dir] = to
	to.Exits[opposite[dir]] = from
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
	mobTicker := time.NewTicker(mobAttackPeriod)
	defer mobTicker.Stop()

	for {
		select {
		case p := <-w.Join:
			w.Players[p] = true
			p.Room = w.StartRoom
			p.Inventory = nil
			p.RightHand = nil
			p.LeftHand = nil
			p.HP = playerMaxHP
			p.MaxHP = playerMaxHP
			p.LastRegenAt = time.Now()
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

		case <-mobTicker.C:
			w.mobCombatTick()
			w.mobRoamTick()
			w.ratSpawnTick()
			w.playerRegenTick()
			w.itemDecayTick()
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

	if creatures := w.creaturesInRoom(room); len(creatures) > 0 {
		b.WriteString("\nCreatures:\n")
		for _, mob := range creatures {
			b.WriteString("  ")
			b.WriteString(mob.Name)
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
			b.WriteString(", ")
			b.WriteString(woundDescription(other.HP, other.MaxHP))
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
attack <target> [left|right]
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

	case "attack":
		if len(parts) < 2 {
			cmd.Player.Send <- "Usage: attack <target> [left|right]"
			return
		}
		if w.attack(cmd.Player, parts[1]) {
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

	if mob, err := findMobInRoom(w, p.Room, query); err == errItemAmbiguous {
		p.Send <- "Which one?"
		return
	} else if err == nil {
		p.Send <- describeMob(mob)
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
	b.WriteString(" is here.\n")
	b.WriteString("Condition: ")
	b.WriteString(woundDescription(target.HP, target.MaxHP))
	b.WriteString("\n\nWielding:\n")
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

	b.WriteString("Health: ")
	b.WriteString(fmt.Sprintf("%d/%d HP", p.HP, p.MaxHP))
	b.WriteString("\n\nWielding:\n")
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
		MinDamage:   4,
		MaxDamage:   6,
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
	if item.Kind == ItemBloodSpatter || item.Kind == ItemBloodPool {
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

	var indices []int
	for i, item := range items {
		name := strings.ToLower(item.Name)
		if name == query || strings.Contains(name, query) {
			indices = append(indices, i)
		}
	}

	switch len(indices) {
	case 0:
		return nil, -1, errItemNotFound
	case 1:
		i := indices[0]
		return items[i], i, nil
	default:
		firstName := strings.ToLower(items[indices[0]].Name)
		for _, i := range indices[1:] {
			if strings.ToLower(items[i].Name) != firstName {
				return nil, -1, errItemAmbiguous
			}
		}
		i := indices[0]
		return items[i], i, nil
	}
}

func removeItemAt(items []*Item, i int) []*Item {
	return append(items[:i], items[i+1:]...)
}

func woundDescription(hp, maxHP int) string {
	if hp <= 0 {
		return "dead"
	}
	pct := float64(hp) / float64(maxHP)
	switch {
	case pct > 0.85:
		return "healthy"
	case pct > 0.65:
		return "lightly wounded"
	case pct > 0.40:
		return "wounded"
	case pct > 0.15:
		return "heavily wounded"
	default:
		return "near death"
	}
}

func rollDamage(min, max int) int {
	if max <= min {
		return min
	}
	return min + rand.Intn(max-min+1)
}

func handDamage(item *Item) (int, int) {
	if item != nil && item.Kind == ItemWeapon && item.MaxDamage > 0 {
		return item.MinDamage, item.MaxDamage
	}
	return unarmedMinDamage, unarmedMaxDamage
}

func (w *World) creaturesInRoom(room *Room) []*Mob {
	var mobs []*Mob
	for _, mob := range w.Mobs {
		if mob.Alive && mob.Room == room {
			mobs = append(mobs, mob)
		}
	}
	return mobs
}

func (w *World) playersInRoom(room *Room) []*Player {
	var players []*Player
	for p := range w.Players {
		if p.Room == room && p.HP > 0 {
			players = append(players, p)
		}
	}
	return players
}

func findMobInRoom(w *World, room *Room, query string) (*Mob, error) {
	query = strings.ToLower(strings.TrimSpace(query))

	var match *Mob
	matches := 0
	for _, mob := range w.Mobs {
		if !mob.Alive || mob.Room != room {
			continue
		}
		name := strings.ToLower(mob.Name)
		if name == query || strings.Contains(name, query) {
			matches++
			match = mob
		}
	}

	switch matches {
	case 0:
		return nil, errItemNotFound
	case 1:
		return match, nil
	default:
		return nil, errItemAmbiguous
	}
}

func describeMob(mob *Mob) string {
	return "A vicious sewer " + mob.Name + ". It looks " + woundDescription(mob.HP, mob.MaxHP) + "."
}

func parseAttackArgs(query string) (target string, hand Hand, ok bool) {
	args := strings.Fields(strings.TrimSpace(query))
	if len(args) == 0 {
		return "", HandRight, false
	}

	hand = HandRight
	if len(args) >= 2 {
		if h, parsed := parseHand(args[len(args)-1]); parsed {
			return strings.Join(args[:len(args)-1], " "), h, true
		}
	}
	return strings.Join(args, " "), HandRight, true
}

func (w *World) attack(p *Player, query string) bool {
	target, hand, ok := parseAttackArgs(query)
	if !ok || target == "" {
		p.Send <- "Usage: attack <target> [left|right]"
		return false
	}

	mob, err := findMobInRoom(w, p.Room, target)
	if err == errItemNotFound {
		p.Send <- "You don't see that here."
		return false
	}
	if err == errItemAmbiguous {
		p.Send <- "Which one?"
		return false
	}

	weapon := handItem(p, hand)
	minD, maxD := handDamage(weapon)
	damage := rollDamage(minD, maxD)
	mob.HP -= damage
	if mob.HP < 0 {
		mob.HP = 0
	}

	attackDesc := "bare fists"
	if weapon != nil {
		attackDesc = "your " + weapon.Name
	}

	p.Send <- fmt.Sprintf("You attack the %s with %s for %d damage!", mob.Name, attackDesc, damage)
	w.broadcastToRoom(p.Room, "*** "+p.Name+" attacks the "+mob.Name+" for "+fmt.Sprintf("%d", damage)+" damage!", p)
	p.Room.Items = append(p.Room.Items, newBloodSpatter())

	if mob.HP <= 0 {
		w.killMob(mob, p.Room)
		return true
	}

	p.Send <- "The " + mob.Name + " looks " + woundDescription(mob.HP, mob.MaxHP) + "."
	return true
}

func (w *World) killMob(mob *Mob, room *Room) {
	mob.Alive = false
	room.Items = append(room.Items, newRatCorpse(), newBloodPool())
	w.broadcastToRoom(room, "*** The "+mob.Name+" dies!", nil)
	w.scheduleNextRatSpawn()
}

func (w *World) mobCombatTick() {
	for _, mob := range w.Mobs {
		if !mob.Alive {
			continue
		}

		players := w.playersInRoom(mob.Room)
		if len(players) == 0 {
			continue
		}

		target := players[rand.Intn(len(players))]
		damage := rollDamage(mob.AttackMin, mob.AttackMax)
		target.HP -= damage
		if target.HP < 0 {
			target.HP = 0
		}

		target.Send <- fmt.Sprintf("The %s bites you for %d damage! (%d/%d HP)", mob.Name, damage, target.HP, target.MaxHP)
		w.broadcastToRoom(mob.Room, "The "+mob.Name+" bites "+target.Name+"!", target)
	}
}

func (w *World) mobRoamTick() {
	now := time.Now()
	for _, mob := range w.Mobs {
		if !mob.Alive {
			continue
		}
		if len(w.playersInRoom(mob.Room)) > 0 {
			continue
		}
		if mob.NextRoamAt.IsZero() {
			mob.NextRoamAt = now.Add(randomRoamDelay())
			continue
		}
		if now.Before(mob.NextRoamAt) {
			continue
		}

		w.moveMob(mob)
		mob.NextRoamAt = now.Add(randomRoamDelay())
	}
}

func (w *World) moveMob(mob *Mob) {
	var options []Direction
	for dir := range mob.Room.Exits {
		if dir == Up || dir == Down {
			continue
		}
		if mob.LastDirection != "" && dir == opposite[mob.LastDirection] {
			continue
		}
		options = append(options, dir)
	}
	if len(options) == 0 {
		return
	}

	dir := options[rand.Intn(len(options))]
	dest := mob.Room.Exits[dir]
	oldRoom := mob.Room

	w.announceMobLeave(oldRoom, mob, dir)
	mob.Room = dest
	mob.LastDirection = dir
	w.announceMobEnter(dest, mob, dir)
}

func (w *World) announceMobLeave(room *Room, mob *Mob, dir Direction) {
	w.broadcastToRoom(room, "A "+mob.Name+" scurries "+string(dir)+".", nil)
}

func (w *World) announceMobEnter(room *Room, mob *Mob, dir Direction) {
	w.broadcastToRoom(room, "A "+mob.Name+" scurries in from the "+string(opposite[dir])+".", nil)
}

func (w *World) ratSpawnTick() {
	if w.RatsNest == nil {
		return
	}
	if w.aliveRatCount() >= maxRats {
		return
	}
	if w.NextRatSpawnAt.IsZero() {
		w.scheduleNextRatSpawn()
		return
	}
	if time.Now().Before(w.NextRatSpawnAt) {
		return
	}

	w.Mobs = append(w.Mobs, newRat(w.RatsNest))
	w.broadcastToRoom(w.RatsNest, "A rat emerges from the refuse!", nil)
	w.scheduleNextRatSpawn()
}

func (w *World) aliveRatCount() int {
	count := 0
	for _, mob := range w.Mobs {
		if mob.Alive {
			count++
		}
	}
	return count
}

func (w *World) scheduleNextRatSpawn() {
	count := w.aliveRatCount()
	if count >= maxRats {
		w.NextRatSpawnAt = time.Time{}
		return
	}
	w.NextRatSpawnAt = time.Now().Add(ratSpawnDelay(count))
}

func ratSpawnDelay(activeRats int) time.Duration {
	base := time.Duration(ratSpawnBaseSec*(activeRats+1)) * time.Second
	jitter := time.Duration(rand.Intn(21)-10) * time.Second
	delay := base + jitter
	if delay < 10*time.Second {
		delay = 10 * time.Second
	}
	return delay
}

func randomRoamDelay() time.Duration {
	sec := ratRoamMinSec + rand.Intn(ratRoamMaxSec-ratRoamMinSec+1)
	return time.Duration(sec) * time.Second
}

func (w *World) playerRegenTick() {
	now := time.Now()
	for p := range w.Players {
		if p.HP <= 0 || p.HP >= p.MaxHP {
			continue
		}
		if p.LastRegenAt.IsZero() {
			p.LastRegenAt = now
			continue
		}
		if now.Sub(p.LastRegenAt) < playerRegenPeriod {
			continue
		}

		p.HP++
		p.LastRegenAt = now
		p.Send <- fmt.Sprintf("You feel a little better. (%d/%d HP)", p.HP, p.MaxHP)
	}
}

func newRat(room *Room) *Mob {
	return &Mob{
		Name:       "rat",
		Room:       room,
		HP:         10,
		MaxHP:      10,
		Alive:      true,
		AttackMin:  1,
		AttackMax:  3,
		NextRoamAt: time.Now().Add(randomRoamDelay()),
	}
}

func (w *World) itemDecayTick() {
	now := time.Now()
	for _, room := range w.Rooms {
		var kept []*Item
		for _, item := range room.Items {
			if item.CreatedAt.IsZero() {
				kept = append(kept, item)
				continue
			}

			age := now.Sub(item.CreatedAt)
			switch item.Kind {
			case ItemBloodSpatter:
				if age >= bloodSpatterLifetime {
					continue
				}
				kept = append(kept, item)
			case ItemBloodPool:
				if age >= bloodPoolToSpatter {
					kept = append(kept, newBloodSpatter())
					continue
				}
				kept = append(kept, item)
			case ItemRatCorpse:
				if age >= ratCorpseToSkeleton {
					kept = append(kept, newRatSkeleton())
					continue
				}
				kept = append(kept, item)
			case ItemRatSkeleton:
				if age >= ratSkeletonLifetime {
					continue
				}
				kept = append(kept, item)
			default:
				kept = append(kept, item)
			}
		}
		room.Items = kept
	}
}

func newBloodSpatter() *Item {
	return &Item{
		Name:        "blood spatter",
		Description: "Fresh blood marks the floor.",
		Kind:        ItemBloodSpatter,
		Fixed:       true,
		CreatedAt:   time.Now(),
	}
}

func newBloodPool() *Item {
	return &Item{
		Name:        "blood pool",
		Description: "Blood has pooled on the ground.",
		Kind:        ItemBloodPool,
		Fixed:       true,
		CreatedAt:   time.Now(),
	}
}

func newRatCorpse() *Item {
	return &Item{
		Name:        "rat corpse",
		Description: "The body of a large rat.",
		Kind:        ItemRatCorpse,
		CreatedAt:   time.Now(),
	}
}

func newRatSkeleton() *Item {
	return &Item{
		Name:        "rat skeleton",
		Description: "A rat skeleton, picked clean.",
		Kind:        ItemRatSkeleton,
		CreatedAt:   time.Now(),
	}
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
