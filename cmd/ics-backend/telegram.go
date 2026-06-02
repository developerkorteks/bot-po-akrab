package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"
)

type Telegram struct {
	token  string
	chatID string
	http   *http.Client
}

func NewTelegram(token, chatID string) *Telegram {
	if token == "" || chatID == "" {
		return nil
	}
	return &Telegram{
		token:  token,
		chatID: chatID,
		http:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *Telegram) Send(text string) {
	if t == nil {
		return
	}
	u := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage?chat_id=%s&text=%s&parse_mode=Markdown",
		t.token, t.chatID, url.QueryEscape(text))
	resp, err := t.http.Get(u)
	if err != nil {
		log.Printf("[TELEGRAM] send error: %v", err)
		return
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body)
}
