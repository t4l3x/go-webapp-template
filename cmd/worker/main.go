// Package main starts the background worker process: it
// polls the PostgreSQL outbox and dispatches claimed events (e.g.
// identity's email-verification delivery) to their handlers.
package main

import "github.com/t4l3x/go-webapp-template/internal/bootstrap"

func main() {
	bootstrap.NewWorker().Run()
}
