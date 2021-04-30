package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/danesparza/fxaudio/data"
	"github.com/danesparza/fxaudio/media"
)

// Service encapsulates API service operations
type Service struct {
	DB        *data.Manager
	StartTime time.Time

	// PlayMedia signals a file should be played
	PlayMedia chan media.PlayAudioRequest

	// StopMedia signals a file should stop playing
	StopMedia chan string
}

// PlayAudioRequest represents a request to play an audio endpoint
type PlayAudioRequest struct {
	Endpoint string `json:"endpoint"`
}

// SystemResponse is a response for a system request
type SystemResponse struct {
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// ErrorResponse represents an API response
type ErrorResponse struct {
	Message string `json:"message"`
}

//	Used to send back an error:
func sendErrorResponse(rw http.ResponseWriter, err error, code int) {
	//	Our return value
	response := ErrorResponse{
		Message: "Error: " + err.Error()}

	//	Serialize to JSON & return the response:
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	rw.WriteHeader(code)
	json.NewEncoder(rw).Encode(response)
}

// ShowUI redirects to the /ui/ url path
func ShowUI(rw http.ResponseWriter, req *http.Request) {
	// http.Redirect(rw, req, "/ui/", 301)
	fmt.Fprintf(rw, "Hello, world - UI")
}
