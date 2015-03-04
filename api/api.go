package api

// HTTP API implementation

import (
	"fmt"
	"github.com/alonsovidales/pit/log"
	"github.com/alonsovidales/pit/recommender"
	"net/http"
)

type ApiInt interface {
	StopAll()
}

type Api struct {
	ApiInt

	recomender recommender.RecommenderInt
}

func (ap *Api) getScores(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Get Scores %s", r.URL.Path[1:])

	// ap.CalcScores()
}

func (ap *Api) addRecord(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Add record %s", r.URL.Path[1:])

	// ap.AddRecord()
}

func Init(port int64, rc recommender.RecommenderInt) (ap *Api) {
	ap = &Api{
		recomender: rc,
	}

	// This method is used to request por a new recomendation, and stores
	// the info on memory
	http.HandleFunc("/q", ap.getScores)
	// This method is used just to migrate the user information from
	// another shard to the local memory of this one
	http.HandleFunc("/a", ap.addRecord)

	log.Info("Starting server at server:", port)
	go func() {
		err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
		if err != nil {
			log.Fatal("HTTP server can't start on port:", port, "Error:", err)
		}
	}()

	return
}

func (ap *Api) StopAll() {
	log.Info("Stopping HTTP API...")
}
