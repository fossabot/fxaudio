package api

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/danesparza/fxaudio/event"
	"github.com/danesparza/fxaudio/media"
	"github.com/danesparza/fxaudio/mediatype"
	"github.com/gorilla/mux"
	"github.com/rs/xid"
	"github.com/spf13/viper"
)

/*

/audio
PUT - Upload file
GET - List all files
POST - Play a random file (or if passed an endpoint in JSON, stream that file)

/audio/1
GET - Download file
POST - Play file
DELETE - Delete file

*/

func (service Service) UploadFile(rw http.ResponseWriter, req *http.Request) {

	MAX_UPLOAD_SIZE := viper.GetInt64("upload.bytelimit")
	UploadPath := viper.GetString("upload.path")

	//	First check for maximum uplooad size and return an error if we exceed it.
	req.Body = http.MaxBytesReader(rw, req.Body, MAX_UPLOAD_SIZE)
	if err := req.ParseMultipartForm(MAX_UPLOAD_SIZE); err != nil {
		err = fmt.Errorf("could not parse multipart form: %v", err)
		sendErrorResponse(rw, err, http.StatusRequestEntityTooLarge)
		return
	}

	// FormFile returns the first file for the given key `myFile`
	// it also returns the FileHeader so we can get the Filename,
	// the Header and the size of the file
	file, fileHeader, err := req.FormFile("file")
	if err != nil {
		err = fmt.Errorf("error retrieving file: %v", err)
		sendErrorResponse(rw, err, http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Create the uploads folder if it doesn't
	// already exist
	err = os.MkdirAll(UploadPath, os.ModePerm)
	if err != nil {
		err = fmt.Errorf("error creating uploads path: %v", err)
		sendErrorResponse(rw, err, http.StatusInternalServerError)
		return
	}

	// Create a new file in the uploads directory
	destinationFile := path.Join(UploadPath, fileHeader.Filename)
	dst, err := os.Create(destinationFile)
	if err != nil {
		err = fmt.Errorf("error creating file: %v", err)
		sendErrorResponse(rw, err, http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	// Copy the uploaded file to the filesystem
	// at the specified destination
	_, err = io.Copy(dst, file)
	if err != nil {
		err = fmt.Errorf("error saving file: %v", err)
		sendErrorResponse(rw, err, http.StatusInternalServerError)
		return
	}

	//	Add it to our system database
	service.DB.AddFile(destinationFile, "Audio file")

	//	Record the event:
	service.DB.AddEvent(event.FileUploaded, mediatype.Audio, fileHeader.Filename, GetIP(req), service.HistoryTTL)

	//	If we've gotten this far, indicate a successful upload
	response := SystemResponse{
		Message: "File uploaded",
	}

	//	Serialize to JSON & return the response:
	rw.WriteHeader(http.StatusCreated)
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(rw).Encode(response)
}

//	ListAllFiles lists all files in the system
func (service Service) ListAllFiles(rw http.ResponseWriter, req *http.Request) {

	//	Get a list of files
	retval, err := service.DB.GetAllFiles()
	if err != nil {
		err = fmt.Errorf("error getting a list of files: %v", err)
		sendErrorResponse(rw, err, http.StatusInternalServerError)
		return
	}

	//	Construct our response
	response := SystemResponse{
		Message: fmt.Sprintf("%v file(s)", len(retval)),
		Data:    retval,
	}

	//	Serialize to JSON & return the response:
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(rw).Encode(response)
}

//	DeleteFile deletes a file in the system
func (service Service) DeleteFile(rw http.ResponseWriter, req *http.Request) {

	//	Get the id from the url (if it's blank, return an error)
	vars := mux.Vars(req)
	if vars["id"] == "" {
		err := fmt.Errorf("requires an id of a file to delete")
		sendErrorResponse(rw, err, http.StatusBadRequest)
		return
	}

	//	Delete the file
	err := service.DB.DeleteFile(vars["id"])
	if err != nil {
		err = fmt.Errorf("error deleting file: %v", err)
		sendErrorResponse(rw, err, http.StatusInternalServerError)
		return
	}

	//	Record the event:
	service.DB.AddEvent(event.FileDeleted, mediatype.Audio, vars["id"], GetIP(req), service.HistoryTTL)

	//	Construct our response
	response := SystemResponse{
		Message: "File deleted",
		Data:    vars["id"],
	}

	//	Serialize to JSON & return the response:
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(rw).Encode(response)
}

// PlayAudio plays an audio file
func (service Service) PlayAudio(rw http.ResponseWriter, req *http.Request) {

	//	Get the id from the url (if it's blank, return an error)
	vars := mux.Vars(req)
	if vars["id"] == "" {
		err := fmt.Errorf("requires an id of a file to play")
		sendErrorResponse(rw, err, http.StatusBadRequest)
		return
	}

	//	Get the file information
	fileToPlay, err := service.DB.GetFile(vars["id"])
	if err != nil {
		err = fmt.Errorf("error getting file information: %v", err)
		sendErrorResponse(rw, err, http.StatusInternalServerError)
		return
	}

	/*
		//	Make sure omxplayer is installed (for HDMI based audio)
		//	Instructions on how to install it:
		//	https://www.gaggl.com/2013/01/installing-omxplayer-on-raspberry-pi/
		_, err := exec.LookPath("omxplayer")
		if err != nil {
			err = fmt.Errorf("Didn't find omxplayer executable in the path: %v", err)
			sendErrorResponse(rw, err, http.StatusInternalServerError)
			return
		}
	*/

	//	Make sure mpg123 is installed (for i2s / ALSA based digital audio)
	//	Instructions on how to install it:
	//	https://learn.adafruit.com/adafruit-speaker-bonnet-for-raspberry-pi/raspberry-pi-test
	_, err = exec.LookPath("mpg123")
	if err != nil {
		err = fmt.Errorf("didn't find mpg123 executable in the path: %v", err)
		sendErrorResponse(rw, err, http.StatusServiceUnavailable)
		return
	}

	//	Send to the channel:
	playRequest := media.PlayAudioRequest{
		ProcessID: xid.New().String(), // Generate a new id
		ID:        fileToPlay.ID,
		FilePath:  fileToPlay.FilePath,
	}
	service.PlayMedia <- playRequest

	//	Record the event:
	service.DB.AddEvent(event.RequestPlay, mediatype.Audio, fmt.Sprintf("%+v", playRequest), GetIP(req), service.HistoryTTL)

	//	Create our response and send information back:
	response := SystemResponse{
		Message: "Audio playing",
		Data:    playRequest,
	}

	//	Serialize to JSON & return the response:
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(rw).Encode(response)
}

// StreamAudio plays an audio endpoint
func (service Service) StreamAudio(rw http.ResponseWriter, req *http.Request) {

	//	req.Body is a ReadCloser -- we need to remember to close it:
	defer req.Body.Close()

	//	Decode the request
	request := PlayAudioRequest{}
	err := json.NewDecoder(req.Body).Decode(&request)
	if err != nil {
		sendErrorResponse(rw, err, http.StatusBadRequest)
		return
	}

	//	Get just the file endpoint:
	fileendpoint := strings.TrimSpace(request.Endpoint)

	//	Make sure mpg123 is installed (for i2s / ALSA based digital audio)
	_, err = exec.LookPath("mpg123")
	if err != nil {
		err = fmt.Errorf("didn't find mpg123 executable in the path: %v", err)
		sendErrorResponse(rw, err, http.StatusServiceUnavailable)
		return
	}

	//	Send to the channel:
	playRequest := media.PlayAudioRequest{
		ProcessID: xid.New().String(), // Generate a new id
		FilePath:  fileendpoint,
	}
	service.PlayMedia <- playRequest

	//	Record the event:
	service.DB.AddEvent(event.RequestPlay, mediatype.Audio, fmt.Sprintf("%+v", playRequest), GetIP(req), service.HistoryTTL)

	//	Create our response and send information back:
	response := SystemResponse{
		Message: "Audio playing",
		Data:    playRequest,
	}

	//	Serialize to JSON & return the response:
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(rw).Encode(response)
}

// PlayAudio plays an audio endpoint or a random file already uploaded
func (service Service) PlayRandomAudio(rw http.ResponseWriter, req *http.Request) {

	//	req.Body is a ReadCloser -- we need to remember to close it:
	defer req.Body.Close()

	//	Get just the file endpoint:
	fileendpoint := ""

	//	If we don't have an endpoint specified, get a random file that we manage
	if fileendpoint == "" {
		retval, err := service.DB.GetAllFiles()
		if err != nil {
			err = fmt.Errorf("error getting a list of files: %v", err)
			sendErrorResponse(rw, err, http.StatusInternalServerError)
			return
		}

		//	If we don't have anything to pick from, bomb out
		if len(retval) < 1 {
			err = fmt.Errorf("can't play anything -- no endpoint specified, and no files to randomly pick from: %v", err)
			sendErrorResponse(rw, err, http.StatusBadRequest)
			return
		}

		//	Pick a random file:
		randomIndex := rand.Intn(len(retval))

		//	Set fileendpoint to the random file's path:
		fileendpoint = retval[randomIndex].FilePath
	}

	//	Make sure mpg123 is installed (for i2s / ALSA based digital audio)
	_, err := exec.LookPath("mpg123")
	if err != nil {
		err = fmt.Errorf("didn't find mpg123 executable in the path: %v", err)
		sendErrorResponse(rw, err, http.StatusServiceUnavailable)
		return
	}

	//	Send to the channel:
	playRequest := media.PlayAudioRequest{
		ProcessID: xid.New().String(), // Generate a new id
		FilePath:  fileendpoint,
	}
	service.PlayMedia <- playRequest

	//	Record the event:
	service.DB.AddEvent(event.RequestPlay, mediatype.Audio, fmt.Sprintf("%+v", playRequest), GetIP(req), service.HistoryTTL)

	//	Create our response and send information back:
	response := SystemResponse{
		Message: "Audio playing",
		Data:    playRequest,
	}

	//	Serialize to JSON & return the response:
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(rw).Encode(response)
}

// StopAudio stops an audio file 'play' process
func (service Service) StopAudio(rw http.ResponseWriter, req *http.Request) {

	//	Get the id from the url (if it's blank, return an error)
	vars := mux.Vars(req)
	if vars["pid"] == "" {
		err := fmt.Errorf("requires a processid of a process to stop")
		sendErrorResponse(rw, err, http.StatusBadRequest)
		return
	}

	//	Send to the channel:
	service.StopMedia <- vars["pid"]

	//	Record the event:
	service.DB.AddEvent(event.RequestStop, mediatype.Audio, vars["pid"], GetIP(req), service.HistoryTTL)

	//	Create our response and send information back:
	response := SystemResponse{
		Message: "Audio stopping",
		Data:    vars["pid"],
	}

	//	Serialize to JSON & return the response:
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(rw).Encode(response)
}

// StopAllAudio stops all audio 'play' processes
func (service Service) StopAllAudio(rw http.ResponseWriter, req *http.Request) {

	//	Send to the channel:
	service.StopAllMedia <- true

	//	Record the event:
	service.DB.AddEvent(event.RequestStopAll, mediatype.Audio, "Stop all plays", GetIP(req), service.HistoryTTL)

	//	Create our response and send information back:
	response := SystemResponse{
		Message: "All Audio stopping",
		Data:    ".",
	}

	//	Serialize to JSON & return the response:
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(rw).Encode(response)
}
