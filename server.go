package quayd

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/codegangsta/negroni"
	"github.com/gorilla/mux"
)

var validStatuses = []string{"pending", "success", "error", "failure"}

type Server struct {
	http.Handler
}

func NewServer(q *Quayd) *Server {
	if q == nil {
		q = Default
	}

	m := mux.NewRouter()

	m.Handle("/quay/{status}", &Webhook{q}).Methods("POST")

	n := negroni.Classic()
	n.UseHandler(m)

	return &Server{n}
}

type Webhook struct {
	*Quayd
}

type WebhookForm struct {
	Repository  string           `json:"repository"`
	TriggerKind string           `json:"trigger_kind"`
	IsManual    bool             `json:"is_manual"`
	DockerTags  []string         `json:"docker_tags"`
	BuildURL    string           `json:"homepage"`
	Metadata    *TriggerMetadata `json:"trigger_metadata"`
}

type TriggerMetadata struct {
	Commit string `json:"commit"`
	Ref    string `json:"ref"`
}

func (wh *Webhook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	status := vars["status"]
	if !validStatus(status) {
		http.Error(w, "Invalid status: " + status, 400)
		return
	}

	var form = &WebhookForm{
		Metadata: &TriggerMetadata{},
	}

	if err := json.NewDecoder(r.Body).Decode(&form); err != nil {
		errorResponse(w, err)
		return
	}

	// We don't want to process manually triggered builds.
	if !(!form.IsManual && form.TriggerKind == "github") {
		w.WriteHeader(204)
		return
	}

	if status == "success" {
		if err := wh.Quayd.LoadImageTags(form.DockerTags[0], form.Repository, form.Metadata.Commit); err != nil {
			errorResponse(w, err)
			return
		}
	}

	if err := wh.Quayd.Handle(form.Repository, form.Metadata.Commit, form.BuildURL, status); err != nil {
		errorResponse(w, err)
		return
	}
}

func validStatus(a string) bool {
	for _, b := range validStatuses {
		if b == a {
			return true
		}
	}
	return false
}

func errorResponse(w http.ResponseWriter, err error) {
	fmt.Println(err)
	http.Error(w, err.Error(), 500)
}
