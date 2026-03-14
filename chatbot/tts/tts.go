package tts

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

type TTSRequest struct {
	Sentence   string `json:"sentence"`
	Base64     bool   `json:"base64"`
	OutputPath string `json:"output_path"`
}

type TTSResponse struct {
	Success bool   `json:"success"`
	Base64  string `json:"base64,omitempty"`
	Error   string `json:"error,omitempty"`
}

func Synthesize(host, sentence, outputPath string) error {
	reqBody := TTSRequest{
		Sentence:   sentence,
		Base64:     true,
		OutputPath: outputPath,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	resp, err := http.Post(host+"/synthesize", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	fmt.Printf("TTS Raw Response: %s\n", string(body))

	var tResp TTSResponse
	if err := json.Unmarshal(body, &tResp); err != nil {
		return err
	}

	if !tResp.Success {
		return fmt.Errorf("TTS failed: %s", tResp.Error)
	}

	if tResp.Base64 != "" {
		data, err := base64.StdEncoding.DecodeString(tResp.Base64)
		if err != nil {
			return err
		}
		return ioutil.WriteFile(outputPath, data, 0644)
	}

	return nil
}

type PiperRequest struct {
	Text        string  `json:"text"`
	LengthScale float64 `json:"length_scale,omitempty"`
}

func SynthesizePiper(host, sentence, outputPath string) error {
	reqBody := PiperRequest{
		Text: sentence,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	resp, err := http.Post(host, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("piper TTS failed with status: %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(outputPath, body, 0644)
}
