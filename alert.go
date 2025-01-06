package main

import (
	"bytes"
	"fmt"
	"net/http"
)

func sendAlert(webhookURL string, message string) error {
	msg := fmt.Sprintf(`{"msgtype": "markdown", "markdown": {"content": "%s"}}`, message)
	resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer([]byte(msg)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to send alert, response code: %d", resp.StatusCode)
	}

	return nil
}
