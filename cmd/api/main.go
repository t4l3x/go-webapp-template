// Package main starts the HTTP API process.
package main

import "github.com/t4l3x/go-webapp-template/internal/bootstrap"

func main() {
	bootstrap.NewAPI().Run()
}
