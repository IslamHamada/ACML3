package chatbot

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	calendar "google.golang.org/api/calendar/v3"

	cors "github.com/heppu/simple-cors"
)

var (
	// WelcomeMessage A constant to hold the welcome message
	WelcomeMessage = "Welcome, what do you want to order?"

	// sessions = {
	//   "uuid1" = Session{...},
	//   ...
	// }
	sessions = map[string]Session{}
)

type (
	// Session Holds info about a session
	Session map[string]interface{}

	// JSON Holds a JSON object
	JSON map[string]interface{}
)

func getLoginURL(state string) string {
	return conf.AuthCodeURL(state)
}

type Credentials struct {
	Cid     string `json:"cid"`
	Csecret string `json:"csecret"`
}

var conf *oauth2.Config
var cred Credentials
var tokenMap map[string]*oauth2.Token = make(map[string]*oauth2.Token)

func init() {
	file, err := ioutil.ReadFile("./creds.json")
	if err != nil {
		log.Printf("File error: %v\n", err)
		os.Exit(1)
	}
	json.Unmarshal(file, &cred)

	conf = &oauth2.Config{
		ClientID:     cred.Cid,
		ClientSecret: cred.Csecret,
		RedirectURL:  "https://Project-islamhamada222803960.codeanyapp.com/auth",
		Scopes: []string{
			"https://www.googleapis.com/auth/userinfo.email", // You have to select your own scope from here -> https://developers.google.com/identity/protocols/googlescopes#google_sign-in
			"https://www.googleapis.com/auth/calendar", "https://www.googleapis.com/auth/calendar.readonly",
		},
		Endpoint: google.Endpoint,
	}
}

// withLog Wraps HandlerFuncs to log requests to Stdout
func withLog(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := httptest.NewRecorder()
		fn(c, r)
		log.Printf("[%d] %-4s %s\n", c.Code, r.Method, r.URL.Path)

		for k, v := range c.HeaderMap {
			w.Header()[k] = v
		}
		w.WriteHeader(c.Code)
		c.Body.WriteTo(w)
	}
}

// writeJSON Writes the JSON equivilant for data into ResponseWriter w
func writeJSON(w http.ResponseWriter, data JSON) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// handleWelcome Handles /welcome and responds with a welcome message and a generated UUID
func handleWelcome(w http.ResponseWriter, r *http.Request) {
	// Generate a UUID.
	hasher := md5.New()
	hasher.Write([]byte(strconv.FormatInt(time.Now().Unix(), 10)))
	uuid := hex.EncodeToString(hasher.Sum(nil))

	// Create a session for this UUID
	sessions[uuid] = Session{}
	sessions[uuid]["stage"] = 0
	WelcomeMessage = "open the next url to login and write done when you finish" + "\n" + getLoginURL(uuid)
	// Write a JSON containg the welcome message and the generated UUID
	writeJSON(w, JSON{
		"uuid":    uuid,
		"message": WelcomeMessage,
	})
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	// Make sure only POST requests are handled
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST requests are allowed.", http.StatusMethodNotAllowed)
		return
	}

	// Make sure a UUID exists in the Authorization header
	// 	fmt.Printf()
	uuid := r.Header.Get("Authorization")

	if uuid == "" {
		http.Error(w, "Missing or empty Authorization header.", http.StatusUnauthorized)
		return
	}

	// Make sure a session exists for the extracted UUID
	_, sessionFound := sessions[uuid]
	if !sessionFound {
		http.Error(w, fmt.Sprintf("No session found for: %v.", uuid), http.StatusUnauthorized)
		return
	}

	// Parse the JSON string in the body of the request
	data := JSON{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, fmt.Sprintf("Couldn't decode JSON: %v.", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Make sure a message key is defined in the body of the request
	_, messageFound := data["message"]
	if !messageFound {
		http.Error(w, "Missing message key in body.", http.StatusBadRequest)
		return
	}

	// Process the received message
	//	message, err := processor(session, data["message"].(string))
	//	if err != nil {
	//		http.Error(w, err.Error(), 422 /* http.StatusUnprocessableEntity */)
	//		return
	//	}

	stage := sessions[uuid]["stage"].(int)

	if stage > 1 {
		if data["message"] == "cancel" {
			sessions[uuid]["stage"] = 1
			message := "To create a calendar write 1 , to create an event write 2"
			writeJSON(w, JSON{"message": message})
			return
		}
	}

	if stage == 0 {
		if data["message"] == "done" {
			if tokenMap[uuid] != nil {
				sessions[uuid]["stage"] = 1
				message := "Authentication successful. To create a calendar write 1 , to create an event write 2"
				writeJSON(w, JSON{"message": message})
			} else {
				message := "open the previous url to login"
				writeJSON(w, JSON{"message": message})
			}

		} else {
			message := "write done after authorization"
			writeJSON(w, JSON{"message": message})
		}
	} else {
		if stage == 1 {
			if data["message"] == "1" {
				sessions[uuid]["stage"] = 2
				message := "Enter the calendar info in this form : Summary . For example: gym \n" +
					"To cancel write 'cancel'"
				writeJSON(w, JSON{"message": message})
			} else if data["message"] == "2" {
				sessions[uuid]["stage"] = 3
				message := "Enter the event info in this form : Summary,StartTime,EndTime,Attendees . \n" +
					"For example: summary,2015-05-28T09:00:00-07:00, 2015-05-28T09:10:00-07:00,islamhamada222@gmail.com,morabasha2007@gmail.com. \n" +
					"To cancel write 'cancel'"
				writeJSON(w, JSON{"message": message})
			} else {
				message := "To create a calendar write 1 , to create an event write 2"
				writeJSON(w, JSON{"message": message})
			}
		} else if stage == 2 {
			sessions[uuid]["stage"] = 1
			writeJSON(w, JSON{"message": createCalendar(data["message"].(string), tokenMap[uuid]) + "To create a calendar write 1 , to create an event write 2"})
		} else if stage == 3 {
			sessions[uuid]["stage"] = 1
			input := strings.Split(data["message"].(string), ",")
			writeJSON(w, JSON{"message": createEvent(input, tokenMap[uuid]) + "To create a calendar write 1 , to create an event write 2"})
		}
	}

	// Write a JSON containg the processed response

	//	writeJSON(w, JSON{
	//		"message": message,
	//	})
}

// handle Handles /
func handle(w http.ResponseWriter, r *http.Request) {
	body :=
		"<!DOCTYPE html><html><head><title>Chatbot</title></head><body><pre style=\"font-family: monospace;\">\n" +
			"Available Routes:\n\n" +
			"  GET  /welcome -> handleWelcome\n" +
			"  POST /chat    -> handleChat\n" +
			"  GET  /        -> handle        (current)\n" +
			"</pre></body></html>"
	w.Header().Add("Content-Type", "text/html")
	fmt.Fprintln(w, body)
}

func authHandler(w http.ResponseWriter, r *http.Request) {

	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	log.Println("hello")
	tok, _ := conf.Exchange(oauth2.NoContext, code)
	tokenMap[state] = tok
	fmt.Fprintln(w, "Authentication done you should now return to chatbot")
}

func createCalendar(Summary string, tok *oauth2.Token) string {

	client := conf.Client(oauth2.NoContext, tok)
	calendarService, _ := calendar.New(client)
	cale := new(calendar.Calendar)

	// set calendar attribute
	cale.Summary = Summary
	//

	_, err := calendarService.Calendars.Insert(cale).Do()
	if err != nil {
		return "Cannot create the calendar"
	}

	return "Done successfully"

}

func createEvent(input []string, tok *oauth2.Token) string {

	client := conf.Client(oauth2.NoContext, tok)
	calendarService, _ := calendar.New(client)
	event := new(calendar.Event)

	// set calendar values

	event.Summary = input[0]
	startTime := new(calendar.EventDateTime)
	startTime.DateTime = input[1]
	event.Start = startTime

	endTime := new(calendar.EventDateTime)
	endTime.DateTime = input[2]
	event.End = endTime

	// for adding attendees
	var attendees []*calendar.EventAttendee
	for i := 0; i < len(input)-3; i++ {
		var attendee calendar.EventAttendee
		attendee.Email = input[3+i]
		attendees = append(attendees, &attendee)
	}
	event.Attendees = attendees

	// ignore the remdinders part
	// for adding reminders
	//	var reminderArray []*calendar.EventReminder
	//	var reminder calendar.EventReminder
	//	reminder.Method = "email"
	//	reminder.Minutes = 10
	//	reminderArray = append(reminderArray, &reminder)
	//	log.Println(reminderArray)
	//	event.Reminders = new(calendar.EventReminders)
	//	event.Reminders.UseDefault = false
	//	event.Reminders.Overrides = reminderArray

	_, err := calendarService.Events.Insert("primary", event).Do()
	if err != nil {
		return "Cannot create event"
	}

	return "Done successfully"
}

// Engage Gives control to the chatbot
func Engage(addr string) error {
	// HandleFuncs
	mux := http.NewServeMux()
	mux.HandleFunc("/welcome", withLog(handleWelcome))
	mux.HandleFunc("/chat", withLog(handleChat))
	mux.HandleFunc("/auth", withLog(authHandler))
	mux.HandleFunc("/", withLog(handle))

	// Start the server
	//	return
	return http.ListenAndServe(addr, cors.CORS(mux))
}
