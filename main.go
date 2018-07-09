package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi"
	"github.com/go-chi/cors"
	"github.com/urfave/cli"
)

var requestsPerMinuteLimit int
var verboseLogging bool

func main() {

	var port string
	var redirecturl string
	var allowedPaths string
	var noLimitIPs string

	app := cli.NewApp()

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:        "port, p",
			Value:       "8888",
			Usage:       "default server port, ':8888'",
			Destination: &port,
		},
		cli.StringFlag{
			Name:        "url, u",
			Value:       "http://127.0.0.1:8588",
			Usage:       "redirect url, default is http://127.0.0.1:8588",
			Destination: &redirecturl,
		},
		cli.StringFlag{
			Name:        "allow, a",
			Value:       "eth*,net_*",
			Usage:       "list of allowed pathes(separated by commas) - default is 'eth*,net_*'",
			Destination: &allowedPaths,
		},
		cli.IntFlag{
			Name:        "rpm",
			Value:       1000,
			Usage:       "limit for number of requests per minute from single IP(default it 1000)",
			Destination: &requestsPerMinuteLimit,
		},
		cli.StringFlag{
			Name:        "nolimit, n",
			Usage:       "list of ips allowed unlimited requests(separated by commas)",
			Destination: &noLimitIPs,
		},
		cli.BoolFlag{
			Name:        "verbose",
			Usage:       "verbose logging enabled",
			Destination: &verboseLogging,
		},
	}

	app.Action = func(c *cli.Context) error {
		log.Println("server will run on :", port)
		log.Println("redirecting to :", redirecturl)
		log.Println("list of allowed pathes :", allowedPaths)
		log.Println("list of no-limit IPs :", noLimitIPs)
		log.Println("requests from IP per minute limited to :", requestsPerMinuteLimit)

		// Create proxy server.
		server, err := NewServer(redirecturl, strings.Split(allowedPaths, ","), strings.Split(noLimitIPs, ","))
		if err != nil {
			return fmt.Errorf("failed to start server: %s", err)
		}

		r := chi.NewRouter()
		cors := cors.New(cors.Options{
			AllowedOrigins:   []string{"*"},
			AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
			AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
			AllowCredentials: true,
			MaxAge:           300, // Maximum value not ignored by any of major browsers
		})
		r.Use(cors.Handler)

		r.Get("/", server.HomePage)
		r.Get("/stats", server.Stats)
		r.HandleFunc("/*", server.RPCProxy)
		log.Fatal(http.ListenAndServe(":"+port, r))
		return nil
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
