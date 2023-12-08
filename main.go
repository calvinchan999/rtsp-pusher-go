package main

import (
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Camera struct {
	Source      string `json:"source"`
	Target      string `json:"target"`
	Resolution  string `json:"resolution"`
	Framerate   string `json:"framerate"`
	Encoder     string `json:"encoder"`
	Filter      string `json:"filter"`
	Bitrate		string `json:"bitrate"`
}

type Config struct {
	Cameras []Camera `json:"cameras"`
}

type GoroutineParams struct {
	Source      string
	Target      string
	Resolution  string
	Framerate   string
	Encoder     string
	Filter      string
	Bitrate		string
	Index       int
}

func main() {
	byteValue, err := os.Open("./config/config.json")
	if err != nil {
		log.Fatal(err)
	}

	var config Config
	if err := json.NewDecoder(byteValue).Decode(&config); err != nil {
		log.Println("Failed to decode JSON:", err)
	}

	concurrencyLimit := 30 // Adjust the value based on your system's capabilities
	goroutineChannel := make(chan struct{}, concurrencyLimit)
	var wg sync.WaitGroup

	for i, camera := range config.Cameras {
		params := GoroutineParams{
			Source:      camera.Source,
			Target:      camera.Target,
			Resolution:  camera.Resolution,
			Framerate:   camera.Framerate,
			Encoder:     camera.Encoder,
			Filter:      camera.Filter,
			Bitrate:	 camera.Bitrate,
			Index:       i + 1,
		}

		wg.Add(1)
		go processCamera(&wg, goroutineChannel, params)
	}

	wg.Wait() // Wait for all Goroutines to finish
}

func processCamera(wg *sync.WaitGroup, goroutineChannel chan struct{}, params GoroutineParams) {
	defer wg.Done()
	for {
		goroutineChannel <- struct{}{} // Acquire a spot in the channel
		err := runFFmpegCommand(params)
		<-goroutineChannel // Release the spot in the channel

		if err != nil {
			log.Println("Failed to execute FFmpeg command:", err)
		} else {
			log.Println("Disconnect")
		}
		time.Sleep(5 * time.Second)
	}
}

func runFFmpegCommand(params GoroutineParams) error {
	srcLast := lastPathComponent(params.Source)
	tgLast := lastPathComponent(params.Target)

	log.Printf("Channel: %v Source Name: %v Target Name: %v\n", params.Index, srcLast, tgLast)
	
	// fpsString := strconv.Itoa(params.Framerate) // not work

	cmdArgs := []string{}

	if params.Filter != "" {
		cmdArgs = append(cmdArgs,
			"-rtsp_transport", "tcp",
			"-flags", "low_delay",
			"-timeout", "30000000", // 30s
			"-i", params.Source,
			"-s", params.Resolution,
			"-r", params.Framerate,
			"-c:a", "copy",
			"-c:v", "libx264",
			"-vf", params.Filter,
			"-b:v", params.Bitrate,
			"-preset", "ultrafast",
			"-tune", "zerolatency",
			"-use_wallclock_as_timestamps", "1",
			"-an",
			"-f", "flv",
			params.Target,
		)
	} else {
		cmdArgs = append(cmdArgs,
			"-rtsp_transport", "tcp",
			"-flags", "low_delay", 
			"-timeout", "30000000", // 30s
			"-i", params.Source,
			"-s", params.Resolution,
			"-r", params.Framerate,
			"-c:a", "copy",
			"-c:v", "libx264",
			"-b:v", params.Bitrate,
			"-preset", "ultrafast",
			"-tune", "zerolatency",
			"-use_wallclock_as_timestamps", "1",
			"-an",
			"-f", "flv",
			params.Target,
		)
	}
	cmd := exec.Command("ffmpeg", cmdArgs...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	err := cmd.Run()
	return err
}

func lastPathComponent(path string) string {
	index := strings.LastIndex(path, "/") + 1
	return path[index:]
}