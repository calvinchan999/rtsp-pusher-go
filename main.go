package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"runtime"
	"github.com/gorilla/mux"
)

const (
    defaultEncoder    = "libx264"
    defaultBitrate   = "100k"
    defaultCrf       = "18"
    defaultResolution = "480x270"
    defaultFramerate = "15"
    defaultEnabled   = "true"
    
    apiPort         = ":8080"
    configReloadInterval = 60 * time.Second
    processCheckInterval = 5 * time.Second
    ffmpegTimeout   = "30000000" // 30s
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
    Enabled    bool   `json:"enabled"`
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
                config.Cameras[i].Encoder = defaultEncoder
                configChanged = true
            }
            if config.Cameras[i].Bitrate == "" {
                config.Cameras[i].Bitrate = defaultBitrate
                configChanged = true
            }
            if config.Cameras[i].Crf == "" {
                config.Cameras[i].Crf = defaultCrf
                configChanged = true
            }
            if config.Cameras[i].Resolution == "" {
                config.Cameras[i].Resolution = defaultResolution
                configChanged = true
            }
            if config.Cameras[i].Framerate == "" {
                config.Cameras[i].Framerate = defaultFramerate
                configChanged = true
            }
        }
        if config.Cameras[i].Enabled == false {
            config.Cameras[i].Enabled = true
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

func startControlAPI() error {
	router := mux.NewRouter()
	router.HandleFunc("/camera/{id}/control", controlCameraHandler).Methods("POST")
	router.HandleFunc("/camera/status", getCameraStatusHandler).Methods("GET")

	log.Println("Starting control API server on :8080")
	return http.ListenAndServe(":8080", router)
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
    config.Cameras[cameraIndex].Enabled = action.Action
    configMutex.Unlock()

    if !action.Action {
        cameraProcessMux.Lock()
        if cmd, exists := cameraProcesses[cameraID]; exists {
            if err := stopCommand(cmd); err != nil {
                log.Printf("Failed to stop camera %d: %v", cameraID, err)
            }
        }
        cameraProcessMux.Unlock()
    }

    if err := saveConfig(); err != nil {
        log.Printf("Failed to save config: %v", err)
        http.Error(w, "Failed to save configuration", http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(map[string]string{
        "message": fmt.Sprintf("Camera %d turned %s", cameraID, map[bool]string{true: "on", false: "off"}[action.Action]),
    })
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
			Status:     strings.ToLower(fmt.Sprintf("%v", camera.Enabled)),
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
            enabled = config.Cameras[params.Index-1].Enabled
        }
        camera := config.Cameras[params.Index-1]
        configMutex.RUnlock()

        if enabled {
            if cmd == nil {
                cmdMutex.Lock()
                args := buildFFmpegArgs(camera)
                if args == nil {
                    log.Printf("Failed to build FFmpeg arguments for camera %d", params.Index)
                    cmdMutex.Unlock()
                    continue
                }
                
                cmd = exec.Command("ffmpeg", args...)
                // Redirect stdout and stderr to capture logs
                cmd.Stdout = os.Stdout
                cmd.Stderr = os.Stderr
                cmdMutex.Unlock()

                go func() {
                    if err := cmd.Run(); err != nil {
                        log.Printf("FFmpeg command for camera %d failed: %v", params.Index, err)
                    }
                    
                    cmdMutex.Lock()
                    cmd = nil
                    cmdMutex.Unlock()
                }()
            }
        } else {
            cmdMutex.Lock()
            if cmd != nil && cmd.Process != nil {
                if err := stopCommand(cmd); err != nil {
                    log.Printf("Failed to stop FFmpeg command for camera %d: %v", params.Index, err)
                }
            }
            cmdMutex.Unlock()
        }
        
        <-goroutineChannel // Release the spot in the channel
        time.Sleep(processCheckInterval)
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

    // Create a channel to handle graceful shutdown
    stop := make(chan os.Signal, 1)
    signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

    // Create an error channel for the API server
    errChan := make(chan error, 1)

    // Start the HTTP server for the control API
    go func() {
        errChan <- startControlAPI()
    }()

    // Start config reloader
    ticker := time.NewTicker(configReloadInterval)
    defer ticker.Stop()

    go func() {
        for range ticker.C {
            reloadConfig()
        }
    }()

    concurrencyLimit := 25
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

    // Wait for shutdown signal or error
    select {
    case <-stop:
        log.Println("Shutting down gracefully...")
        // Implement cleanup logic here
        // Stop all FFmpeg processes
        cameraProcessMux.Lock()
        for id, cmd := range cameraProcesses {
            if err := stopCommand(cmd); err != nil {
                log.Printf("Failed to stop camera %d: %v", id, err)
            }
        }
        cameraProcessMux.Unlock()
    case err := <-errChan:
        log.Printf("Error from API server: %v", err)
    }

    log.Println("Shutdown complete")
}
