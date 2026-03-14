package main

import (
	"agi/chatbot/audio"
	"agi/chatbot/display"
	"agi/chatbot/llm"
	"agi/chatbot/stt"
	"agi/chatbot/tts"
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// urlPattern matches any URL tag variants like <URL:link> or <URL link>.
var urlPattern = regexp.MustCompile(`(?i)<URL[:\s]?[^>]*>`)

// urlLabelPattern catches "URL:" followed by any non-whitespace (full URL or path).
// This ensures that "URL: /news/articles/..." is stripped entirely.
var urlLabelPattern = regexp.MustCompile(`(?i)\bURL\s*:\s*\S*`)

// bareURLPattern catches any remaining standalone http(s) links.
var bareURLPattern = regexp.MustCompile(`https?://\S+`)

// oddCharsPattern matches characters that are awkward or meaningless when
// read aloud (markdown formatting symbols, etc.).
var oddCharsPattern = regexp.MustCompile(`[*#_` + "`" + `~|]`)

// listItemPattern matches a numbered list item marker like "1." or " 2.".
var listItemPattern = regexp.MustCompile(`(?m)(^|\s)(\d+\.)\s+`)

// cleanForSpeech strips URL labels, URL tags, and markdown formatting characters
// that the TTS engine would read out literally or that sound odd.
func cleanForSpeech(text string) string {
	text = urlLabelPattern.ReplaceAllString(text, "")
	text = urlPattern.ReplaceAllString(text, "")
	text = bareURLPattern.ReplaceAllString(text, "")
	text = oddCharsPattern.ReplaceAllString(text, "")
	// Collapse multiple spaces left behind by removals.
	text = regexp.MustCompile(` {2,}`).ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

// cleanForDisplay strips URL labels, URL tags and markdown symbols, then ensures
// numbered list items ("1. ", "2. ", …) each start on their own line.
func cleanForDisplay(text string) string {
	text = urlLabelPattern.ReplaceAllString(text, "")
	text = urlPattern.ReplaceAllString(text, "")
	text = bareURLPattern.ReplaceAllString(text, "")
	text = oddCharsPattern.ReplaceAllString(text, "")
	// Insert newline before inline list item markers like " 2. ".
	text = listItemPattern.ReplaceAllString(text, "\n$1 ")
	// Collapse multiple spaces but preserve newlines.
	text = regexp.MustCompile(`[ \t]{2,}`).ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

type State int

const (
	StateIdle State = iota
	StateListening
	StateASR
	StateThinking
	StateAnswering
)

func main() {
	// 1. Load config from .env file if it exists
	env := loadEnv(".env")

	getAddr := func(key, defaultValue string) string {
		// 1. Check system env (exported by run.sh)
		if val := os.Getenv(key); val != "" {
			return val
		}
		// 2. Check loaded .env map
		if val, ok := env[key]; ok {
			return val
		}
		// 3. Final default
		return defaultValue
	}

	hdnAddr := getAddr("HDN_ADDR", "http://192.168.1.53:8083")
	whisperHost := getAddr("LLM8850_WHISPER_HOST", "http://localhost:8801")
	meloHost := getAddr("LLM8850_MELOTTS_HOST", "http://localhost:8802")
	piperHost := getAddr("PIPER_HTTP_HOST", "")
	piperPort := getAddr("PIPER_HTTP_PORT", "8805")
	visionURL := getAddr("VISION_URL", "http://localhost:8001/v1/chat/completions")
	chatModel := getAddr("CHAT_MODEL", "")
	switcherURL := getAddr("SWITCHER_URL", "http://localhost:9000")

	remoteCameraURL := getAddr("REMOTE_CAMERA_URL", "")

	fmt.Printf("[Config] HDN_ADDR: %s\n", hdnAddr)
	fmt.Printf("[Config] Whisper: %s\n", whisperHost)
	if remoteCameraURL != "" {
		fmt.Printf("[Config] Remote Camera: %s\n", remoteCameraURL)
	}
	if piperHost != "" {
		fmt.Printf("[Config] Piper TTS: %s:%s\n", piperHost, piperPort)
	} else {
		fmt.Printf("[Config] MeloTTS: %s\n", meloHost)
	}

	// 2. Start Display Client
	fmt.Println("Connecting to display...")
	disp, err := display.NewClient("localhost:12345")
	if err != nil {
		log.Fatalf("Failed to connect to display: %v", err)
	}
	defer disp.Close()

	ai := llm.NewClient(hdnAddr)
	if chatModel != "" {
		ai.Model = chatModel
	}

	state := StateIdle

	fmt.Println("Chatbot ready. Push button to talk.")

	disp.Display(display.Status{
		Status: "Idle",
		Emoji:  "😴",
		Text:   "Ready! Push button to talk.",
		RGB:    "#000055",
	})

	var recordCmd *exec.Cmd
	var audioPath string
	var audioMu sync.Mutex
	var playCmd *exec.Cmd
	var playMu sync.Mutex
	var buttonPressTime time.Time
	var lastButtonPressTime time.Time
	var lastButtonReleaseTime time.Time
	var lastDoublePressHandled time.Time
	var speakCancel bool
	var cancelMu sync.Mutex
	var currentSpeechID int
	var speechIDMu sync.Mutex
	var cameraIsActive bool
	var cameraMu sync.Mutex
	var isStabilizing bool
	var stabMu sync.Mutex
	var isProcessingVision bool
	var visionMu sync.Mutex

	// Local function declarations
	var stopPlayback func()
	var speakText func(string)
	var switchModelSync func(string, string)
	var switchModelAsync func(string)
	var toggleCameraMode func()

	// Function Definitions
	stopPlayback = func() {
		speechIDMu.Lock()
		currentSpeechID++
		speechIDMu.Unlock()

		cancelMu.Lock()
		speakCancel = true
		cancelMu.Unlock()

		playMu.Lock()
		cmd := playCmd
		playCmd = nil
		playMu.Unlock()

		if cmd != nil {
			fmt.Println("Stopping playback process...")
			audio.StopPlayback(cmd)
		} else {
			// Even if no cmd, kill lingering players
			_ = exec.Command("pkill", "-9", "sox").Run()
			_ = exec.Command("pkill", "-9", "play").Run()
			_ = exec.Command("pkill", "-9", "aplay").Run()
		}
	}

	speakText = func(sayText string) {
		speechIDMu.Lock()
		id := currentSpeechID
		speechIDMu.Unlock()

		audioMu.Lock()
		defer audioMu.Unlock()

		// If a new stop or speech happened while waiting for lock, bail
		speechIDMu.Lock()
		if id != currentSpeechID {
			speechIDMu.Unlock()
			return
		}
		speechIDMu.Unlock()

		cancelMu.Lock()
		speakCancel = false
		cancelMu.Unlock()

		ttsPath := filepath.Join(os.TempDir(), fmt.Sprintf("out_%d.wav", time.Now().UnixNano()))
		fmt.Printf("Synthesizing voice: %s\n", sayText)
		finalPath := ttsPath

		if piperHost != "" {
			piperURL := fmt.Sprintf("http://%s:%s", piperHost, piperPort)
			err := tts.SynthesizePiper(piperURL, sayText, ttsPath)
			if err != nil {
				fmt.Printf("Piper TTS error: %v\n", err)
			}
		} else {
			tts.Synthesize(meloHost, sayText, ttsPath)
			time.Sleep(300 * time.Millisecond)
			timestamp := strings.TrimPrefix(filepath.Base(ttsPath), "out_")
			expectedServerPath := filepath.Join(os.TempDir(), "melotts_output_"+timestamp)
			if _, err := os.Stat(ttsPath); os.IsNotExist(err) {
				if _, err := os.Stat(expectedServerPath); err == nil {
					finalPath = expectedServerPath
				} else {
					latest := audio.FindLatestWav()
					if latest != "" {
						finalPath = latest
					}
				}
			}
		}

		time.Sleep(1000 * time.Millisecond)

		cancelMu.Lock()
		isCanceled := speakCancel
		cancelMu.Unlock()
		if isCanceled {
			os.Remove(ttsPath)
			os.Remove(finalPath)
			return
		}

		cmd, err := audio.StartPlayback(finalPath)
		if err != nil {
			fmt.Printf("Playback error: %v\n", err)
		} else {
			playMu.Lock()
			playCmd = cmd
			playMu.Unlock()
			_ = cmd.Wait()
			playMu.Lock()
			if playCmd == cmd {
				playCmd = nil
			}
			playMu.Unlock()
		}
		os.Remove(ttsPath)
		os.Remove(finalPath)
	}

	switchModelSync = func(model string, targetURL string) {
		if model == "vision" {
			stabMu.Lock()
			isStabilizing = true
			stabMu.Unlock()
			defer func() {
				stabMu.Lock()
				isStabilizing = false
				stabMu.Unlock()
			}()
		}

		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
		defer cancel()

		err := llm.SwitchModel(ctx, switcherURL, model)
		if err != nil {
			fmt.Printf("[Switcher] Error switching to %s: %v\n", model, err)
			return
		}

		err = llm.WaitForHealth(ctx, targetURL)
		if err != nil {
			fmt.Printf("[Switcher] %s health check failed: %v\n", model, err)
		}

		if model == "vision" {
			fmt.Println("[Switcher] Backend is up, allowing 60s for weights to settle...")
			disp.Display(display.Status{
				Status: "Wait",
				Emoji:  "⏳",
				Text:   "Vision model stabilizing (60s)...",
			})
			for i := 0; i < 4; i++ {
				remaining := 60 - (i * 15)
				if i > 0 {
					go speakText(fmt.Sprintf("%d seconds remaining.", remaining))
				}
				time.Sleep(15 * time.Second)
			}
			fmt.Println("[Switcher] Vision model stabilized and ready")
			go speakText("Vision is ready.")
			disp.Display(display.Status{
				Status: "Ready",
				Emoji:  "✅",
				Text:   "Vision is ready! Press button to capture.",
			})
		}
	}

	switchModelAsync = func(model string) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 130*time.Second)
			defer cancel()
			err := llm.SwitchModel(ctx, switcherURL, model)
			if err != nil {
				fmt.Printf("[Switcher] Async error: %v\n", err)
			}
		}()
	}

	toggleCameraMode = func() {
		cameraMu.Lock()
		if !cameraIsActive {
			stopPlayback()
			cameraIsActive = true
			cameraMu.Unlock()

			fmt.Println("[Vision] Hold trigger: Enabling Vision Mode")
			go switchModelSync("vision", visionURL)
			cameraOn := true
			disp.Display(display.Status{
				CameraMode:       &cameraOn,
				CaptureImagePath: filepath.Join(os.TempDir(), "vision_capture.jpg"),
			})
		} else {
			// Camera IS active, so this hold means "turn it off"
			stopPlayback()
			cameraIsActive = false
			cameraMu.Unlock()

			fmt.Println("[Vision] Hold trigger: Disabling Vision Mode (Return to image_gen)")
			go switchModelSync("image_gen", hdnAddr)
			cameraOff := false
			disp.Display(display.Status{
				CameraMode: &cameraOff,
				Status:     "Idle",
				Emoji:      "😴",
				Text:       "Ready! Push button to talk.",
				RGB:        "#000055",
			})
			go speakText("Returning to chat mode.")
		}
	}

	switchModelAsync("image_gen")

	var vActive bool
	for ev := range disp.EventChan {
		// Global event handlers (regardless of current chatbot state)
		if ev == "camera_capture" {
			now := time.Now()
			// If we just handled a double press on the 'pressed' side, ignore the releases
			if now.Sub(lastDoublePressHandled) < 800*time.Millisecond {
				fmt.Println("[Vision] Ignoring capture event – just handled double-press stop.")
				continue
			}

			// If releases are too close together, it's a double-release (part of a double-click)
			if now.Sub(lastButtonReleaseTime) < 400*time.Millisecond {
				fmt.Println("[Vision] Double click detected on release - Cancelling capture and stopping speech.")
				stopPlayback()
				lastButtonReleaseTime = now
				lastDoublePressHandled = now // Mark it handled
				// Visual update to confirm stop
				disp.Display(display.Status{
					Status: "Stopped",
					Emoji:  "🔇",
					Text:   "Ready! Push button to talk.",
					RGB:    "#000055",
				})
				time.Sleep(1 * time.Second)
				cameraOn := true
				disp.Display(display.Status{
					CameraMode: &cameraOn,
					Status:     "Ready",
					Emoji:      "✅",
					Text:       "Vision is ready! Press button to capture.",
				})
				continue
			}
			lastButtonReleaseTime = now

			stopPlayback()

			stabMu.Lock()
			stabilizing := isStabilizing
			stabMu.Unlock()

			if stabilizing {
				fmt.Println("[Vision] Capture rejected - still stabilizing")
				go speakText("The vision model is still starting up. Please wait a few more seconds.")
				disp.Display(display.Status{
					Status: "Waiting",
					Emoji:  "⏳",
					Text:   "Vision model starting up... Please wait.",
					RGB:    "#FFFF00",
				})
				continue // FIX: was return, which exited the program
			}

			fmt.Println("[Vision] Processing capture...")
			capturePath := filepath.Join(os.TempDir(), "vision_capture.jpg")

			// Do NOT turn off CameraMode here, just update the status overlay
			disp.Display(display.Status{
				Status: "Thinking",
				Emoji:  "🔍",
				Text:   "Analyzing image...",
			})

			go func() {
				visionMu.Lock()
				isProcessingVision = true
				visionMu.Unlock()

				defer func() {
					visionMu.Lock()
					isProcessingVision = false
					visionMu.Unlock()
				}()

				// 1. Ensure vision is active and healthy
				// Since we might have triggered switch in background, we wait here
				ctxHealth, cancelHealth := context.WithTimeout(context.Background(), 130*time.Second)
				err := llm.WaitForHealth(ctxHealth, visionURL)
				cancelHealth()
				if err != nil {
					fmt.Printf("[Vision] Backend not ready: %v\n", err)
					disp.Display(display.Status{
						Status: "Error",
						Emoji:  "❌",
						Text:   "Vision backend not ready. Try again in a moment.",
					})
					// Do NOT switch back to image_gen automatically
					time.Sleep(3 * time.Second)
					cameraOn := true
					disp.Display(display.Status{
						CameraMode: &cameraOn,
						Status:     "Ready",
						Emoji:      "✅",
						Text:       "Vision is ready! Press button to capture.",
					})
					return
				}

				imgData, err := os.ReadFile(capturePath)
				if err != nil {
					fmt.Printf("[Vision] Error reading image: %v\n", err)
					return
				}
				b64 := base64.StdEncoding.EncodeToString(imgData)

				ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
				defer cancel()

				description, err := ai.DescribeImage(ctx, visionURL, b64)
				if err != nil {
					fmt.Printf("[Vision] Error: %v\n", err)
					disp.Display(display.Status{
						Status: "Error",
						Emoji:  "❌",
						Text:   "Vision error: " + err.Error(),
					})
					time.Sleep(3 * time.Second)
					cameraOn := true
					disp.Display(display.Status{
						CameraMode: &cameraOn,
						Status:     "Ready",
						Emoji:      "✅",
						Text:       "Vision is ready! Press button to capture.",
					})
					return
				}

				fmt.Printf("[Vision] Description: %s\n", description)

				// Do NOT switch back to image_gen yet - user wants to stay in vision mode
				disp.Display(display.Status{
					Status: "Vision Result",
					Emoji:  "👁️",
					Text:   description,
					RGB:    "#00ffff",
				})

				speakText(cleanForSpeech(description))

				// NOW reactivate the camera preview
				cameraOn := true
				disp.Display(display.Status{
					CameraMode: &cameraOn,
					Status:     "Ready",
					Emoji:      "✅",
					Text:       "Vision is ready! Press button to capture.",
				})
			}()
			continue
		}

		if ev == "exit_camera_mode" {
			fmt.Println("[Vision] Exiting camera mode (Event from UI)")
			cameraMu.Lock()
			cameraIsActive = false
			cameraMu.Unlock()
			stabMu.Lock()
			isStabilizing = false
			stabMu.Unlock()

			// Check if we already started a switch to image_gen
			// Most times this event comes from our own Display(CameraMode: false) call
			// but could be from a long-press in Python UI.
			continue
		}

		switch state {
		case StateIdle:
			if ev == "button_pressed" {
				visionMu.Lock()
				processing := isProcessingVision
				visionMu.Unlock()
				if processing {
					fmt.Println("[System] Button press ignored - processing vision result.")
					continue
				}

				// Double-press detection to stop speech
				now := time.Now()
				if now.Sub(lastButtonPressTime) < 500*time.Millisecond {
					fmt.Println("Double press verified – stopping playback.")
					stopPlayback()
					lastDoublePressHandled = now
				}
				lastButtonPressTime = now

				cameraMu.Lock()
				vActive = cameraIsActive
				cameraMu.Unlock()

				state = StateListening
				buttonPressTime = time.Now()

				audioPath = filepath.Join(os.TempDir(), fmt.Sprintf("rec_%d.wav", time.Now().Unix()))
				fmt.Println("Recording to", audioPath)
				cmd, err := audio.RecordAudio(audioPath)
				if err != nil {
					fmt.Printf("Record error: %v\n", err)
					state = StateIdle
					continue
				}
				recordCmd = cmd

				if vActive {
					fmt.Println("[Vision] Recording started for potential command (Hold for voice, Tap for capture)")
					disp.Display(display.Status{
						Status: "Listening",
						Emoji:  "🎤",
						Text:   "Hold for voice command...",
						RGB:    "#00ff88",
					})
				} else {
					disp.Display(display.Status{
						Status: "Listening",
						Emoji:  "😐",
						Text:   "Listening...",
						RGB:    "#00ff00",
					})
				}
			}
		case StateListening:
			if ev == "button_released" {
				holdDuration := time.Since(buttonPressTime)

				// Check current camera status
				cameraMu.Lock()
				vActive = cameraIsActive
				cameraMu.Unlock()

				// If in Camera Mode, distinguish between Tap (Capture) and Hold (Voice)
				if vActive {
					if holdDuration < 800*time.Millisecond {
						fmt.Printf("[System] Short tap in Vision Mode (%v). Expecting capture event.\n", holdDuration)
						state = StateIdle
						audio.StopRecording(recordCmd)
						recordCmd = nil
						if audioPath != "" {
							os.Remove(audioPath)
						}
						continue
					}
					fmt.Printf("[System] Long press in Vision Mode (%v). Proceeding to ASR for command.\n", holdDuration)
				}

				state = StateASR
				audio.StopRecording(recordCmd)
				recordCmd = nil

				disp.Display(display.Status{
					Status: "Recognizing",
					Emoji:  "⏳",
					Text:   "Processing audio...",
					RGB:    "#ff6800",
				})

				// Perform ASR
				info, err := os.Stat(audioPath)
				if err != nil {
					fmt.Printf("Stat error: %v\n", err)
					state = StateIdle
					continue
				}

				fmt.Printf("Recorded audio size: %d bytes\n", info.Size())

				// Ignore very short recordings (less than 8KB which is roughly 0.25s of 16kHz audio)
				if info.Size() < 8000 {
					fmt.Println("Recording too short, ignoring.")
					state = StateIdle
					disp.Display(display.Status{
						Status: "Idle",
						Emoji:  "😴",
						Text:   "Ready! Push button to talk.",
						RGB:    "#000055",
					})
					os.Remove(audioPath)
					continue
				}

				text, err := stt.Recognize(whisperHost, audioPath)
				os.Remove(audioPath) // Cleanup recording
				if err != nil || text == "" {
					fmt.Printf("ASR error or empty: %v\n", err)
					state = StateIdle
					disp.Display(display.Status{
						Status: "Idle",
						Emoji:  "😴",
						Text:   "Didn't catch that.",
						RGB:    "#000055",
					})
					continue
				}

				fmt.Printf("User: %s\n", text)
				disp.Display(display.Status{
					Status: "Thinking",
					Emoji:  "🤔",
					Text:   text,
				})

				// Check for camera/vision keywords to trigger live feed or exit it
				lowerText := strings.ToLower(text)
				isCameraRequest := strings.Contains(lowerText, "camera") || strings.Contains(lowerText, "vision") || strings.Contains(lowerText, "see")
				isExitRequest := strings.Contains(lowerText, "exit") || strings.Contains(lowerText, "stop") || strings.Contains(lowerText, "chat") || strings.Contains(lowerText, "back")

				cameraMu.Lock()
				active := cameraIsActive
				cameraMu.Unlock()

				if isCameraRequest && !isExitRequest && !active {
					toggleCameraMode()
				} else if isExitRequest && (active || isCameraRequest) {
					if active {
						toggleCameraMode()
					}
				}

				// Let the user know we are thinking about it out loud
				// Don't start this if we just activated the camera or if we are stabilizing
				cameraMu.Lock()
				vActive = cameraIsActive
				cameraMu.Unlock()
				stabMu.Lock()
				stabilizing := isStabilizing
				stabMu.Unlock()

				if !vActive && !stabilizing {
					go speakText("I am thinking about " + cleanForSpeech(text))
				}

				state = StateThinking
				// Create a cancelable context for this specific AI request
				ctx, cancelAI := context.WithCancel(context.Background())

				type chatResult struct {
					resp *llm.ChatResponse
					err  error
				}
				chatChan := make(chan chatResult, 1)

				// Call AI in a goroutine so we can stay responsive to button presses
				go func() {
					res, err := ai.Chat(ctx, text+" (Brevity required: under 200 tokens)")
					chatChan <- chatResult{res, err}
				}()

				var aiResp *llm.ChatResponse
				lastPressThinking := time.Time{}
				aiTimedOut := false

			thinkingLoop:
				for {
					select {
					case res := <-chatChan:
						cancelAI() // Clean up context
						if res.err != nil {
							if res.err == context.Canceled {
								fmt.Println("AI request was canceled by user.")
								state = StateIdle
								break thinkingLoop
							}
							fmt.Printf("AI error: %v\n", res.err)
							state = StateIdle
							disp.Display(display.Status{
								Status: "Error",
								Emoji:  "❌",
								Text:   "AI error: " + res.err.Error(),
							})
							break thinkingLoop
						}
						aiResp = res.resp
						break thinkingLoop

					case ev2 := <-disp.EventChan:
						if ev2 == "button_pressed" {
							now := time.Now()
							diff := now.Sub(lastPressThinking)
							fmt.Printf("[Thinking Double-Press Check] Time since last: %v\n", diff)
							if diff < 500*time.Millisecond {
								fmt.Println("Double press during thinking – canceling request.")
								cancelAI()
								stopPlayback() // Stop the "I am thinking" audio too
								state = StateIdle
								disp.Display(display.Status{
									Status: "Idle",
									Emoji:  "😴",
									Text:   "Ready! Push button to talk.",
									RGB:    "#000055",
								})
								// Transition to Idle and wait for the goroutine to finish/fail
								aiTimedOut = true // Marker to skip the rest of this block
								break thinkingLoop
							}
							lastPressThinking = now
						}
					}
				}

				if state == StateIdle || aiTimedOut {
					continue
				}

				fmt.Printf("AI: %s\n", aiResp.Response)

				// Show thoughts if any
				if thoughtsSlice, ok := aiResp.Thoughts.([]string); ok && len(thoughtsSlice) > 0 {
					thoughtsStr := strings.Join(thoughtsSlice, "\n")
					disp.Display(display.Status{
						Status: "Thoughts",
						Text:   thoughtsStr,
					})
					time.Sleep(1 * time.Second)
				}

				// 3. Answer State
				state = StateAnswering

				// Use a channel to wait for the speech to finish
				speechDone := make(chan bool, 1)

				// Launch speech in background so synthesis starts now
				go func() {
					speakText(cleanForSpeech(aiResp.Response))
					speechDone <- true
				}()

				// Trigger display immediately (it will start scrolling)
				disp.Display(display.Status{
					Status: "Answering",
					Emoji:  "😊",
					Text:   cleanForDisplay(aiResp.Response),
					RGB:    "#0000ff",
				})

				// Wait for speech to finish, but allow a double-press to interrupt it.
				stopped := false
				lastPress := time.Time{}
			waitLoop:
				for {
					select {
					case <-speechDone:
						break waitLoop
					case ev2 := <-disp.EventChan:
						if ev2 == "button_pressed" {
							now := time.Now()
							diff := now.Sub(lastPress)
							fmt.Printf("[Double-Press Check] Time since last: %v\n", diff)
							if diff < 700*time.Millisecond {
								fmt.Println("Double press verified – stopping playback.")
								stopPlayback()
								stopped = true
								<-speechDone
								break waitLoop
							}
							lastPress = now
						}
					}
				}

				// Brief pause so the user can read the displayed text,
				// but skip it if they already pressed the button to cancel.
				if !stopped {
					time.Sleep(1 * time.Second)
				}
				state = StateIdle
				disp.Display(display.Status{
					Status: "Idle",
					Emoji:  "😴",
					Text:   "Ready! Push button to talk.",
					RGB:    "#000055",
				})
			}
		}
	}
}
