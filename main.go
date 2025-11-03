package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	onlivesAPI      = "https://www.showroom-live.com/api/live/onlives"
	streamingAPIFmt = "https://www.showroom-live.com/api/live/streaming_url?room_id=%d&abr_available=1"
	pollInterval    = 3 * time.Second
)

// OnlivesResponse represents the response from /api/live/onlives
type OnlivesResponse struct {
	Onlives []struct {
		GenreID   int    `json:"genre_id"`
		GenreName string `json:"genre_name"`
		Lives     []struct {
			RoomID     int    `json:"room_id"`
			RoomURLKey string `json:"room_url_key"`
			StartedAt  int64  `json:"started_at"`
		} `json:"lives"`
	} `json:"onlives"`
}

// StreamingURLResponse represents the response from /api/live/streaming_url
type StreamingURLResponse struct {
	StreamingURLList []struct {
		ID        int    `json:"id"`
		Label     string `json:"label"`
		Quality   int    `json:"quality"`
		Type      string `json:"type"`
		URL       string `json:"url"`
		IsDefault bool   `json:"is_default"`
	} `json:"streaming_url_list"`
}

type Recorder struct {
	roomURLKey     string
	currentRoomID  int
	isRecording    bool
	ffmpegCmd      *exec.Cmd
	stopChan       chan struct{}
	recordingCount int
	debug          bool
}

func main() {
	debugFlag := flag.Bool("debug", false, "Enable debug mode (show ffmpeg logs)")
	flag.Parse()

	if flag.NArg() < 1 {
		log.Fatal("Usage: go run main.go [--debug] <room_url>")
	}

	roomURL := flag.Arg(0)
	roomURLKey, err := extractRoomURLKey(roomURL)
	if err != nil {
		log.Fatalf("Failed to parse room URL: %v", err)
	}

	log.Printf("Started monitoring: %s", roomURLKey)

	recorder := &Recorder{
		roomURLKey: roomURLKey,
		debug:      *debugFlag,
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Initial check
	recorder.checkAndRecord()

	for {
		select {
		case <-ticker.C:
			recorder.checkAndRecord()
		case <-sigChan:
			log.Println("Received shutdown signal")
			recorder.stopRecording()
			return
		}
	}
}

// extractRoomURLKey extracts the room_url_key from a Showroom URL
func extractRoomURLKey(roomURL string) (string, error) {
	// Expected format: https://www.showroom-live.com/r/watashi_idol_0196
	parts := strings.Split(roomURL, "/r/")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid room URL format")
	}

	roomURLKey := strings.TrimSpace(parts[1])
	if roomURLKey == "" {
		return "", fmt.Errorf("empty room URL key")
	}

	return roomURLKey, nil
}

// checkAndRecord checks if the room is live and manages recording state
func (r *Recorder) checkAndRecord() {
	roomID, isLive := r.checkLiveStatus()

	if isLive && !r.isRecording {
		// Room just went live - start recording
		r.currentRoomID = roomID
		r.startRecording()
	} else if !isLive && r.isRecording {
		// Room went offline - stop recording
		r.stopRecording()
	}
}

// checkLiveStatus polls the onlives API to check if the room is live
func (r *Recorder) checkLiveStatus() (int, bool) {
	resp, err := http.Get(onlivesAPI)
	if err != nil {
		log.Printf("Error fetching onlives: %v", err)
		return 0, false
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response: %v", err)
		return 0, false
	}

	var onlivesResp OnlivesResponse
	if err := json.Unmarshal(body, &onlivesResp); err != nil {
		log.Printf("Error parsing JSON: %v", err)
		return 0, false
	}

	// Search for our room in the onlives list (nested in genres)
	for _, genre := range onlivesResp.Onlives {
		for _, room := range genre.Lives {
			if room.RoomURLKey == r.roomURLKey {
				return room.RoomID, true
			}
		}
	}

	// Room not found in live list
	return 0, false
}

// getStreamingURL fetches the streaming URL with highest quality
func (r *Recorder) getStreamingURL() (string, error) {
	url := fmt.Sprintf(streamingAPIFmt, r.currentRoomID)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var streamResp StreamingURLResponse
	if err := json.Unmarshal(body, &streamResp); err != nil {
		return "", err
	}

	if len(streamResp.StreamingURLList) == 0 {
		return "", fmt.Errorf("no streaming URLs available")
	}

	// Find the stream with highest quality
	var bestStream *struct {
		ID        int    `json:"id"`
		Label     string `json:"label"`
		Quality   int    `json:"quality"`
		Type      string `json:"type"`
		URL       string `json:"url"`
		IsDefault bool   `json:"is_default"`
	}

	for i := range streamResp.StreamingURLList {
		stream := &streamResp.StreamingURLList[i]
		if bestStream == nil || stream.Quality > bestStream.Quality {
			bestStream = stream
		}
	}

	return bestStream.URL, nil
}

// startRecording starts ffmpeg to record the stream
func (r *Recorder) startRecording() {
	streamURL, err := r.getStreamingURL()
	if err != nil {
		log.Printf("Error getting streaming URL: %v", err)
		return
	}

	r.recordingCount++
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("%d_%s_%d.ts", r.currentRoomID, timestamp, r.recordingCount)

	log.Printf("Started recording: %s", filename)

	// Create output directory if it doesn't exist
	outputDir := "recordings"
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Printf("Error creating output directory: %v", err)
		return
	}

	outputPath := filepath.Join(outputDir, filename)

	// Use ffmpeg to download HLS stream and save to TS
	// -i: input URL
	// -c copy: copy codec without re-encoding (faster, native format)
	r.ffmpegCmd = exec.Command("ffmpeg",
		"-i", streamURL,
		"-c", "copy",
		"-y", // overwrite output file if exists
		outputPath,
	)

	// Only show ffmpeg output in debug mode
	if r.debug {
		r.ffmpegCmd.Stdout = os.Stdout
		r.ffmpegCmd.Stderr = os.Stderr
	} else {
		r.ffmpegCmd.Stdout = io.Discard
		r.ffmpegCmd.Stderr = io.Discard
	}

	r.stopChan = make(chan struct{})

	if err := r.ffmpegCmd.Start(); err != nil {
		log.Printf("Error starting ffmpeg: %v", err)
		return
	}

	r.isRecording = true

	// Monitor ffmpeg process
	go func() {
		err := r.ffmpegCmd.Wait()
		select {
		case <-r.stopChan:
			// Normal stop
			log.Printf("Recording saved: %s", outputPath)
		default:
			// Unexpected exit
			if err != nil {
				log.Printf("ffmpeg exited with error: %v", err)
			} else {
				log.Printf("ffmpeg exited unexpectedly")
			}
		}
		r.isRecording = false
	}()
}

// stopRecording stops the current recording
func (r *Recorder) stopRecording() {
	if !r.isRecording || r.ffmpegCmd == nil {
		return
	}

	// Close stop channel to signal normal stop
	close(r.stopChan)

	// Send interrupt signal to ffmpeg for graceful shutdown
	if err := r.ffmpegCmd.Process.Signal(os.Interrupt); err != nil {
		log.Printf("Error sending interrupt to ffmpeg: %v", err)
		// Force kill if interrupt fails
		r.ffmpegCmd.Process.Kill()
	}

	// Wait a bit for ffmpeg to finish
	time.Sleep(2 * time.Second)

	r.isRecording = false
	r.ffmpegCmd = nil
}
