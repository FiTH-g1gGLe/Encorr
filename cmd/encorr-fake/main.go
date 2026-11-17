// encorr-fake runs a pretend Plex, TMDB, Radarr, Sonarr and Discord for
// trying Encorr without touching a real server. Developer tool.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/FiTH-g1gGLe/encorr/internal/fake"
)

func main() {
	port := flag.Int("port", 5199, "port")
	flag.Parse()
	s := fake.New()
	s.URL = fmt.Sprintf("http://localhost:%d", *port)
	log.Printf("fake stack on %s (TMDB key: good-key, Radarr key: radarr-key, Sonarr key: sonarr-key)", s.URL)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), s))
}
