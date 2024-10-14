package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"runtime"
	"github.com/gorilla/mux"
)

type Camera struct {
    Source     string `json:"source"`
    Target     string `json:"target"`
    Resolution string `json:"resolution,omitempty"`
    Framerate  string `json:"framerate,omitempty"`
    Filter     string `json:"filter,omitempty"`
    Encoder    string `json:"encoder,omitempty"`
    Bitrate    string `json:"bitrate,omitempty"`
    Crf        string `json:"crf,omitempty"`
    Enabled    string `json:"enabled,omitempty"`
}


type Config struct {
	Cameras []Camera `json:"cameras"`
}

type GoroutineParams struct {
	Camera Camera
	Index  int
}

type CameraStatus struct {
	Id int `json:"id"`
	Source string `json:"source"`
	StreamUrl string `json:"streamUrl"`
	Resolution string `json:"resolution"`
	Framerate string `json:"framerate"`
    Status string `json:"status"`
}

var (
	config           Config
	configMutex      sync.RWMutex
    saveMutex        sync.Mutex
	configPath       = "./config/config.json"
	cameraProcesses  = make(map[int]*exec.Cmd)
	cameraProcessMux sync.Mutex
)

func loadConfig() error {
    file, err := os.Open(configPath)
    if err != nil {
        return fmt.Errorf("failed to open config file: %w", err)
    }
    defer file.Close()

    configMutex.Lock()
    if err := json.NewDecoder(file).Decode(&config); err != nil {
        configMutex.Unlock()
        return fmt.Errorf("failed to decode JSON: %w", err)
    }

    configChanged := false

    // Set default values for missing fields
	for i := range config.Cameras {
        if strings.HasPrefix(config.Cameras[i].Target, "rtmp://") {
            if config.Cameras[i].Encoder == "" {
                config.Cameras[i].Encoder = "libx264"
                configChanged = true
            }
            if config.Cameras[i].Bitrate == "" {
                config.Cameras[i].Bitrate = "100k"
                configChanged = true
            }
            if config.Cameras[i].Crf == "" {
                config.Cameras[i].Crf = "18"
                configChanged = true
            }
            if config.Cameras[i].Resolution == "" {
                config.Cameras[i].Resolution = "480x270"
                configChanged = true
            }
            if config.Cameras[i].Framerate == "" {
                config.Cameras[i].Framerate = "15"
                configChanged = true
            }
        }
        if config.Cameras[i].Enabled == "" {
            config.Cameras[i].Enabled = "true"
            configChanged = true
        }
    }

    configMutex.Unlock()

    // Only save if changes were made
    if configChanged {
        if err := saveConfig(); err != nil {
            return fmt.Errorf("failed to save updated config: %w", err)
        }
    }

    return nil
}

func reloadConfig() {
    configMutex.Lock()
    defer configMutex.Unlock()
    
    file, err := os.Open(configPath)
    if err != nil {
        log.Printf("Failed to open config file: %v", err)
        return
    }
    defer file.Close()

    if err := json.NewDecoder(file).Decode(&config); err != nil {
        log.Printf("Failed to decode JSON: %v", err)
        return
    }
    
	// Debug
    // log.Println("Configuration reloaded")
}

func saveConfig() error {
    saveMutex.Lock()
    defer saveMutex.Unlock()

    configMutex.RLock()
    configCopy := config
    configMutex.RUnlock()

    file, err := os.Create(configPath)
    if err != nil {
        return fmt.Errorf("failed to create config file: %w", err)
    }
    defer file.Close()

    encoder := json.NewEncoder(file)
    encoder.SetIndent("", "  ") // Pretty print the JSON
    if err := encoder.Encode(configCopy); err != nil {
        return fmt.Errorf("failed to encode config: %w", err)
    }

    return nil
}

func startControlAPI() {
	router := mux.NewRouter()
	router.HandleFunc("/camera/{id}/control", controlCameraHandler).Methods("POST")
	router.HandleFunc("/camera/status", getCameraStatusHandler).Methods("GET")

	log.Println("Starting control API server on :8080")
	if err := http.ListenAndServe(":8080", router); err != nil {
		log.Fatal("Failed to start control API server:", err)
	}
}

func controlCameraHandler(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    cameraID, err := strconv.Atoi(vars["id"])
    if err != nil {
        http.Error(w, "Invalid camera ID", http.StatusBadRequest)
        return
    }

    var action struct {
        Action bool `json:"action"`
    }
    if err := json.NewDecoder(r.Body).Decode(&action); err != nil {
        http.Error(w, "Invalid request body", http.StatusBadRequest)
        return
    }

    if cameraID < 1 || cameraID > len(config.Cameras) {
        http.Error(w, "Camera ID out of range", http.StatusBadRequest)
        return
    }

    cameraIndex := cameraID - 1

	configMutex.Lock()
    // config.Cameras[cameraIndex].Enabled = action.Action

	if action.Action {
        config.Cameras[cameraIndex].Enabled = "true"
    } else {
        config.Cameras[cameraIndex].Enabled = "false"
    }

    configMutex.Unlock()

	if !action.Action {
        cameraProcessMux.Lock()
        if cmd, exists := cameraProcesses[cameraID]; exists {
            log.Printf("Attempting to terminate process for camera %d", cameraID)
            if err := cmd.Process.Signal(os.Interrupt); err != nil {
                log.Printf("Failed to send interrupt signal to process for camera %d: %v", cameraID, err)
            }
            go func() {
                err := cmd.Wait()
                log.Printf("Process for camera %d exited. Error: %v", cameraID, err)
                cameraProcessMux.Lock()
                delete(cameraProcesses, cameraID)
                cameraProcessMux.Unlock()
            }()
        } else {
            log.Printf("No active process found for camera %d", cameraID)
        }
        cameraProcessMux.Unlock()
    }

    if err := saveConfig(); err != nil {
        log.Printf("Failed to save config: %v", err)
        http.Error(w, "Failed to save configuration", http.StatusInternalServerError)
        return
    }

	reloadConfig()

    w.WriteHeader(http.StatusOK)
    if action.Action {
        fmt.Fprintf(w, "Camera %d turned on", cameraID)
    } else {
        fmt.Fprintf(w, "Camera %d turned off", cameraID)
    }
}

func getCameraStatusHandler(w http.ResponseWriter, r *http.Request) {
	
	configMutex.RLock()
	defer configMutex.RUnlock()

	status := make([]CameraStatus, len(config.Cameras))
	for i, camera := range config.Cameras {
		status[i] = CameraStatus{
			Id:         i + 1,
			Source:     camera.Source,
			StreamUrl:  camera.Target,
			Resolution: camera.Resolution,
			Framerate:  camera.Framerate,
			Status:     strings.ToLower(camera.Enabled),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func processCamera(wg *sync.WaitGroup, goroutineChannel chan struct{}, params GoroutineParams) {
    defer wg.Done()
    
    var cmd *exec.Cmd
    var cmdMutex sync.Mutex

    for {
        goroutineChannel <- struct{}{} // Acquire a spot in the channel
        configMutex.RLock()
        enabled := false
		if params.Index > 0 && params.Index <= len(config.Cameras) {
			// enabled = config.Cameras[params.Index-1].Enabled
			 enabled = strings.ToLower(config.Cameras[params.Index-1].Enabled) == "true"
		}
        configMutex.RUnlock()

		// Debug
        // log.Printf("Camera %d enabled status: %v", params.Index, enabled)
        
        if enabled {
            if cmd == nil {
                go func() {
                    cmdMutex.Lock()
                    cmd = exec.Command("ffmpeg", buildFFmpegArgs(params.Camera)...)
                    cmdMutex.Unlock()
                    
                    err := cmd.Run()
                    if err != nil {
                        log.Printf("FFmpeg command for camera %d exited with error: %v", params.Index, err)
                    }
                    
                    cmdMutex.Lock()
                    cmd = nil
                    cmdMutex.Unlock()
                }()
            }
        } else {
            cmdMutex.Lock()
            if cmd != nil && cmd.Process != nil {
                log.Printf("Stopping FFmpeg command for camera %d", params.Index)
                // if err := cmd.Process.Signal(os.Interrupt); err != nil {
                //     log.Printf("Failed to stop FFmpeg command for camera %d: %v", params.Index, err)
                // }
				if err := stopCommand(cmd); err != nil {
                    log.Printf("Failed to stop FFmpeg command for camera %d: %v", params.Index, err)
                }
            }
            cmdMutex.Unlock()
        }
        
        <-goroutineChannel // Release the spot in the channel
        time.Sleep(5 * time.Second)
    }
}

func stopCommand(cmd *exec.Cmd) error {
    if runtime.GOOS == "windows" {
        return cmd.Process.Kill()
    }
    return cmd.Process.Signal(os.Interrupt)
}


func runFFmpegCommand(params GoroutineParams) error {
	srcLast := lastPathComponent(params.Camera.Source)
	tgLast := lastPathComponent(params.Camera.Target)

	log.Printf("Channel: %v Source Name: %v Target Name: %v\n", params.Index, srcLast, tgLast)

	cmdArgs := buildFFmpegArgs(params.Camera)

	cmd := exec.Command("ffmpeg", cmdArgs...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	cameraProcessMux.Lock()
	cameraProcesses[params.Index] = cmd
	cameraProcessMux.Unlock()

	err := cmd.Run()

	cameraProcessMux.Lock()
	delete(cameraProcesses, params.Index)
	cameraProcessMux.Unlock()

	return err
}

func buildFFmpegArgs(camera Camera) []string {
    var cmdArgs []string

    // Common arguments for both RTMP and RTSP
    cmdArgs = []string{
        "-rtsp_transport", "tcp",
        "-timeout", "30000000", // 30s
        "-i", camera.Source,
    }

    if strings.HasPrefix(camera.Target, "rtmp://") {
        // RTMP-specific arguments
        cmdArgs = append(cmdArgs,
            "-c:v", camera.Encoder,
            "-preset", "ultrafast",
            "-tune", "zerolatency",
            "-b:v", camera.Bitrate,
            "-maxrate", camera.Bitrate,
            "-bufsize", camera.Bitrate,
            "-s", camera.Resolution,
            "-r", camera.Framerate,
            "-crf", camera.Crf,
            "-f", "flv",
        )

        if camera.Filter != "" {
            cmdArgs = append(cmdArgs, "-vf", camera.Filter)
        }
    } else if strings.HasPrefix(camera.Target, "rtsp://") {
        // RTSP-specific arguments (no re-encoding)
        cmdArgs = append(cmdArgs,
            "-c", "copy",  // Copy both video and audio without re-encoding
            "-f", "rtsp",
            "-rtsp_transport", "tcp",
        )
    } else {
        log.Printf("Unsupported target protocol for camera: %s", camera.Target)
        return nil
    }

    // Add the target URL at the end
    cmdArgs = append(cmdArgs, camera.Target)

    return cmdArgs
}

func lastPathComponent(path string) string {
	index := strings.LastIndex(path, "/") + 1
	return path[index:]
}

func main() {
	if err := loadConfig(); err != nil {
        log.Fatalf("Failed to load configuration: %v", err)
    }

	// Start the HTTP server for the control API
	go startControlAPI()

	go func() {
        for {
            time.Sleep(60 * time.Second)
            reloadConfig()
        }
    }()

	concurrencyLimit := 25 // Adjust the value based on your system's capabilities
	goroutineChannel := make(chan struct{}, concurrencyLimit)
	var wg sync.WaitGroup

	for i, camera := range config.Cameras {
		params := GoroutineParams{
			Camera: camera,
			Index:  i + 1,
		}

		wg.Add(1)
		go processCamera(&wg, goroutineChannel, params)
	}

	log.Println("Camera manager started. Use the control API to manage cameras.")
	wg.Wait() // Wait for all Goroutines to finish
}
