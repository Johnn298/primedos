package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type tgUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type tgChat struct {
	ID int64 `json:"id"`
}

type tgMessage struct {
	MessageID int64   `json:"message_id"`
	From      *tgUser `json:"from,omitempty"`
	Chat      tgChat  `json:"chat"`
	Text      string  `json:"text,omitempty"`
}

type tgCallback struct {
	ID      string     `json:"id"`
	From    tgUser     `json:"from"`
	Data    string     `json:"data"`
	Message *tgMessage `json:"message,omitempty"`
}

type tgUpdate struct {
	UpdateID int64       `json:"update_id"`
	Message  *tgMessage  `json:"message,omitempty"`
	Callback *tgCallback `json:"callback_query,omitempty"`
}

type inlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

type inlineKeyboard struct {
	InlineKeyboard [][]inlineButton `json:"inline_keyboard"`
}

type api struct {
	token string
	base  string
	cl    *http.Client
}

func newAPI(token string) *api {
	return &api{
		token: token,
		base:  "https://api.telegram.org",
		cl:    &http.Client{Timeout: 60 * time.Second},
	}
}

func (a *api) call(method string, in any, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(http.MethodPost, a.base+"/bot"+a.token+"/"+method, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	var env struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("%s: ответ не разобран: %w", method, err)
	}
	if !env.OK {
		return fmt.Errorf("%s: %s", method, env.Description)
	}
	if out != nil && len(env.Result) > 0 {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

func (a *api) getUpdates(offset int64) ([]tgUpdate, error) {
	in := map[string]any{
		"offset":          offset,
		"timeout":         25,
		"allowed_updates": []string{"message", "callback_query"},
	}
	var out []tgUpdate
	err := a.call("getUpdates", in, &out)
	return out, err
}

func (a *api) sendMessage(chatID int64, text string, kb *inlineKeyboard) (int64, error) {
	in := map[string]any{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}
	if kb != nil {
		in["reply_markup"] = kb
	}
	var out tgMessage
	if err := a.call("sendMessage", in, &out); err != nil {
		return 0, err
	}
	return out.MessageID, nil
}

func (a *api) editMessage(chatID, msgID int64, text string, kb *inlineKeyboard) error {
	in := map[string]any{
		"chat_id":    chatID,
		"message_id": msgID,
		"text":       text,
		"parse_mode": "HTML",
	}
	if kb != nil {
		in["reply_markup"] = kb
	}
	err := a.call("editMessageText", in, nil)
	if err != nil && contains(err.Error(), "message is not modified") {
		return nil
	}
	return err
}

func (a *api) answerCallback(id, text string) error {
	in := map[string]any{"callback_query_id": id}
	if text != "" {
		in["text"] = text
	}
	return a.call("answerCallbackQuery", in, nil)
}

func contains(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func (a *api) getMe() (tgUser, error) {
	var u tgUser
	err := a.call("getMe", nil, &u)
	return u, err
}
