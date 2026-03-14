package audio

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func FindLatestWav() string {
	files, _ := filepath.Glob("/tmp/melotts_output_*.wav")
	var latest string
	var maxTime time.Time
	for _, f := range files {
		info, err := os.Stat(f)
		if err == nil && info.ModTime().After(maxTime) {
			maxTime = info.ModTime()
			latest = f
		}
	}
	return latest
}

func RecordAudio(outputPath string) (*exec.Cmd, error) {
	device := "default"
	if cardIndex := os.Getenv("SOUND_CARD_INDEX"); cardIndex != "" {
		device = "plughw:" + cardIndex + ",0"
	}

	cmd := exec.Command("sox",
		"-t", "alsa", device,
		"-r", "16000",
		"-c", "1",
		"-t", "wav", outputPath,
	)

	err := cmd.Start()
	if err != nil {
		return nil, err
	}
	return cmd, nil
}

// StopRecording sends a SIGINT to the recording process and waits for it to exit.
func StopRecording(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	// Send SIGINT so sox finishes the file correctly
	cmd.Process.Signal(os.Interrupt)
	// IMPORTANT: Wait for sox to actually exit and flush the file
	_, err := cmd.Process.Wait()
	return err
}

// StartPlayback begins playback of filePath without blocking.
// It returns the running *exec.Cmd so the caller can cancel it via StopPlayback.
// The returned cmd.Wait() must eventually be called (StopPlayback does this).
func StartPlayback(filePath string) (*exec.Cmd, error) {
	device := "plughw:2,0"
	if cardIndex := os.Getenv("SOUND_CARD_INDEX"); cardIndex != "" {
		device = "plughw:" + cardIndex + ",0"
	}

	fmt.Printf("Playing audio: %s on %s\n", filePath, device)

	// Prefer sox play; fall back to aplay.
	var cmd *exec.Cmd
	if _, err := exec.LookPath("play"); err == nil {
		cmd = exec.Command("play", "-q", filePath)
	} else {
		cmd = exec.Command("aplay", "-D", device, filePath)
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

// StopPlayback kills a running playback command and waits for it to exit.
// It also forcefully kills common audio players to ensure silence.
func StopPlayback(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	// Forcefully kill any lingering audio processes.
	_ = exec.Command("pkill", "-9", "sox").Run()
	_ = exec.Command("pkill", "-9", "play").Run()
	_ = exec.Command("pkill", "-9", "aplay").Run()
}

// PlayAudio is the original blocking convenience wrapper around StartPlayback.
// It retries once if the audio device reports busy.
func PlayAudio(filePath string) error {
	// Let the audio hardware settle after recording.
	time.Sleep(1000 * time.Millisecond)

	device := "plughw:2,0"
	if cardIndex := os.Getenv("SOUND_CARD_INDEX"); cardIndex != "" {
		device = "plughw:" + cardIndex + ",0"
	}

	fmt.Printf("Playing audio: %s on %s\n", filePath, device)

	playFunc := func() error {
		// Attempt 1: Standard sox play (auto-detects format)
		fmt.Printf("Attempting sox play... ")
		cmd1 := exec.Command("sudo", "play", "-q", filePath)
		if err := cmd1.Run(); err == nil {
			fmt.Println("Success")
			return nil
		}

		// Attempt 2: aplay (ALSA's native player, good for raw WAV)
		fmt.Printf("Attempting native aplay... ")
		cmd2 := exec.Command("sudo", "aplay", "-D", device, filePath)
		out, err := cmd2.CombinedOutput()
		if err == nil {
			fmt.Println("Success")
			return nil
		}
		return fmt.Errorf("%v: %s", err, string(out))
	}

	err := playFunc()
	if err != nil && (strings.Contains(err.Error(), "busy") || strings.Contains(err.Error(), "status 1")) {
		fmt.Println("\n!! Device busy. Forcefully killing audio processes and retrying...")
		exec.Command("sudo", "pkill", "-9", "sox").Run()
		exec.Command("sudo", "pkill", "-9", "play").Run()
		exec.Command("sudo", "pkill", "-9", "aplay").Run()
		time.Sleep(1000 * time.Millisecond)
		return playFunc()
	}
	return err
}

func PlayMP3(filePath string) error {
	time.Sleep(200 * time.Millisecond)

	device := "default"
	if cardIndex := os.Getenv("SOUND_CARD_INDEX"); cardIndex != "" {
		device = "hw:" + cardIndex + ",0"
	}

	cmd := exec.Command("mpg123", "-a", device, filePath)
	return cmd.Run()
}
