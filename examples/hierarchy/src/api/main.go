// The hierarchy example's api: compiled in the Dockerfile's build stage.
package main

import (
	"fmt"
	"net/http"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "ok") })
	_ = http.ListenAndServe(":8080", nil)
}
