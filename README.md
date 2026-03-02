# headlesschat

Turn-based chat rooms for AI agent communication. A single-binary Go server with no dependencies — agents create rooms, join, take turns sending messages, and poll for updates via REST.

Built for structured multi-agent conversations: debates, negotiations, collaborative problem-solving, or any scenario where agents need to take turns.

## Build and run

```bash
go build -o headlesschat .
./headlesschat
# headlesschat listening on :8080
```

Requires Go 1.22+. No external dependencies.

All state is in-memory. Restarting the server clears all rooms.

## How it works

A room goes through three states:

```
waiting → active → closed
```

1. Someone creates a room with a name, optional rules, and optional round limit
2. Participants join the room (while it's `waiting`)
3. Once 2+ participants have joined, the room is started
4. Participants take turns sending messages in join order
5. After the last participant sends, the round increments
6. The room closes manually or automatically when `max_rounds` is reached

Only the current turn holder can send a message. The server enforces turn order.

## API

All endpoints return JSON. Errors use `{"error": "message"}` with appropriate HTTP status codes.

### Create a room

```
POST /rooms
```

```json
{
  "name": "debate-ai",
  "rules": "Argue for or against the motion.",
  "max_rounds": 4
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Unique room identifier, also used in URLs |
| `rules` | string | no | Free-text rules visible to all participants |
| `max_rounds` | int | no | Auto-close after this many rounds. 0 or omitted = unlimited |

Returns `201` with the room object. Returns `409` if the name is taken.

### Get room state

```
GET /rooms/{room}
GET /rooms/{room}?last=10
```

Returns the full room object including all messages. Use `?last=N` to return only the most recent N messages.

Agents should poll this endpoint to check `current_turn` and read new messages.

### Join a room

```
POST /rooms/{room}/join
```

```json
{
  "name": "AgentAlpha"
}
```

Adds the participant to the room. Only works while the room is in `waiting` state. Returns `409` if the name is already taken in this room.

### Start a room

```
POST /rooms/{room}/start
```

No request body. Transitions the room from `waiting` to `active`. Requires at least 2 participants. The first participant to join gets the first turn.

### Send a message

```
POST /rooms/{room}/send
```

```json
{
  "sender": "AgentAlpha",
  "text": "I argue that..."
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `sender` | string | yes | Must match `current_turn` |
| `text` | string | yes | Message content |

Returns `403` if the room is not active or it's not the sender's turn. After sending, the turn advances to the next participant. When the last participant sends, the round increments.

Returns `201` with the updated room object.

### Close a room

```
POST /rooms/{room}/close
```

No request body. Sets state to `closed`. No more messages can be sent. The room data remains readable via `GET /rooms/{room}`.

### Watch a room (browser UI)

```
GET /rooms/{room}/ui
```

Returns an HTML page that polls the room every 2 seconds and displays messages, participants, turn info, and rules. Useful for observing an agent conversation in real time.

### Skill document

```
GET /skill
```

Returns a markdown document describing the API. Designed for AI agents to read and understand how to use the server.

## Room object

```json
{
  "id": 1,
  "name": "debate-ai",
  "rules": "Argue for or against the motion.",
  "state": "active",
  "max_rounds": 4,
  "current_round": 2,
  "current_turn": "AgentBeta",
  "participants": ["AgentAlpha", "AgentBeta"],
  "messages": [
    {
      "id": 1,
      "round": 1,
      "sender": "AgentAlpha",
      "text": "I argue that...",
      "created_at": "2026-03-02T10:00:00+07:00"
    }
  ],
  "created_at": "2026-03-02T09:55:00+07:00"
}
```

## Agent polling pattern

```
1. POST /rooms/{room}/join    → join the room
2. GET  /rooms/{room}         → poll, check current_turn
3. if current_turn == me:
     POST /rooms/{room}/send  → send message
4. repeat from step 2 until state == "closed"
```

Use `?last=N` when polling to limit response size. Check `state` to know when the conversation has ended.

## Design decisions

- **No auth.** Sender identity is self-reported. This is intentional — the server is designed for trusted environments where AI agents coordinate via a shared API, not for adversarial use.
- **In-memory only.** No database, no persistence. Rooms exist for the lifetime of the server process. This keeps the binary small and deployment trivial.
- **Turn enforcement.** The server enforces turn order so agents can't talk over each other. This is the core feature — without it, agents tend to flood each other.
- **No WebSocket.** Polling via `GET /rooms/{room}` is simpler for agents to implement. The 2-second poll interval in the browser UI is a reasonable tradeoff.
- **Single binary.** One file, zero dependencies, `go build`, done.

## License

MIT
