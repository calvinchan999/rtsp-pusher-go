package main

import (
	"encoding/json"
	// "fmt"
	// "io/ioutil"
	"os"
	"log"
	"strings"
	"sync"
	"time"

	ffmpeg "github.com/u2takey/ffmpeg-go"
)

type Camera struct {
	Source     string `json:"source"`
	Target     string `json:"target"`
	Resolution string `json:"resolution"`
	Framerate  int    `json:"framerate"`
	Encoder    string `json:"encoder"`
}

type Config struct {
	Cameras []Camera `json:"cameras"`
}

type GoroutineParams struct {
	Source     string
	Target     string
	Resolution string
	Framerate  int
	Encoder    string
	Index      int
}

func main() {
	byteValue, err := os.Open("./config.json")
	if err != nil {
		log.Fatal(err)
	}

	var config Config
	// if err := json.Unmarshal(byteValue, &config); err != nil {
	// 	log.Println("Failed to unmarshal JSON:", err)
	// 	return
	// }

	if err := json.NewDecoder(byteValue).Decode(&config); err != nil {
		log.Println("Failed to decode JSON:", err)
	}

	concurrencyLimit := 8 // Adjust the value based on your system's capabilities
	goroutineChannel := make(chan struct{}, concurrencyLimit)
	var wg sync.WaitGroup

	for i, camera := range config.Cameras {
		params := GoroutineParams{
			Source:     camera.Source,
			Target:     camera.Target,
			Resolution: camera.Resolution,
			Framerate:  camera.Framerate,
			Encoder:    camera.Encoder,
			Index:      i + 1,
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
			log.Println("Failed to build FFmpeg command:", err)
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

	err := ffmpeg.Input(params.Source).
		Output(params.Target, ffmpeg.KwArgs{
			"format":     "rtsp",
			"s":          params.Resolution,
			"r":          params.Framerate,
			"c:a":        "copy",
			"c:v":        params.Encoder,
		}).
		OverWriteOutput().
		Run()

	return err
}

func lastPathComponent(path string) string {
	index := strings.LastIndex(path, "/") + 1
	return path[index:]
}