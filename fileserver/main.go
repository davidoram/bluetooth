package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
)

func methodHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("%s to /method", r.Method)
	switch r.Method {

	case http.MethodDelete:
		fmt.Fprintf(w, "You sent a DELETE")
		w.WriteHeader(http.StatusOK)

	case http.MethodHead:
		w.WriteHeader(http.StatusOK)

	case http.MethodPatch:
		b, err := ioutil.ReadAll(r.Body)
		if err != nil {
			panic(err)
		}
		fmt.Fprintf(w, "You sent a PATCH, with body %s", string(b))
		w.WriteHeader(http.StatusOK)

	case http.MethodPost:
		b, err := ioutil.ReadAll(r.Body)
		if err != nil {
			panic(err)
		}
		fmt.Fprintf(w, "You sent a POST, with body %s", string(b))
		w.WriteHeader(http.StatusCreated)
	}
}

func main() {
	port := flag.String("p", "8100", "port to serve on")
	directory := flag.String("d", ".", "the directory of static file to host")
	flag.Parse()

	http.HandleFunc("/method", methodHandler)
	http.Handle("/", http.FileServer(http.Dir(*directory)))

	log.Printf("Serving %s on HTTP port: %s\n", *directory, *port)
	log.Fatal(http.ListenAndServe(":"+*port, nil))
}
