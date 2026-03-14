package stt

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

type WhisperRequest struct {
	FilePath string `json:"filePath"`
	Base64   string `json:"base64"`
}

type WhisperResponse struct {
	Recognition string `json:"recognition"`
}

func Recognize(host, audioPath string) (string, error) {
	fmt.Printf("Sending audio to STT: %s\n", audioPath)
	audioData, err := ioutil.ReadFile(audioPath)
	if err != nil {
		return "", err
	}

	reqBody := WhisperRequest{
		FilePath: audioPath,
		Base64:   base64.StdEncoding.EncodeToString(audioData),
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	resp, err := http.Post(host+"/recognize", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	fmt.Printf("STT Raw Response: %s\n", string(body))

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("STT server returned status %d: %s", resp.StatusCode, string(body))
	}

	var wResp WhisperResponse
	if err := json.Unmarshal(body, &wResp); err != nil {
		fmt.Printf("STT JSON error: %v, body: %s\n", err, string(body))
		return "", err
	}

	return wResp.Recognition, nil
}
