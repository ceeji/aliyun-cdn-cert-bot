package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func sendAlert(webhookURL string, message string) error {
	msg, err := json.Marshal(map[string]any{"msgtype": "markdown", "markdown": map[string]string{"content": message}})
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(webhookURL, "application/json", bytes.NewBuffer(msg))
	if err != nil {
		return fmt.Errorf("notification request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to send alert, response code: %d", resp.StatusCode)
	}
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("invalid notification response")
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("notification rejected: code %d", result.ErrCode)
	}

	return nil
}
