package main

import (
	"fmt"
	_"os"
	"io/ioutil"
	"encoding/json"
	"sync"
	"time"
	"strings"
	ffmpeg "github.com/u2takey/ffmpeg-go"
)

type Camera struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type Config struct {
	Cameras []Camera `json:"cameras"`
}

func main() {

	byteValue, err := ioutil.ReadFile("./config.json")
	if err != nil {
		fmt.Print(err)
	}

	var config Config
	if err := json.Unmarshal(byteValue, &config); err != nil {
		fmt.Println("Failed to unmarshal JSON:", err)
		return
	}

	var wg sync.WaitGroup

	wg.Add(len(config.Cameras))

	for i, camera := range config.Cameras {
		go func(src string, tg string, index int) {
			defer wg.Done()
			for {
				err := ffmpeg.Input(src).
					Output(tg, ffmpeg.KwArgs{"format": "rtsp", "s": "480x270", "r": "15", "c:a": "copy", "c:v": "libx265"}).
					OverWriteOutput().Run() // ErrorToStdOut
				
				// source
				srcLast := strings.LastIndex(src, "/") + 1
				srcLastStr := src[srcLast:]

				// target
				tgLast := strings.LastIndex(src, "/") + 1
				tgLastStr := tg[tgLast:]

				combindedStr :=  fmt.Sprintf("Channel: %v Source Name: %v Target Name: %v", index + 1, srcLastStr, tgLastStr)

				if err != nil {
					fmt.Println("Failed to build FFmpeg command:", err)
					fmt.Println(combindedStr)
				} else {
					// If the FFmpeg command completes successfully, break the loop
					fmt.Println("Disconnect")
					fmt.Println(combindedStr)
				}
				time.Sleep(5 * time.Second)
			}
		}(camera.Source, camera.Target, i)
	}

	wg.Wait() // Wait for all Goroutines to finish
}