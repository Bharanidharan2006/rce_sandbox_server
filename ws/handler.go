package ws

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/Bharanidharan2006/rce_sandbox_server/sandbox"
	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
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

type ChatOutput struct {
	Stream string          `json:"stream"`
	Reply  json.RawMessage `json:"reply"`
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

			go func() {
				if err := sandbox.RunCode(conn, []byte(payload.Code), inputChan); err != nil {
					// Send the error to the client so it shows up in the terminal
					errData, _ := json.Marshal(map[string]string{
						"stream": "stderr",
						"text":   fmt.Sprintf("sandbox error: %v\n", err),
					})
					conn.WriteJSON(SocketMessage{Event: "output", Data: errData})
					conn.WriteJSON(SocketMessage{Event: "finished"})
				}
			}()
		case "input":
			var payload InputPayload
			json.Unmarshal(socketMessage.Data, &payload)

			select {
			case inputChan <- payload.Text:
			default:
			}

		case "chat":
			go func() {
				sendResponse := func(streamType, replyText string) {
					innerData, _ := json.Marshal(map[string]string{
						"stream": streamType,
						"reply":  replyText,
					})

					conn.WriteJSON(SocketMessage{
						Event: "chat_output",
						Data:  innerData,
					})
				}
				if err := godotenv.Load(); err != nil {
					sendResponse("error", fmt.Sprintf("Cannot parse .env: %v", err))
				}
				apiKey := os.Getenv("GEMINI_API_KEY")
				url := "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent?key=" + apiKey
				resp, err := http.Post(url, "application/json", bytes.NewBuffer(socketMessage.Data))
				if err != nil {
					sendResponse("error", fmt.Sprintf("Failed to reach AI: %v", err))
					return
				}
				defer resp.Body.Close()

				body, _ := io.ReadAll(resp.Body)

				if resp.StatusCode != http.StatusOK {
					sendResponse("error", fmt.Sprintf("API Error (%d): %s", resp.StatusCode, string(body)))
					return
				}

				var geminiResp struct {
					Candidates []struct {
						Content struct {
							Parts []struct {
								Text string `json:"text"`
							} `json:"parts"`
						} `json:"content"`
					} `json:"candidates"`
				}

				if err := json.Unmarshal(body, &geminiResp); err != nil {
					sendResponse("error", "Failed to parse AI response.")
					return
				}

				replyText := "No response generated."
				if len(geminiResp.Candidates) > 0 && len(geminiResp.Candidates[0].Content.Parts) > 0 {
					replyText = geminiResp.Candidates[0].Content.Parts[0].Text
				}

				sendResponse("reply", replyText)
			}()
		}
	}
}
