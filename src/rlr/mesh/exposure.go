// This file is the loopback HTTP transport over the mesh.
package mesh

import (
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"time"
)

// Reply is one HTTP response the language layer renders.
type Reply struct {
	Status      int
	ContentType string
	Body        []byte
}

// Route is one method reachable through the exposure.
type Route struct {
	Locator Locator
	Method  string
	Mutates bool
	Decode  func(body []byte) ([]any, *Reply)
	Encode  func(Response) Reply
}

// Exposure serves routes over HTTP with one external trigger per request.
type Exposure struct {
	Runtime   *Runtime
	Routes    map[string]Route
	Budget    time.Duration
	BodyLimit int64
}

const (
	headerTimeout = 5 * time.Second
	idleTimeout   = time.Minute
)

// Serve listens on addr and serves requests until the process ends.
func (x Exposure) Serve(addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: x, ReadHeaderTimeout: headerTimeout, IdleTimeout: idleTimeout}
	go server.Serve(l)
	return nil
}

func (x Exposure) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	route, found := x.Routes[req.URL.Path]
	if !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if req.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !jsonMedia(req.Header.Get("Content-Type")) {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, x.BodyLimit))
	if err != nil {
		if _, tooLarge := errors.AsType[*http.MaxBytesError](err); tooLarge {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
		return
	}
	args, malformed := route.Decode(body)
	var reply Reply
	if malformed != nil {
		reply = *malformed
	} else {
		client := x.Runtime.Trigger(x.Budget)
		r, _ := client.Start(route.Locator, route.Method, route.Mutates, args).Settle()
		reply = route.Encode(r)
	}
	if reply.ContentType != "" {
		w.Header().Set("Content-Type", reply.ContentType)
	}
	w.WriteHeader(reply.Status)
	w.Write(reply.Body)
}

// jsonMedia reports whether a content type names JSON.
func jsonMedia(ct string) bool {
	media, _, err := mime.ParseMediaType(ct)
	return err == nil && media == "application/json"
}
