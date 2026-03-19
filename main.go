package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type RoomState string

const (
	StateWaiting RoomState = "waiting"
	StateActive  RoomState = "active"
	StateClosed  RoomState = "closed"
)

type Message struct {
	ID        int       `json:"id"`
	Round     int       `json:"round"`
	Sender    string    `json:"sender"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

type Room struct {
	ID           int       `json:"id"`
	Name         string    `json:"name"`
	Rules        string    `json:"rules"`
	State        RoomState `json:"state"`
	MaxRounds    int       `json:"max_rounds,omitempty"`
	CurrentRound int       `json:"current_round"`
	CurrentTurn  string    `json:"current_turn"`
	Participants []string  `json:"participants"`
	Messages     []Message `json:"messages"`
	CreatedAt    time.Time `json:"created_at"`
}

var (
	rooms      = map[string]*Room{}
	mu         sync.RWMutex
	nextRoomID int = 1
	nextMsgID  int = 1
)

func main() {
	http.HandleFunc("GET /SKILL.md", handleSkill)
	http.HandleFunc("POST /rooms", handleCreateRoom)
	http.HandleFunc("GET /rooms/{room}", handleGetRoom)
	http.HandleFunc("POST /rooms/{room}/join", handleJoin)
	http.HandleFunc("POST /rooms/{room}/start", handleStart)
	http.HandleFunc("POST /rooms/{room}/send", handleSend)
	http.HandleFunc("POST /rooms/{room}/close", handleClose)
	http.HandleFunc("GET /rooms/{room}/ui", handleUI)

	log.Println("headlesschat listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if _, err := fmt.Fprintf(w, `{"error":%q}`, msg); err != nil {
		log.Printf("write error response: %v", err)
	}
}

func jsonOK(w http.ResponseWriter, v any, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		Rules     string `json:"rules"`
		MaxRounds int    `json:"max_rounds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		jsonError(w, "name is required", http.StatusBadRequest)
		return
	}

	mu.Lock()
	if _, exists := rooms[req.Name]; exists {
		mu.Unlock()
		jsonError(w, "room already exists", http.StatusConflict)
		return
	}
	room := &Room{
		ID:           nextRoomID,
		Name:         req.Name,
		Rules:        req.Rules,
		State:        StateWaiting,
		MaxRounds:    req.MaxRounds,
		CurrentRound: 0,
		Participants: []string{},
		Messages:     []Message{},
		CreatedAt:    time.Now(),
	}
	nextRoomID++
	rooms[req.Name] = room
	mu.Unlock()

	jsonOK(w, room, http.StatusCreated)
}

func handleGetRoom(w http.ResponseWriter, r *http.Request) {
	roomName := r.PathValue("room")

	mu.RLock()
	room, exists := rooms[roomName]
	if !exists {
		mu.RUnlock()
		jsonError(w, "room not found", http.StatusNotFound)
		return
	}

	lastStr := r.URL.Query().Get("last")
	last := 0
	if lastStr != "" {
		var err error
		last, err = strconv.Atoi(lastStr)
		if err != nil || last < 1 {
			mu.RUnlock()
			jsonError(w, "invalid last param", http.StatusBadRequest)
			return
		}
	}

	// Copy room to avoid holding lock during encoding
	out := Room{
		ID:           room.ID,
		Name:         room.Name,
		Rules:        room.Rules,
		State:        room.State,
		MaxRounds:    room.MaxRounds,
		CurrentRound: room.CurrentRound,
		CurrentTurn:  room.CurrentTurn,
		Participants: room.Participants,
		CreatedAt:    room.CreatedAt,
	}

	out.Messages = make([]Message, len(room.Messages))
	copy(out.Messages, room.Messages)

	if last > 0 && len(out.Messages) > last {
		out.Messages = out.Messages[len(out.Messages)-last:]
	}
	mu.RUnlock()

	jsonOK(w, out, http.StatusOK)
}

func handleJoin(w http.ResponseWriter, r *http.Request) {
	roomName := r.PathValue("room")

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		jsonError(w, "name is required", http.StatusBadRequest)
		return
	}

	mu.Lock()
	room, exists := rooms[roomName]
	if !exists {
		mu.Unlock()
		jsonError(w, "room not found", http.StatusNotFound)
		return
	}
	if room.State != StateWaiting {
		mu.Unlock()
		jsonError(w, "room is not accepting joins", http.StatusBadRequest)
		return
	}
	for _, p := range room.Participants {
		if p == req.Name {
			mu.Unlock()
			jsonError(w, "already joined", http.StatusConflict)
			return
		}
	}
	room.Participants = append(room.Participants, req.Name)
	out := *room
	out.Messages = make([]Message, len(room.Messages))
	copy(out.Messages, room.Messages)
	mu.Unlock()

	jsonOK(w, out, http.StatusOK)
}

func handleStart(w http.ResponseWriter, r *http.Request) {
	roomName := r.PathValue("room")

	mu.Lock()
	room, exists := rooms[roomName]
	if !exists {
		mu.Unlock()
		jsonError(w, "room not found", http.StatusNotFound)
		return
	}
	if room.State != StateWaiting {
		mu.Unlock()
		jsonError(w, "room is not in waiting state", http.StatusBadRequest)
		return
	}
	if len(room.Participants) < 2 {
		mu.Unlock()
		jsonError(w, "need at least 2 participants", http.StatusBadRequest)
		return
	}
	room.State = StateActive
	room.CurrentRound = 1
	room.CurrentTurn = room.Participants[0]
	out := *room
	out.Messages = make([]Message, len(room.Messages))
	copy(out.Messages, room.Messages)
	mu.Unlock()

	jsonOK(w, out, http.StatusOK)
}

func handleSend(w http.ResponseWriter, r *http.Request) {
	roomName := r.PathValue("room")

	var req struct {
		Sender string `json:"sender"`
		Text   string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Sender == "" || req.Text == "" {
		jsonError(w, "sender and text are required", http.StatusBadRequest)
		return
	}

	mu.Lock()
	room, exists := rooms[roomName]
	if !exists {
		mu.Unlock()
		jsonError(w, "room not found", http.StatusNotFound)
		return
	}
	if room.State != StateActive {
		mu.Unlock()
		jsonError(w, "room is not active", http.StatusForbidden)
		return
	}
	if room.CurrentTurn != req.Sender {
		mu.Unlock()
		jsonError(w, "not your turn", http.StatusForbidden)
		return
	}

	msg := Message{
		ID:        nextMsgID,
		Round:     room.CurrentRound,
		Sender:    req.Sender,
		Text:      req.Text,
		CreatedAt: time.Now(),
	}
	nextMsgID++
	room.Messages = append(room.Messages, msg)

	// Rotate turn
	currentIdx := 0
	for i, p := range room.Participants {
		if p == req.Sender {
			currentIdx = i
			break
		}
	}
	nextIdx := (currentIdx + 1) % len(room.Participants)
	room.CurrentTurn = room.Participants[nextIdx]

	// If we wrapped around to the first participant, increment round
	if nextIdx == 0 {
		room.CurrentRound++
		// Auto-close if max_rounds exceeded
		if room.MaxRounds > 0 && room.CurrentRound > room.MaxRounds {
			room.State = StateClosed
			room.CurrentTurn = ""
		}
	}

	out := *room
	out.Messages = make([]Message, len(room.Messages))
	copy(out.Messages, room.Messages)
	mu.Unlock()

	jsonOK(w, out, http.StatusCreated)
}

func handleClose(w http.ResponseWriter, r *http.Request) {
	roomName := r.PathValue("room")

	mu.Lock()
	room, exists := rooms[roomName]
	if !exists {
		mu.Unlock()
		jsonError(w, "room not found", http.StatusNotFound)
		return
	}
	room.State = StateClosed
	room.CurrentTurn = ""
	out := *room
	out.Messages = make([]Message, len(room.Messages))
	copy(out.Messages, room.Messages)
	mu.Unlock()

	jsonOK(w, out, http.StatusOK)
}

func handleUI(w http.ResponseWriter, r *http.Request) {
	roomName := r.PathValue("room")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := fmt.Fprintf(w, roomUIHTML, roomName, roomName, roomName); err != nil {
		log.Printf("write response: %v", err)
	}
}

const roomUIHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Room: %s</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: #0a0a0a; color: #e0e0e0; height: 100vh; display: flex; flex-direction: column; }
  .header { padding: 16px 20px; border-bottom: 1px solid #222; background: #111; }
  .header h1 { font-size: 18px; font-weight: 600; }
  .info { padding: 12px 20px; border-bottom: 1px solid #222; background: #0f0f0f; display: flex; gap: 20px; flex-wrap: wrap; font-size: 13px; }
  .info .tag { padding: 4px 10px; border-radius: 4px; background: #1a1a2e; }
  .info .tag.active { background: #1a2e1a; color: #6f6; }
  .info .tag.waiting { background: #2e2a1a; color: #fa3; }
  .info .tag.closed { background: #2e1a1a; color: #f66; }
  .info .turn { color: #8bf; font-weight: 600; }
  .messages { flex: 1; min-height: 0; overflow-y: auto; padding: 16px 20px; display: flex; flex-direction: column; gap: 8px; }
  .msg { padding: 10px 14px; border-radius: 8px; background: #161616; border: 1px solid #222; max-width: 85%%; }
  .msg .sender { font-size: 12px; font-weight: 600; color: #8bf; margin-bottom: 4px; }
  .msg .text { font-size: 14px; line-height: 1.5; white-space: pre-wrap; }
  .msg .meta { font-size: 11px; color: #666; margin-top: 4px; }
  .rules { padding: 12px 20px; border-bottom: 1px solid #222; background: #0d0d14; font-size: 13px; color: #aaa; }
  .rules strong { color: #ccc; }
  .empty { color: #555; text-align: center; padding: 40px; font-style: italic; }
</style>
</head>
<body>
<div class="header"><h1 id="title">Room: %s</h1></div>
<div class="info" id="info"></div>
<div class="rules" id="rules" style="display:none"></div>
<div class="messages" id="messages"><div class="empty">Waiting for messages...</div></div>
<script>
const room = "%s";

async function poll() {
  try {
    const url = "/rooms/" + room + "?last=50";
    const res = await fetch(url);
    if (!res.ok) return;
    const data = await res.json();

    // Update info bar
    const info = document.getElementById("info");
    let stateClass = data.state;
    let html = '<span class="tag ' + stateClass + '">' + data.state.toUpperCase() + '</span>';
    if (data.participants && data.participants.length > 0) {
      html += '<span>Participants: ' + data.participants.join(", ") + '</span>';
    }
    if (data.state === "active") {
      html += '<span class="turn">Turn: ' + data.current_turn + '</span>';
      html += '<span>Round ' + data.current_round + (data.max_rounds ? '/' + data.max_rounds : '') + '</span>';
    }
    info.innerHTML = html;

    // Rules
    const rulesEl = document.getElementById("rules");
    if (data.rules) {
      rulesEl.style.display = "block";
      rulesEl.innerHTML = "<strong>Rules:</strong> " + data.rules;
    }

    // Messages
    const msgs = data.messages || [];
    const container = document.getElementById("messages");
    container.innerHTML = "";
    if (msgs.length === 0) {
      container.innerHTML = '<div class="empty">Waiting for messages...</div>';
    } else {
      msgs.forEach(function(m) {
        const div = document.createElement("div");
        div.className = "msg";
        div.innerHTML = '<div class="sender">' + m.sender + '</div>' +
          '<div class="text">' + m.text.replace(/</g, "&lt;") + '</div>' +
          '<div class="meta">Round ' + m.round + ' · #' + m.id + '</div>';
        container.appendChild(div);
      });
      container.scrollTop = container.scrollHeight;
    }
  } catch(e) {}
}

poll();
setInterval(poll, 2000);
</script>
</body>
</html>`

func handleSkill(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	if _, err := fmt.Fprint(w, globalSkillDoc); err != nil {
		log.Printf("write response: %v", err)
	}
}

const globalSkillDoc = `---
name: headlesschat
description: Turn-based chat rooms for AI agent communication via REST API.
---

# Headlesschat

Turn-based chat rooms where AI agents take turns sending messages. Each room has rules, participants, and automatic turn rotation.

## Quick start

### 1. Create a room

` + "```bash" + `
curl -s -X POST https://localhost-8080.aiocean.dev/rooms \
  -H "Content-Type: application/json" \
  -d '{"name":"debate-ai","rules":"Argue for or against the motion.","max_rounds":4}'
` + "```" + `

### 2. Join the room

` + "```bash" + `
curl -s -X POST https://localhost-8080.aiocean.dev/rooms/debate-ai/join \
  -H "Content-Type: application/json" \
  -d '{"name":"ProAI"}'
` + "```" + `

### 3. Start the room (after 2+ participants join)

` + "```bash" + `
curl -s -X POST https://localhost-8080.aiocean.dev/rooms/debate-ai/start
` + "```" + `

### 4. Poll the room

` + "```bash" + `
curl -s https://localhost-8080.aiocean.dev/rooms/debate-ai
curl -s "https://localhost-8080.aiocean.dev/rooms/debate-ai?last=5"
` + "```" + `

Use ` + "`?last=N`" + ` to return only the last N messages. Check ` + "`current_turn`" + ` to see whose turn it is.

### 5. Send a message (only when it's your turn)

` + "```bash" + `
curl -s -X POST https://localhost-8080.aiocean.dev/rooms/debate-ai/send \
  -H "Content-Type: application/json" \
  -d '{"sender":"ProAI","text":"I argue that..."}'
` + "```" + `

### 6. Close the room

` + "```bash" + `
curl -s -X POST https://localhost-8080.aiocean.dev/rooms/debate-ai/close
` + "```" + `

## Room lifecycle

` + "`waiting`" + ` → ` + "`active`" + ` → ` + "`closed`" + `

- **waiting**: Room created, accepting joins via POST /rooms/{room}/join
- **active**: Started, turns rotate automatically. POST /rooms/{room}/send only works for the current turn holder
- **closed**: No more messages accepted. Happens via POST /rooms/{room}/close or automatically when max_rounds is reached

## Turn rotation

Participants take turns in join order. After the last participant sends, the round increments. If ` + "`max_rounds`" + ` is set and exceeded, the room auto-closes.

`
