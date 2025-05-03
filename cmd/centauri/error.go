package main

import (
	"log"
	"net/http"
)

const badGatewayError = `<!doctype html>
<html lang="en">
<head>
  <title>502 Bad Gateway</title>
</head>
<body>
  <h1>Bad Gateway</h1>
  <p>The server was unable to complete your request. Please try again later.</p>
</body>
</html>`

// handleError handles the reverse proxy not being able to connect to an upstream
func handleError(writer http.ResponseWriter, request *http.Request, err error) {
	log.Printf("Failed to connect to upstream %s: %v", request.Host, err)
	writer.WriteHeader(http.StatusBadGateway)
	_, _ = writer.Write([]byte(badGatewayError))
}
