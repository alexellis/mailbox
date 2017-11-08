package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

type WorkQueue struct {
	Items *[]DeferredRequest
	Mutex sync.Mutex
}

func (w *WorkQueue) Add(request DeferredRequest) {
	w.Mutex.Lock()
	*w.Items = append(*w.Items, request)
	w.Mutex.Unlock()
}

func (w *WorkQueue) Recompose() {
	w.Mutex.Lock()
	newQueue := []DeferredRequest{}
	for _, item := range *w.Items {
		if !item.Sent {
			newQueue = append(newQueue, item)
		}
	}

	w.Items = &newQueue
	w.Mutex.Unlock()
}

func NewWorkQueue() WorkQueue {
	return WorkQueue{Items: &[]DeferredRequest{}, Mutex: sync.Mutex{}}
}

func main() {
	req := DeferredRequest{
		Function:     "bin",
		Query:        "post=true",
		Retries:      0,
		MaxRetries:   1,
		RestartDelay: time.Second * 4,
		Body:         []byte("body data here."),
		LastTry:      time.Now(),
	}

	q := NewWorkQueue()
	q.Add(req)

	ticker := time.NewTicker(time.Second * 1)

	fmt.Println("here")
	go func() {
		for t := range ticker.C {
			fmt.Println(t)
			for i, request := range *q.Items {
				deadline := request.LastTry.Add(request.RestartDelay)

				if time.Now().After(deadline) {
					fmt.Println("Try again")

					item := *q.Items
					item[i].LastTry = time.Now()
					item[i].Retries = item[i].Retries + 1

					error := submit(&request)
					if error != nil {
						item[i].Sent = true

					}
				}
			}
			q.Recompose()
		}
	}()

	r := mux.NewRouter()
	r.HandleFunc("/deadletter/{function}", func(w http.ResponseWriter, req *http.Request) {
		vars := mux.Vars(req)

		delay := time.Second * 4
		durationHeader := req.Header.Get("X-Delay-Duration")
		if len(durationHeader) > 0 {
			seconds, _ := strconv.Atoi(durationHeader)
			delay = time.Second * time.Duration(seconds)
		}

		retries := 0
		retriesHeader := req.Header.Get("X-Retries")
		if len(retriesHeader) > 0 {
			retries, _ = strconv.Atoi(retriesHeader)
		}

		maxRetries := 0
		maxRetriesHeader := req.Header.Get("X-Max-Retries")
		if len(maxRetriesHeader) > 0 {
			maxRetries, _ = strconv.Atoi(maxRetriesHeader)
		}

		if val := vars["function"]; len(val) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		var body []byte
		if req.Body != nil {
			defer req.Body.Close()
			body, _ = ioutil.ReadAll(req.Body)
		}

		newReq := DeferredRequest{
			Function:     vars["function"],
			Query:        "",
			Retries:      retries,
			MaxRetries:   maxRetries,
			RestartDelay: delay,
			Body:         body,
			LastTry:      time.Now(),
		}
		q.Add(newReq)

		w.WriteHeader(http.StatusAccepted)
	})

	fmt.Println("Listen and Serve on 8080")
	log.Fatalln(http.ListenAndServe(":8080", r))
}

func submit(req *DeferredRequest) error {
	gatewayURL := "http://gateway:8080"
	if val, exists := os.LookupEnv("gateway_url"); exists {
		gatewayURL = val
	}

	URI := fmt.Sprintf("%s/async-function/%s/", strings.TrimRight(gatewayURL, "/"), req.Function)
	buf := bytes.NewBuffer(req.Body)

	request, _ := http.NewRequest(http.MethodPost, URI, buf)
	request.Header.Add("X-Retries", fmt.Sprintf("%d", req.Retries))
	request.Header.Add("X-Max-Retries", fmt.Sprintf("%d", req.MaxRetries))
	request.Header.Add("X-Delay-Duration", fmt.Sprintf("%d", int64(req.RestartDelay.Seconds())))

	c := http.Client{}

	response, err := c.Do(request)
	if err != nil {
		log.Printf("Cannot retry %T\n", req)
		return err
	} else if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusAccepted {
		return fmt.Errorf("unexpected status from gateway: %s", response.Status)
	}

	log.Printf("Posting to %s, status: %s", URI, response.Status)
	return nil
}

type DeferredRequest struct {
	// Definition

	Function     string
	Query        string
	Header       http.Header
	MaxRetries   int
	RestartDelay time.Duration
	Body         []byte

	// State

	Retries int
	LastTry time.Time
	Sent    bool
}
