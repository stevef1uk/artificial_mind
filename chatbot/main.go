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
	"net/http"
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

const cameraHoldThreshold = 4 * time.Second

func isRemoteCameraAvailable(url string) bool {
	if url == "" {
		return true
	}
	// Strip trailing slash if present to avoid double slashes
	url = strings.TrimSuffix(url, "/")

	fmt.Printf("[Camera] Checking camera availability at: %s\n", url)
	client := &http.Client{Timeout: 5 * time.Second}

	// 1. Try dedicated health endpoint first (lightweight)
	resp, err := client.Get(url + "/health")
	if err == nil {
		if resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			fmt.Printf("[Camera] Remote camera healthy via /health\n")
			return true
		}
		fmt.Printf("[Camera] /health returned status: %d\n", resp.StatusCode)
		resp.Body.Close()
	} else {
		fmt.Printf("[Camera] /health check error: %v\n", err)
	}

	// 2. Fallback to /preview (traditional check, but heavier)
	fmt.Printf("[Camera] Checking /preview fallback...\n")
	resp, err = client.Get(url + "/preview")
	if err != nil {
		fmt.Printf("[Camera] /preview check error: %v\n", err)
		return false
	}
	defer resp.Body.Close()
	fmt.Printf("[Camera] /preview status: %d\n", resp.StatusCode)
	return resp.StatusCode == http.StatusOK
}

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

	hdnAddr := getAddr("HDN_ADDR", "http://192.168.1.53:8081")
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
	var speakCancel bool
	var cancelMu sync.Mutex
	var cameraTimer *time.Timer
	var cameraTimerMu sync.Mutex
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

	// Function Definitions
	stopPlayback = func() {
		cancelMu.Lock()
		speakCancel = true
		cancelMu.Unlock()

		playMu.Lock()
		cmd := playCmd
		playCmd = nil
		playMu.Unlock()

		if cmd != nil {
			fmt.Println("Stopping playback process...")
		}
		audio.StopPlayback(cmd)
	}

	speakText = func(sayText string) {
		audioMu.Lock()
		defer audioMu.Unlock()

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

	switchModelAsync("image_gen")

	var vActiveAtPress bool

	for ev := range disp.EventChan {
		// Global event handlers (regardless of current chatbot state)
		if ev == "camera_capture" {
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
						Text:   "Vision backend not ready",
					})
					switchModelAsync("image_gen") // fallback
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
					// Also switch back to image_gen if it fails
					switchModelAsync("image_gen")
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
			if cameraIsActive {
				cameraIsActive = false
				cameraMu.Unlock()

				fmt.Println("[Vision] UI event: Disabling Vision Mode (Returning to talk mode)")
				go switchModelSync("image_gen", hdnAddr)
				cameraOff := false
				disp.Display(display.Status{
					CameraMode: &cameraOff,
					Status:     "Idle",
					Emoji:      "😴",
					Text:       "Ready! Returning to talk mode.",
					RGB:        "#000055",
				})
				go speakText("Returning to talk mode.")
			} else {
				cameraMu.Unlock()
			}

			stabMu.Lock()
			isStabilizing = false
			stabMu.Unlock()

			if state == StateListening {
				fmt.Println("[Vision] UI exit event received while listening – returning to Idle")
				state = StateIdle
			}
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
				}
				lastButtonPressTime = now

				cameraMu.Lock()
				vActiveAtPress = cameraIsActive
				cameraMu.Unlock()

				state = StateListening
				buttonPressTime = time.Now()

				if !vActiveAtPress {
					audioPath = filepath.Join(os.TempDir(), fmt.Sprintf("rec_%d.wav", time.Now().Unix()))
					fmt.Println("Recording to", audioPath)
					cmd, err := audio.RecordAudio(audioPath)
					if err != nil {
						fmt.Printf("Record error: %v\n", err)
						state = StateIdle
						continue
					}
					recordCmd = cmd

					disp.Display(display.Status{
						Status: "Listening",
						Emoji:  "😐",
						Text:   "Listening...",
						RGB:    "#00ff00",
					})
				} else {
					fmt.Println("[Vision] Button pressed in Vision Mode (Monitoring for hold/release)")
					// We don't record audio if vision is already active
				}
				// Start the hold timer ONLY if camera is active (to handle EXIT hold)
				// Activation is now strictly by speech.
				cameraMu.Lock()
				active := cameraIsActive
				cameraMu.Unlock()

				if active {
					cameraTimerMu.Lock()
					cameraTimer = time.AfterFunc(cameraHoldThreshold, func() {
						cameraMu.Lock()
						if cameraIsActive {
							cameraIsActive = false
							cameraMu.Unlock()

							fmt.Println("[Vision] Hold trigger: Disabling Vision Mode (Return to image_gen)")
							go switchModelSync("image_gen", hdnAddr)
							cameraOff := false
							disp.Display(display.Status{
								CameraMode: &cameraOff,
								Status:     "Idle",
								Emoji:      "😴",
								Text:       "Ready! Returning to talk mode.",
								RGB:        "#000055",
							})
							go speakText("Returning to talk mode.")
						} else {
							cameraMu.Unlock()
						}
					})
					cameraTimerMu.Unlock()
				}
			}
		case StateListening:
			if ev == "button_released" {
				state = StateASR
				audio.StopRecording(recordCmd)
				recordCmd = nil
				_ = time.Since(buttonPressTime)

				// Cancel the proactive timer
				cameraTimerMu.Lock()
				if cameraTimer != nil {
					cameraTimer.Stop()
					cameraTimer = nil
				}
				cameraTimerMu.Unlock()

				// If vision was active at press time, this button interaction
				// was either for capture or for exit. Never proceed to ASR.
				if vActiveAtPress {
					fmt.Println("[System] Release handled for Vision mode interaction. Returning to Idle.")
					state = StateIdle
					if audioPath != "" {
						os.Remove(audioPath)
					}
					continue
				}

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
				// Check for camera/vision keywords to trigger live feed
				lowerText := strings.ToLower(text)
				intentHandled := false
				if strings.Contains(lowerText, "camera") || strings.Contains(lowerText, "vision mode") {
					cameraMu.Lock()
					activeNow := cameraIsActive
					if !activeNow {
						stopPlayback()
						if !isRemoteCameraAvailable(remoteCameraURL) {
							fmt.Println("[Vision] Keyword trigger rejected: Remote camera unavailable")
							go speakText("The remote camera is currently unavailable. Please check if the camera Pi is turned on.")
							cameraMu.Unlock()
							intentHandled = true
						} else {
							cameraIsActive = true
							cameraMu.Unlock()

							fmt.Println("[Vision] Triggering live camera feed via keywords...")
							go switchModelSync("vision", visionURL)
							cameraOn := true
							disp.Display(display.Status{
								CameraMode:       &cameraOn,
								CaptureImagePath: filepath.Join(os.TempDir(), "vision_capture.jpg"),
							})
							intentHandled = true
							go speakText("Switching to vision mode.")
						}
					} else {
						cameraMu.Unlock()
					}
				}

				if intentHandled {
					state = StateIdle
					disp.Display(display.Status{
						Status: "Ready",
						Emoji:  "✅",
						Text:   "Vision mode requested.",
					})
					continue
				}

				// Let the user know we are thinking about it out loud
				// Don't start this if we just activated the camera or if we are stabilizing
				cameraMu.Lock()
				vCurrentlyActive := cameraIsActive
				cameraMu.Unlock()
				stabMu.Lock()
				stabilizing := isStabilizing
				stabMu.Unlock()

				if !vCurrentlyActive && !stabilizing {
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
