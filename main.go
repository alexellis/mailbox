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
	"github.com/prometheus/client_golang/prometheus"
)

var sendClient *http.Client

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
	q := NewWorkQueue()

	serviceReplicas := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "mailbox_queue_depth",
			Help: "Mailbox Queue Depth",
		},
	)

	sendClient = &http.Client{}

	prometheus.MustRegister(serviceReplicas)

	ticker := time.NewTicker(time.Second * 1)

	go func() {
		for range ticker.C {
			if len(*q.Items) > 0 {
				log.Printf("[%d] items in mailbox", len(*q.Items))
			}
			serviceReplicas.Set(float64(len(*q.Items)))

			for i, request := range *q.Items {
				deadline := request.LastTry.Add(request.RestartDelay)
				items := *q.Items

				if items[i].Sent == false && time.Now().After(deadline) {
					fmt.Println("Try again")

					items[i].LastTry = time.Now()
					items[i].Retries = items[i].Retries + 1

					statusCode, err := submit(&request)
					fmt.Printf("Posting [%d] code: %d", i, statusCode)
					items[i].Sent = (err == nil)

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
	r.Handle("/metrics", prometheus.Handler())

	log.Fatalln(http.ListenAndServe(":8080", r))
}

func submit(req *DeferredRequest) (int, error) {
	gatewayURL := "http://gateway:8080"
	if val, exists := os.LookupEnv("gateway_url"); exists {
		gatewayURL = val
	}

	URI := fmt.Sprintf("%s/async-function/%s/", strings.TrimRight(gatewayURL, "/"), req.Function)
	buf := bytes.NewBuffer(req.Body)

	request, _ := http.NewRequest(http.MethodPost, URI, buf)
	request.Header.Add("X-Retries", fmt.Sprintf("%d", req.Retries+1))
	request.Header.Add("X-Max-Retries", fmt.Sprintf("%d", req.MaxRetries))
	request.Header.Add("X-Delay-Duration", fmt.Sprintf("%d", int64(req.RestartDelay.Seconds())))

	response, err := sendClient.Do(request)
	if err != nil {
		return http.StatusGatewayTimeout, fmt.Errorf("cannot retry item %T - error: %s", req, err.Error())
	} else if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusAccepted {
		return response.StatusCode, fmt.Errorf("unexpected status from gateway: %s", response.Status)
	}

	log.Printf("Post to %s, status: %s", URI, response.Status)
	return response.StatusCode, nil
}

// DeferredRequest serialized on slice / queue
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
