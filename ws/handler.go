package ws

import (
	"encoding/json"
	"net/http"

	"github.com/Bharanidharan2006/rce_sandbox_server/sandbox"
	"github.com/gorilla/websocket"
)

type SocketMessage struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

type CodePayload struct {
	Language string `json:"lang"`
	Code     string `json:"code"`
}

type InputPayload struct {
	Text string `json:"text"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func HandleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)

	if err != nil {
		return
	}

	defer conn.Close()

	inputChan := make(chan string, 10)
	defer close(inputChan)
	for {
		_, rawBytes, err := conn.ReadMessage()

		if err != nil {
			break // Cause the client disconnected
		}

		var socketMessage SocketMessage

		if err := json.Unmarshal(rawBytes, &socketMessage); err != nil {
			continue
		}

		switch socketMessage.Event {
		case "execute":
			var payload CodePayload

			json.Unmarshal(socketMessage.Data, &payload)

			go sandbox.RunCode(conn, []byte(payload.Code), inputChan)
		case "input":
			var payload InputPayload
			json.Unmarshal(socketMessage.Data, &payload)

			select {
			case inputChan <- payload.Text:
			default:
			}
		}
	}
}
